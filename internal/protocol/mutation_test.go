package protocol_test

import (
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"

	"cpa-usage-keeper/internal/protocol"
)

// TestFixtureMutation is the manual-QA driver from the protocol contract
// section 13. It is skipped during normal runs; the contract's mutation
// commands invoke it as:
//
//	go test ./internal/protocol -run TestFixtureMutation -args <file> <expectedCode>
//
// It decodes a mutated copy of a fixture (never a checked-in fixture) with the
// strict decoder matching its wire shape and requires rejection with the exact
// stable code.
func TestFixtureMutation(t *testing.T) {
	args := flag.Args()
	if len(args) == 0 {
		t.Skip("no mutation arguments; run with -args <file> <expectedCode>")
	}
	if len(args) != 2 {
		t.Fatalf("want exactly 2 args <file> <expectedCode>, got %v", args)
	}
	path, expected := args[0], args[1]

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read mutated fixture: %v", err)
	}

	decodeErr := decodeByShape(path, data)
	if decodeErr == nil {
		t.Fatalf("mutated fixture %s was accepted; want rejection with code %q", path, expected)
	}
	if decodeErr.Code != expected {
		t.Fatalf("mutated fixture %s rejected with code %q, want %q", path, decodeErr.Code, expected)
	}
	t.Logf("rejected %s with expected code %q", path, decodeErr.Code)
}

// decodeByShape selects the strict decoder matching the document's wire shape.
func decodeByShape(path string, data []byte) *protocol.Error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		// Not even generic JSON (for example duplicate keys are preserved only
		// by strict decoders); fall through to the usage batch decoder, whose
		// strict scan must report the stable code.
		_, decErr := protocol.DecodeUsageBatch(data)
		return decErr
	}
	has := func(keys ...string) bool {
		for _, key := range keys {
			if _, ok := top[key]; ok {
				return true
			}
		}
		return false
	}

	switch {
	case has("error"):
		_, err := protocol.DecodeErrorEnvelope(data)
		return err
	case has("events", "streamId"):
		_, err := protocol.DecodeUsageBatch(data)
		return err
	case has("acknowledgedThrough"):
		_, err := protocol.DecodeUsageAck(data)
		return err
	case has("items", "revision"):
		_, err := protocol.DecodeMetadataSnapshot(data, categoryForPath(path, data))
		return err
	case has("settings"):
		_, err := protocol.DecodeSettingsPutRequest(data)
		return err
	case has("state"):
		_, err := protocol.DecodeStatusResponse(data)
		return err
	case has("ok"):
		_, err := protocol.DecodeConnectionTestResponse(data)
		return err
	case has("fingerprintSecretHex"):
		_, err := protocol.DecodeFingerprintVectors(data)
		return err
	case has("credential") && has("displayName"):
		_, err := protocol.DecodeCreateInstanceRequest(data)
		return err
	case has("instance") && has("credential"):
		_, err := protocol.DecodeInstanceRegistration(data)
		return err
	case has("instance"):
		_, err := protocol.DecodeIdentityResponse(data)
		return err
	default:
		_, err := protocol.DecodeUsageBatch(data)
		return err
	}
}

func categoryForPath(path string, data []byte) protocol.MetadataCategory {
	base := path
	if idx := strings.LastIndexByte(base, '/'); idx >= 0 {
		base = base[idx+1:]
	}
	switch {
	case strings.Contains(base, "api-keys"):
		return protocol.CategoryAPIKeys
	case strings.Contains(base, "provider-identities"):
		return protocol.CategoryProviderIdentities
	case strings.Contains(base, "auth-files"), strings.Contains(base, "empty-complete"), strings.Contains(base, "incomplete"), strings.Contains(base, "duplicate"), strings.Contains(base, "revision"):
		return protocol.CategoryAuthFiles
	}
	// Shape sniffing for non-conventional file names.
	var envelope struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && len(envelope.Items) > 0 {
		item := envelope.Items[0]
		if _, ok := item["fingerprint"]; ok {
			return protocol.CategoryAPIKeys
		}
		if _, ok := item["providerType"]; ok {
			return protocol.CategoryProviderIdentities
		}
	}
	return protocol.CategoryAuthFiles
}
