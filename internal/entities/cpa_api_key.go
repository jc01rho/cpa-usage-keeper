package entities

import "time"

const (
	CPAAPIKeySourceNative = "native"
	CPAAPIKeySourcePlugin = "plugin"
)

// PluginAPIKeyIdentity 只交付用量提交后登记需要的统一标识和完整认证来源。
type PluginAPIKeyIdentity struct {
	APIGroupKey    string
	AccessProvider string
}

// CPAAPIKey 保存原生密钥或插件统计身份，内部关联值仅供后端使用。
type CPAAPIKey struct {
	ID             int64      `gorm:"primaryKey"`
	InstanceID     string     `gorm:"type:text;not null;default:00000000-0000-7000-8000-000000000000;uniqueIndex:uniq_cpa_api_keys_instance_api_key,priority:1;index:idx_cpa_api_keys_instance_id"`
	APIKey         string     `gorm:"uniqueIndex:uniq_cpa_api_keys_instance_api_key,priority:2"`
	DisplayKey     string
	KeyAlias       string
	SourceType     string     `gorm:"not null;default:'native'"`
	AccessProvider string     `gorm:"not null;default:''"`
	IsDeleted      bool       `gorm:"index:idx_cpa_api_keys_is_deleted"`
	LastSyncedAt   *time.Time `gorm:"serializer:storageTime"`
	CreatedAt      time.Time  `gorm:"serializer:storageTime"`
	UpdatedAt      time.Time  `gorm:"serializer:storageTime"`
}
