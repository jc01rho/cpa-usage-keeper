package migration

import (
	"fmt"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

// removeRankingMigration 在默认事务中精确清退废弃排名结构与设置，保留共享 Key 和其他配置。
func removeRankingMigration(tx *gorm.DB) error {
	// fork: cpa_instances의 delete 트리거가 이 테이블을 참조하므로 테이블보다 먼저 지워야 이후 ALTER TABLE이 깨지지 않는다.
	if err := dropRankingInstanceTriggerMigration(tx); err != nil {
		return err
	}
	if tx.Migrator().HasTable("local_ranking_period_stats") {
		if err := tx.Migrator().DropTable("local_ranking_period_stats"); err != nil {
			return fmt.Errorf("drop local ranking period stats: %w", err)
		}
	}
	if err := dropColumnIfExists(tx, "cpa_api_keys", "local_ranking_avatar_id", "cpa_api_keys"); err != nil {
		return err
	}
	if err := tx.Where("setting_key IN ?", []string{"ranking.identity", "ranking.local.period_state"}).Delete(&entities.AppSetting{}).Error; err != nil {
		return fmt.Errorf("delete ranking settings: %w", err)
	}
	return nil
}
