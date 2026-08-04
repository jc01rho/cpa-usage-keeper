package migration

import (
	"time"

	"gorm.io/gorm"
)

// legacyCPAAPIKey preserves the schema contract of this historical migration.
// Later instance scoping is applied only by 20260803_keeper_instances.
type legacyCPAAPIKey struct {
	ID           int64  `gorm:"primaryKey"`
	APIKey       string `gorm:"uniqueIndex:uniq_cpa_api_keys_api_key"`
	DisplayKey   string
	KeyAlias     string
	IsDeleted    bool       `gorm:"index:idx_cpa_api_keys_is_deleted"`
	LastSyncedAt *time.Time `gorm:"serializer:storageTime"`
	CreatedAt    time.Time  `gorm:"serializer:storageTime"`
	UpdatedAt    time.Time  `gorm:"serializer:storageTime"`
}

func (legacyCPAAPIKey) TableName() string { return "cpa_api_keys" }

func createCPAAPIKeysMigration(tx *gorm.DB) error {
	return tx.AutoMigrate(&legacyCPAAPIKey{})
}
