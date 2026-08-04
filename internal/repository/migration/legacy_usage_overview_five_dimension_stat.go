package migration

import "time"

// These historical models pin the pre-instance final index shape for the
// 20260723 migration. Runtime entities evolve independently in later versions.
type legacyUsageOverviewHourlyStat struct {
	ID                                                                                                          int64     `gorm:"primaryKey"`
	BucketStart                                                                                                 time.Time `gorm:"serializer:storageTime;not null;uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:1"`
	APIGroupKey                                                                                                 string    `gorm:"not null;uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:2"`
	Model                                                                                                       string    `gorm:"not null;uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:3"`
	AuthIndex                                                                                                   string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:4"`
	ModelAlias                                                                                                  string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:5"`
	ServiceTier                                                                                                 string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:6"`
	ResponseServiceTier                                                                                         string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:7"`
	ReasoningEffort                                                                                             string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:8"`
	Endpoint                                                                                                    string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:9"`
	ExecutorType                                                                                                string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_hourly_stats_dimensions,priority:10"`
	RequestCount, SuccessCount, FailureCount                                                                    int64     `gorm:"not null;default:0"`
	InputTokens, OutputTokens, ReasoningTokens, CachedTokens, CacheReadTokens, CacheCreationTokens, TotalTokens int64     `gorm:"not null;default:0"`
	CreatedAt, UpdatedAt                                                                                        time.Time `gorm:"serializer:storageTime;not null"`
}

func (legacyUsageOverviewHourlyStat) TableName() string { return "usage_overview_hourly_stats" }

type legacyUsageOverviewDailyStat struct {
	ID                                                                                                          int64     `gorm:"primaryKey"`
	BucketStart                                                                                                 time.Time `gorm:"serializer:storageTime;not null;uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:1"`
	APIGroupKey                                                                                                 string    `gorm:"not null;uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:2"`
	Model                                                                                                       string    `gorm:"not null;uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:3"`
	AuthIndex                                                                                                   string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:4"`
	ModelAlias                                                                                                  string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:5"`
	ServiceTier                                                                                                 string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:6"`
	ResponseServiceTier                                                                                         string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:7"`
	ReasoningEffort                                                                                             string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:8"`
	Endpoint                                                                                                    string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:9"`
	ExecutorType                                                                                                string    `gorm:"not null;default:'';uniqueIndex:uniq_usage_overview_daily_stats_dimensions,priority:10"`
	RequestCount, SuccessCount, FailureCount                                                                    int64     `gorm:"not null;default:0"`
	InputTokens, OutputTokens, ReasoningTokens, CachedTokens, CacheReadTokens, CacheCreationTokens, TotalTokens int64     `gorm:"not null;default:0"`
	CreatedAt, UpdatedAt                                                                                        time.Time `gorm:"serializer:storageTime;not null"`
}

func (legacyUsageOverviewDailyStat) TableName() string { return "usage_overview_daily_stats" }
