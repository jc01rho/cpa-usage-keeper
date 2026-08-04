package migration

import (
	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

func keeperMetadataSnapshotsMigration(db *gorm.DB) error {
	return db.AutoMigrate(&entities.CPAMetadataSnapshot{})
}
