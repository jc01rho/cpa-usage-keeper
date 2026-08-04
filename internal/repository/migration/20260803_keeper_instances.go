package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

var keeperInstanceTables = []string{
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

// keeperInstancesMigration is intentionally transaction-wrapped by the normal
// migration runner. SQLite DDL participates in that transaction, so a failure
// rolls back the instance row, every added column/index, and the version mark.
// The framework is forward-only: rollback is restore-from-backup or run the old
// binary against a pre-migration copy, never a destructive down migration.
func keeperInstancesMigration(tx *gorm.DB) error {
	if err := tx.AutoMigrate(
		&entities.CPAInstance{},
		&entities.CPAInstanceCredential{},
		&entities.CPAUsageDelivery{},
		&entities.CPAUsageStreamWatermark{},
	); err != nil {
		return fmt.Errorf("create keeper instance schema: %w", err)
	}
	if err := tx.Exec(`
		INSERT OR IGNORE INTO cpa_instances (id, display_name, enabled, created_at, updated_at)
		VALUES (?, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, entities.LegacyCPAInstanceID, entities.LegacyCPAInstanceName).Error; err != nil {
		return fmt.Errorf("seed legacy CPA instance: %w", err)
	}

	for _, table := range keeperInstanceTables {
		if !tx.Migrator().HasTable(table) {
			return fmt.Errorf("add instance namespace: required table %s is missing", table)
		}
		if !tx.Migrator().HasColumn(table, "instance_id") {
			statement := fmt.Sprintf(
				"ALTER TABLE %s ADD COLUMN instance_id TEXT NOT NULL DEFAULT '%s'",
				table,
				entities.LegacyCPAInstanceID,
			)
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("add %s.instance_id: %w", table, err)
			}
		}
		if err := tx.Exec("UPDATE "+table+" SET instance_id = ? WHERE instance_id IS NULL OR instance_id = ''", entities.LegacyCPAInstanceID).Error; err != nil {
			return fmt.Errorf("backfill %s.instance_id: %w", table, err)
		}
	}

	statements := []string{
		`DROP INDEX IF EXISTS uniq_usage_identities_type_identity`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_usage_identities_instance_type_identity ON usage_identities(instance_id, auth_type, identity)`,
		`DROP INDEX IF EXISTS uniq_cpa_api_keys_api_key`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_cpa_api_keys_instance_api_key ON cpa_api_keys(instance_id, api_key)`,
		`DROP INDEX IF EXISTS uniq_usage_overview_hourly_stats_dimensions`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_usage_overview_hourly_stats_dimensions ON usage_overview_hourly_stats(instance_id, bucket_start, api_group_key, model, auth_index, model_alias, service_tier, response_service_tier, reasoning_effort, endpoint, executor_type)`,
		`DROP INDEX IF EXISTS uniq_usage_overview_daily_stats_dimensions`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_usage_overview_daily_stats_dimensions ON usage_overview_daily_stats(instance_id, bucket_start, api_group_key, model, auth_index, model_alias, service_tier, response_service_tier, reasoning_effort, endpoint, executor_type)`,
		`DROP INDEX IF EXISTS uniq_usage_activity_stats_grain_start_api`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_usage_activity_stats_instance_grain_start_api ON usage_activity_stats(instance_id, grain, bucket_start, api_group_key)`,
		`DROP INDEX IF EXISTS uniq_usage_latency_stats_bucket_api`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uniq_usage_latency_stats_instance_bucket_api ON usage_latency_stats(instance_id, bucket_type, bucket_start, api_group_key)`,
	}
	for _, statement := range statements {
		if err := tx.Exec(statement).Error; err != nil {
			return fmt.Errorf("create instance-scoped index: %w", err)
		}
	}

	if err := rebuildUsageAggregationCheckpoints(tx); err != nil {
		return err
	}
	if err := rebuildLocalRankingPeriodStats(tx); err != nil {
		return err
	}
	return EnsureCPAInstanceIntegrity(tx)
}

func rebuildUsageAggregationCheckpoints(tx *gorm.DB) error {
	statements := []string{
		`CREATE TABLE usage_aggregation_checkpoints_instance_new (
			instance_id TEXT NOT NULL,
			name TEXT NOT NULL CHECK (name IN ('overview','activity','latency')),
			last_aggregated_usage_event_id INTEGER NOT NULL DEFAULT 0,
			stats_updated_at DATETIME,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			PRIMARY KEY(instance_id, name)
		)`,
		`INSERT INTO usage_aggregation_checkpoints_instance_new (instance_id, name, last_aggregated_usage_event_id, stats_updated_at, created_at, updated_at)
		 SELECT instance_id, name, last_aggregated_usage_event_id, stats_updated_at, created_at, updated_at FROM usage_aggregation_checkpoints`,
		`DROP TABLE usage_aggregation_checkpoints`,
		`ALTER TABLE usage_aggregation_checkpoints_instance_new RENAME TO usage_aggregation_checkpoints`,
	}
	return runKeeperInstanceRebuildStatements(tx, "usage aggregation checkpoints", statements)
}

func rebuildLocalRankingPeriodStats(tx *gorm.DB) error {
	statements := []string{
		`CREATE TABLE local_ranking_period_stats_instance_new (
			instance_id TEXT NOT NULL,
			period_kind TEXT NOT NULL CHECK (period_kind IN ('day','month')),
			period_key TEXT NOT NULL,
			api_key_id INTEGER NOT NULL,
			request_count INTEGER NOT NULL DEFAULT 0,
			success_count INTEGER NOT NULL DEFAULT 0,
			failure_count INTEGER NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			ttft_sum_ms INTEGER NOT NULL DEFAULT 0,
			ttft_sample_count INTEGER NOT NULL DEFAULT 0,
			latency_sum_ms INTEGER NOT NULL DEFAULT 0,
			latency_sample_count INTEGER NOT NULL DEFAULT 0,
			peak_5m_request_count INTEGER NOT NULL DEFAULT 0,
			peak_5m_total_tokens INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME NOT NULL,
			PRIMARY KEY(instance_id, period_kind, period_key, api_key_id)
		)`,
		`INSERT INTO local_ranking_period_stats_instance_new (
			instance_id, period_kind, period_key, api_key_id, request_count, success_count, failure_count,
			input_tokens, cache_read_tokens, total_tokens, ttft_sum_ms, ttft_sample_count, latency_sum_ms,
			latency_sample_count, peak_5m_request_count, peak_5m_total_tokens, updated_at
		) SELECT instance_id, period_kind, period_key, api_key_id, request_count, success_count, failure_count,
			input_tokens, cache_read_tokens, total_tokens, ttft_sum_ms, ttft_sample_count, latency_sum_ms,
			latency_sample_count, peak_5m_request_count, peak_5m_total_tokens, updated_at
		  FROM local_ranking_period_stats`,
		`DROP TABLE local_ranking_period_stats`,
		`ALTER TABLE local_ranking_period_stats_instance_new RENAME TO local_ranking_period_stats`,
		`CREATE INDEX idx_local_ranking_period_stats_api_key ON local_ranking_period_stats(api_key_id)`,
	}
	return runKeeperInstanceRebuildStatements(tx, "local ranking period stats", statements)
}

func runKeeperInstanceRebuildStatements(tx *gorm.DB, name string, statements []string) error {
	for _, statement := range statements {
		if err := tx.Exec(statement).Error; err != nil {
			return fmt.Errorf("rebuild %s: %w", name, err)
		}
	}
	return nil
}
