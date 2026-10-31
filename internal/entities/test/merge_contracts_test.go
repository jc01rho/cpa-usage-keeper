package test

import (
	. "cpa-usage-keeper/internal/entities"
	"sort"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func parseSchema(t *testing.T, model any) *schema.Schema {
	t.Helper()
	parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse schema %T: %v", model, err)
	}
	return parsed
}

func storedColumns(parsed *schema.Schema) []string {
	var cols []string
	for _, field := range parsed.Fields {
		if field.DBName != "" {
			cols = append(cols, field.DBName)
		}
	}
	sort.Strings(cols)
	return cols
}

// The hot/archive copy uses UsageEventStorageColumns verbatim. A merge that adds
// a column to one side only silently drops data from the archive copy.
func TestUsageEventStorageColumnsMatchEntities(t *testing.T) {
	want := strings.Split(UsageEventStorageColumns, ",")
	for i := range want {
		want[i] = strings.TrimSpace(want[i])
	}
	sort.Strings(want)
	for _, model := range []any{&UsageEvent{}, &UsageEventArchive{}} {
		got := storedColumns(parseSchema(t, model))
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%T columns diverge from UsageEventStorageColumns\n got: %v\nwant: %v", model, got, want)
		}
	}
	for _, fork := range []string{"instance_id", "is_native_key", "access_provider"} {
		found := false
		for _, col := range want {
			found = found || col == fork
		}
		if !found {
			t.Fatalf("UsageEventStorageColumns lost %q", fork)
		}
	}
}

// Fork instance scoping (instance_id) and upstream key provenance
// (source_type, access_provider) must coexist on the API key entity, and the
// uniqueness must stay per instance and key.
func TestCPAAPIKeyKeepsInstanceScopeAndProvenance(t *testing.T) {
	parsed := parseSchema(t, &CPAAPIKey{})
	cols := map[string]bool{}
	for _, col := range storedColumns(parsed) {
		cols[col] = true
	}
	for _, want := range []string{"instance_id", "api_key", "source_type", "access_provider"} {
		if !cols[want] {
			t.Fatalf("cpa_api_keys lost column %q", want)
		}
	}
	var index *schema.Index
	for _, candidate := range parsed.ParseIndexes() {
		if candidate.Name == "uniq_cpa_api_keys_instance_api_key" {
			index = candidate
		}
	}
	if index == nil {
		t.Fatal("unique index uniq_cpa_api_keys_instance_api_key is gone")
	}
	if index.Class != "UNIQUE" || len(index.Fields) != 2 ||
		index.Fields[0].DBName != "instance_id" || index.Fields[1].DBName != "api_key" {
		t.Fatalf("unique index must be (instance_id, api_key), got class=%q fields=%+v", index.Class, index.Fields)
	}
}
