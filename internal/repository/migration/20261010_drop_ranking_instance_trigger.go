package migration

import (
	"fmt"

	"gorm.io/gorm"
)

// dropRankingInstanceTriggerMigration is fork-only: the multi-instance integrity layer attached a
// BEFORE DELETE trigger on cpa_instances that reads local_ranking_period_stats. Upstream
// 20261009_remove_ranking drops that table, so the orphaned trigger would make every instance
// delete fail with "no such table". The table's own insert/update triggers vanish with the table.
func dropRankingInstanceTriggerMigration(tx *gorm.DB) error {
	if err := tx.Exec("DROP TRIGGER IF EXISTS fk_local_ranking_period_stats_instance_id_delete").Error; err != nil {
		return fmt.Errorf("drop local ranking instance delete trigger: %w", err)
	}
	return nil
}
