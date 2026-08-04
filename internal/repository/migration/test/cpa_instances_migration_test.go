package test

import (
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/repository/migration"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	keeperInstancesMigrationVersion = "20260803_keeper_instances"
	legacyInstanceID                = "00000000-0000-7000-8000-000000000000"
	secondInstanceID                = "0198aa10-4d88-7a20-8f4e-8c8de4a9cb11"
)

func TestKeeperInstancesMigrationBackfillsLegacyRowsLosslessly(t *testing.T) {
	db := openKeeperInstanceMigrationDB(t, "lossless.db")
	createPreKeeperInstanceSchema(t, db, true)
	seedPreKeeperInstanceRows(t, db)
	markOnlyKeeperInstancesMigrationPending(t, db)

	before := captureKeeperInstanceFixture(t, db)
	if err := migration.Run(db); err != nil {
		t.Fatalf("run keeper instances migration: %v", err)
	}
	after := captureKeeperInstanceFixture(t, db)

	if before != after {
		t.Fatalf("migration changed legacy fixture values:\n before=%+v\n after=%+v", before, after)
	}
	assertSingleLegacyInstance(t, db)
	assertAllSourceTablesBackfilled(t, db)
	assertInstanceScopedConstraints(t, db)
	assertOrphanInstanceIDsRejected(t, db)
	assertInstanceIntegrityTriggers(t, db)
	assertReferencedInstanceDeleteRejected(t, db)
	assertKeeperInstancesMigrationApplied(t, db, true)
}

func TestKeeperInstancesMigrationIsIdempotent(t *testing.T) {
	db := openKeeperInstanceMigrationDB(t, "idempotent.db")
	createPreKeeperInstanceSchema(t, db, true)
	seedPreKeeperInstanceRows(t, db)
	markOnlyKeeperInstancesMigrationPending(t, db)

	if err := migration.Run(db); err != nil {
		t.Fatalf("first keeper instances migration: %v", err)
	}
	first := captureKeeperInstanceFixture(t, db)
	if err := migration.Run(db); err != nil {
		t.Fatalf("second keeper instances migration: %v", err)
	}
	second := captureKeeperInstanceFixture(t, db)
	if first != second {
		t.Fatalf("rerun changed migrated fixture:\n first=%+v\n second=%+v", first, second)
	}
	assertSingleLegacyInstance(t, db)
	assertKeeperInstancesMigrationApplied(t, db, true)
}

func TestKeeperInstancesMigrationRollsBackOnMidMigrationFailure(t *testing.T) {
	db := openKeeperInstanceMigrationDB(t, "rollback.db")
	createPreKeeperInstanceSchema(t, db, true)
	seedPreKeeperInstanceRows(t, db)
	markOnlyKeeperInstancesMigrationPending(t, db)

	// The deliberately malformed late table makes ALTER TABLE fail only after
	// earlier instance table/column work has executed inside the migration.
	if err := db.Exec(`DROP TABLE local_ranking_period_stats`).Error; err != nil {
		t.Fatalf("drop late migration table: %v", err)
	}
	if err := db.Exec(`CREATE VIEW local_ranking_period_stats AS SELECT 'day' AS period_kind, '2026-08-03' AS period_key, 1 AS api_key_id`).Error; err != nil {
		t.Fatalf("create malformed late migration view: %v", err)
	}
	if err := migration.Run(db); err == nil {
		t.Fatal("expected keeper instances migration failure")
	}

	if db.Migrator().HasTable("cpa_instances") {
		t.Fatal("cpa_instances survived rolled-back migration")
	}
	for _, table := range keeperInstanceSourceTables[:len(keeperInstanceSourceTables)-2] {
		if db.Migrator().HasColumn(table, "instance_id") {
			t.Fatalf("%s.instance_id survived rolled-back migration", table)
		}
	}
	assertKeeperInstancesMigrationApplied(t, db, false)
}

func openKeeperInstanceMigrationDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), name)+"?_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open keeper instance migration database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load keeper instance migration sql database: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func markOnlyKeeperInstancesMigrationPending(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := migration.MarkAllAsApplied(db); err != nil {
		t.Fatalf("mark historical migrations applied: %v", err)
	}
	if err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", keeperInstancesMigrationVersion).Error; err != nil {
		t.Fatalf("mark keeper instances migration pending: %v", err)
	}
}

func assertKeeperInstancesMigrationApplied(t *testing.T, db *gorm.DB, want bool) {
	t.Helper()
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", keeperInstancesMigrationVersion).Count(&count).Error; err != nil {
		t.Fatalf("count keeper instances migration: %v", err)
	}
	if (count == 1) != want {
		t.Fatalf("keeper instances migration applied=%t, count=%d", want, count)
	}
}

func assertSingleLegacyInstance(t *testing.T, db *gorm.DB) {
	t.Helper()
	var rows []struct {
		ID          string
		DisplayName string
		Enabled     bool
	}
	if err := db.Table("cpa_instances").Find(&rows).Error; err != nil {
		t.Fatalf("load cpa instances: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != legacyInstanceID || rows[0].DisplayName != "Legacy" || !rows[0].Enabled {
		t.Fatalf("unexpected legacy instance rows: %+v", rows)
	}
}

func assertAllSourceTablesBackfilled(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, table := range keeperInstanceSourceTables {
		if !db.Migrator().HasColumn(table, "instance_id") {
			t.Fatalf("missing %s.instance_id", table)
		}
		var nullCount int64
		if err := db.Table(table).Where("instance_id IS NULL OR instance_id = ''").Count(&nullCount).Error; err != nil {
			t.Fatalf("count blank %s.instance_id: %v", table, err)
		}
		if nullCount != 0 {
			t.Fatalf("%s has %d blank instance IDs", table, nullCount)
		}
		var foreignCount int64
		if err := db.Table(table).Where("instance_id <> ?", legacyInstanceID).Count(&foreignCount).Error; err != nil {
			t.Fatalf("count nonlegacy %s.instance_id: %v", table, err)
		}
		if foreignCount != 0 {
			t.Fatalf("%s has %d nonlegacy rows", table, foreignCount)
		}
	}
}

func assertOrphanInstanceIDsRejected(t *testing.T, db *gorm.DB) {
	t.Helper()
	const orphanInstanceID = "0198aa10-4d88-7a20-8f4e-000000000099"
	for _, table := range keeperInstanceIntegrityTables {
		statement := "INSERT INTO " + table + " (instance_id) VALUES (?)"
		if err := db.Exec(statement, orphanInstanceID).Error; err == nil {
			t.Fatalf("orphan %s insert should be rejected", table)
		}
	}

	now := "2026-08-03T12:34:56.789Z"
	if err := db.Exec(`INSERT INTO cpa_instance_credentials (id, instance_id, name, token_hash, scopes, created_at, updated_at) VALUES ('credential-1', ?, 'test', 'hash-1', 'ingest', ?, ?)`, legacyInstanceID, now, now).Error; err != nil {
		t.Fatalf("seed instance credential: %v", err)
	}
	if err := db.Exec(`INSERT INTO cpa_usage_deliveries (instance_id, stream_id, sequence, payload_digest, inbox_id, accepted_at) VALUES (?, 'stream-1', 1, X'01', 1, ?)`, legacyInstanceID, now).Error; err != nil {
		t.Fatalf("seed usage delivery: %v", err)
	}
	if err := db.Exec(`INSERT INTO cpa_usage_stream_watermarks (instance_id, stream_id, acknowledged_through, created_at, updated_at) VALUES (?, 'stream-1', 1, ?, ?)`, legacyInstanceID, now, now).Error; err != nil {
		t.Fatalf("seed usage stream watermark: %v", err)
	}
	for _, table := range keeperInstanceIntegrityTables {
		statement := "UPDATE " + table + " SET instance_id = ? WHERE instance_id = ?"
		if err := db.Exec(statement, orphanInstanceID, legacyInstanceID).Error; err == nil {
			t.Fatalf("orphan %s instance move should be rejected", table)
		}
	}
}

func assertInstanceIntegrityTriggers(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, table := range keeperInstanceIntegrityTables {
		for _, suffix := range []string{"insert", "update", "delete"} {
			name := "fk_" + table + "_instance_id_" + suffix
			var count int64
			if err := db.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = ?`, name).Scan(&count).Error; err != nil {
				t.Fatalf("count %s: %v", name, err)
			}
			if count != 1 {
				t.Fatalf("missing instance integrity trigger %s", name)
			}
		}
	}
}

func assertReferencedInstanceDeleteRejected(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`DELETE FROM cpa_instances WHERE id = ?`, legacyInstanceID).Error; err == nil {
		t.Fatal("referenced legacy instance delete should be rejected")
	}
}

func assertInstanceScopedConstraints(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := "2026-08-03T12:34:56.789Z"
	if err := db.Exec(`INSERT INTO cpa_instances (id, display_name, enabled, created_at, updated_at) VALUES (?, 'Second', 1, ?, ?)`, secondInstanceID, now, now).Error; err != nil {
		t.Fatalf("insert second instance: %v", err)
	}

	if err := db.Exec(`INSERT INTO usage_identities (instance_id, auth_type, identity, name) VALUES (?, 1, 'shared-auth', 'other')`, secondInstanceID).Error; err != nil {
		t.Fatalf("same identity in second instance should be allowed: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_identities (instance_id, auth_type, identity, name) VALUES (?, 1, 'shared-auth', 'duplicate')`, legacyInstanceID).Error; err == nil {
		t.Fatal("same identity in one instance should be rejected")
	}
	if err := db.Exec(`INSERT INTO cpa_api_keys (instance_id, api_key, display_key) VALUES (?, 'shared-key', 'other')`, secondInstanceID).Error; err != nil {
		t.Fatalf("same API key in second instance should be allowed: %v", err)
	}
	if err := db.Exec(`INSERT INTO cpa_api_keys (instance_id, api_key, display_key) VALUES (?, 'shared-key', 'duplicate')`, legacyInstanceID).Error; err == nil {
		t.Fatal("same API key in one instance should be rejected")
	}
	if err := db.Exec(`INSERT INTO usage_overview_hourly_stats (instance_id, bucket_start, api_group_key, model, auth_index, model_alias, service_tier, response_service_tier, reasoning_effort, endpoint, executor_type, created_at, updated_at) SELECT ?, bucket_start, api_group_key, model, auth_index, model_alias, service_tier, response_service_tier, reasoning_effort, endpoint, executor_type, created_at, updated_at FROM usage_overview_hourly_stats WHERE instance_id = ? LIMIT 1`, secondInstanceID, legacyInstanceID).Error; err != nil {
		t.Fatalf("same hourly dimensions in second instance should be allowed: %v", err)
	}
	if err := db.Exec(`INSERT INTO usage_overview_hourly_stats (instance_id, bucket_start, api_group_key, model, auth_index, model_alias, service_tier, response_service_tier, reasoning_effort, endpoint, executor_type, created_at, updated_at) SELECT instance_id, bucket_start, api_group_key, model, auth_index, model_alias, service_tier, response_service_tier, reasoning_effort, endpoint, executor_type, created_at, updated_at FROM usage_overview_hourly_stats WHERE instance_id = ? LIMIT 1`, legacyInstanceID).Error; err == nil {
		t.Fatal("same hourly dimensions in one instance should be rejected")
	}
	if err := db.Exec(`INSERT INTO usage_aggregation_checkpoints (instance_id, name, last_aggregated_usage_event_id, created_at, updated_at) VALUES (?, 'overview', 0, ?, ?)`, secondInstanceID, now, now).Error; err != nil {
		t.Fatalf("same checkpoint name in second instance should be allowed: %v", err)
	}
}

var keeperInstanceIntegrityTables = []string{
	"redis_usage_inboxes",
	"usage_events",
	"usage_events_archive",
	"usage_identities",
	"cpa_api_keys",
	"usage_overview_hourly_stats",
	"usage_overview_daily_stats",
	"usage_activity_stats",
	"usage_latency_stats",
	"usage_aggregation_checkpoints",
	"local_ranking_period_stats",
	"cpa_instance_credentials",
	"cpa_usage_deliveries",
	"cpa_usage_stream_watermarks",
}

var keeperInstanceSourceTables = []string{
	"redis_usage_inboxes",
	"usage_events",
	"usage_events_archive",
	"usage_identities",
	"cpa_api_keys",
	"usage_overview_hourly_stats",
	"usage_overview_daily_stats",
	"usage_activity_stats",
	"usage_latency_stats",
	"usage_aggregation_checkpoints",
	"local_ranking_period_stats",
}

type keeperInstanceFixture struct {
	InboxCount, EventCount, ArchiveCount, IdentityCount, APIKeyCount int64
	HourlyCount, DailyCount, ActivityCount, LatencyCount             int64
	CheckpointCount, RankingCount                                    int64
	EventTokenTotal, ArchiveTokenTotal                               int64
	HourlyRequests, DailyRequests, ActivityTokens, LatencySamples    int64
	IdentityRequests, RankingRequests, CheckpointCursor              int64
	DuplicateRequestIDs                                              int64
}

func captureKeeperInstanceFixture(t *testing.T, db *gorm.DB) keeperInstanceFixture {
	t.Helper()
	result := keeperInstanceFixture{}
	countTable(t, db, "redis_usage_inboxes", &result.InboxCount)
	countTable(t, db, "usage_events", &result.EventCount)
	countTable(t, db, "usage_events_archive", &result.ArchiveCount)
	countTable(t, db, "usage_identities", &result.IdentityCount)
	countTable(t, db, "cpa_api_keys", &result.APIKeyCount)
	countTable(t, db, "usage_overview_hourly_stats", &result.HourlyCount)
	countTable(t, db, "usage_overview_daily_stats", &result.DailyCount)
	countTable(t, db, "usage_activity_stats", &result.ActivityCount)
	countTable(t, db, "usage_latency_stats", &result.LatencyCount)
	countTable(t, db, "usage_aggregation_checkpoints", &result.CheckpointCount)
	countTable(t, db, "local_ranking_period_stats", &result.RankingCount)
	sumColumn(t, db, "usage_events", "total_tokens", &result.EventTokenTotal)
	sumColumn(t, db, "usage_events_archive", "total_tokens", &result.ArchiveTokenTotal)
	sumColumn(t, db, "usage_overview_hourly_stats", "request_count", &result.HourlyRequests)
	sumColumn(t, db, "usage_overview_daily_stats", "request_count", &result.DailyRequests)
	sumColumn(t, db, "usage_activity_stats", "total_tokens", &result.ActivityTokens)
	sumColumn(t, db, "usage_latency_stats", "sample_count", &result.LatencySamples)
	sumColumn(t, db, "usage_identities", "total_requests", &result.IdentityRequests)
	sumColumn(t, db, "local_ranking_period_stats", "request_count", &result.RankingRequests)
	sumColumn(t, db, "usage_aggregation_checkpoints", "last_aggregated_usage_event_id", &result.CheckpointCursor)
	if err := db.Table("usage_events").Where("request_id = ?", "duplicate-request").Count(&result.DuplicateRequestIDs).Error; err != nil {
		t.Fatalf("count duplicate request IDs: %v", err)
	}
	return result
}

func countTable(t *testing.T, db *gorm.DB, table string, destination *int64) {
	t.Helper()
	if err := db.Table(table).Count(destination).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
}

func sumColumn(t *testing.T, db *gorm.DB, table, column string, destination *int64) {
	t.Helper()
	if err := db.Table(table).Select("COALESCE(SUM(" + column + "), 0)").Scan(destination).Error; err != nil {
		t.Fatalf("sum %s.%s: %v", table, column, err)
	}
}

func seedPreKeeperInstanceRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Date(2026, 8, 3, 12, 34, 56, 789000000, time.UTC)
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO redis_usage_inboxes (id, source, message_hash, raw_message, status, attempt_count, usage_event_key, popped_at, created_at, updated_at) VALUES (1, 'redis_pull:usage', 'h1', '{}', 'pending', 2, 'duplicate-request', ?, ?, ?), (2, 'redis_pull:usage', 'h2', '{}', 'processed', 1, 'duplicate-request', ?, ?, ?)`, []any{now, now, now, now, now, now}},
		{`INSERT INTO usage_events (id, event_key, request_id, api_group_key, model, timestamp, total_tokens, created_at) VALUES (10, 'duplicate-request', 'duplicate-request', 'shared-key', 'model-a', ?, 17, ?), (11, 'duplicate-request', 'duplicate-request', 'shared-key', 'model-b', ?, 23, ?)`, []any{now, now, now, now}},
		{`INSERT INTO usage_events_archive (id, event_key, request_id, api_group_key, model, timestamp, total_tokens, created_at) VALUES (9, 'archived', 'archived', 'shared-key', 'model-a', ?, 31, ?)`, []any{now, now}},
		{`INSERT INTO usage_identities (id, name, auth_type, identity, total_requests, is_deleted, created_at, updated_at) VALUES (20, 'active', 1, 'shared-auth', 5, 0, ?, ?), (21, 'deleted', 2, 'deleted-auth', 7, 1, ?, ?)`, []any{now, now, now, now}},
		{`INSERT INTO cpa_api_keys (id, api_key, display_key, is_deleted, created_at, updated_at) VALUES (30, 'shared-key', 'sk-***key', 0, ?, ?), (31, 'deleted-key', 'sk-***gone', 1, ?, ?)`, []any{now, now, now, now}},
		{`INSERT INTO usage_overview_hourly_stats (id, bucket_start, api_group_key, model, auth_index, model_alias, service_tier, response_service_tier, reasoning_effort, endpoint, executor_type, request_count, total_tokens, created_at, updated_at) VALUES (40, ?, 'shared-key', 'model-a', 'auth', '', '', '', '', '', '', 3, 40, ?, ?)`, []any{now, now, now}},
		{`INSERT INTO usage_overview_daily_stats (id, bucket_start, api_group_key, model, auth_index, model_alias, service_tier, response_service_tier, reasoning_effort, endpoint, executor_type, request_count, total_tokens, created_at, updated_at) VALUES (41, ?, 'shared-key', 'model-a', 'auth', '', '', '', '', '', '', 4, 50, ?, ?)`, []any{now, now, now}},
		{`INSERT INTO usage_activity_stats (id, grain, bucket_start, bucket_end, api_group_key, success_count, failure_count, total_tokens, created_at, updated_at) VALUES (50, 'short', ?, ?, 'shared-key', 2, 1, 60, ?, ?)`, []any{now, now.Add(time.Hour), now, now}},
		{`INSERT INTO usage_latency_stats (id, bucket_type, bucket_start, api_group_key, sample_count, max_ttft_ms, max_latency_ms, format_version, ttft_sketch, latency_sketch, sample_points, created_at, updated_at) VALUES (60, 'hour', ?, 'shared-key', 2, 10, 20, 1, X'01', X'02', X'03', ?, ?)`, []any{now, now, now}},
		{`INSERT INTO usage_aggregation_checkpoints (name, last_aggregated_usage_event_id, created_at, updated_at) VALUES ('overview', 11, ?, ?), ('activity', 10, ?, ?), ('latency', 9, ?, ?)`, []any{now, now, now, now, now, now}},
		{`INSERT INTO local_ranking_period_stats (period_kind, period_key, api_key_id, request_count, updated_at) VALUES ('day', '2026-08-03', 30, 8, ?)`, []any{now}},
	}
	for _, statement := range statements {
		if err := db.Exec(statement.sql, statement.args...).Error; err != nil {
			t.Fatalf("seed keeper instance fixture: %v\nSQL: %s", err, statement.sql)
		}
	}
}
