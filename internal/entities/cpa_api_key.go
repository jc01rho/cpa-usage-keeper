package entities

import "time"

// CPAAPIKey 保存 CPA 管理接口同步到本地的 API-Key，完整 key 仅供后端内部查询使用。
type CPAAPIKey struct {
	ID                   int64  `gorm:"primaryKey"`
	InstanceID           string `gorm:"type:text;not null;default:00000000-0000-7000-8000-000000000000;uniqueIndex:uniq_cpa_api_keys_instance_api_key,priority:1;index:idx_cpa_api_keys_instance_id"`
	APIKey               string `gorm:"uniqueIndex:uniq_cpa_api_keys_instance_api_key,priority:2"`
	DisplayKey           string
	KeyAlias             string
	LocalRankingAvatarID *uint8
	IsDeleted            bool       `gorm:"index:idx_cpa_api_keys_is_deleted"`
	LastSyncedAt         *time.Time `gorm:"serializer:storageTime"`
	CreatedAt            time.Time  `gorm:"serializer:storageTime"`
	UpdatedAt            time.Time  `gorm:"serializer:storageTime"`
}
