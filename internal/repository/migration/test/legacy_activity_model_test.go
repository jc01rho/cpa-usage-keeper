package test

import (
	"cpa-usage-keeper/internal/entities"
	"time"
)

type legacyActivityStatForTest struct {
	ID                  int64                       `gorm:"primaryKey"`
	Grain               entities.UsageActivityGrain `gorm:"uniqueIndex:uniq_usage_activity_stats_grain_start_api,priority:1"`
	BucketStart         time.Time                   `gorm:"uniqueIndex:uniq_usage_activity_stats_grain_start_api,priority:2"`
	BucketEnd           time.Time
	APIGroupKey         string `gorm:"uniqueIndex:uniq_usage_activity_stats_grain_start_api,priority:3"`
	SuccessCount        int64
	FailureCount        int64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (legacyActivityStatForTest) TableName() string { return "usage_activity_stats" }
