package test

import (
	"time"

	"cpa-usage-keeper/internal/entities"
)

type legacyLatencyStatForTest struct {
	ID            int64                           `gorm:"primaryKey"`
	BucketType    entities.UsageLatencyBucketType `gorm:"uniqueIndex:uniq_usage_latency_stats_bucket_api,priority:1"`
	BucketStart   time.Time                       `gorm:"serializer:storageTime;uniqueIndex:uniq_usage_latency_stats_bucket_api,priority:2"`
	APIGroupKey   string                          `gorm:"uniqueIndex:uniq_usage_latency_stats_bucket_api,priority:3"`
	SampleCount   int64
	MaxTTFTMS     int64 `gorm:"column:max_ttft_ms"`
	MaxLatencyMS  int64
	FormatVersion int
	TTFTSketch    []byte `gorm:"type:blob;not null"`
	LatencySketch []byte `gorm:"type:blob;not null"`
	SamplePoints  []byte `gorm:"type:blob;not null"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (legacyLatencyStatForTest) TableName() string { return "usage_latency_stats" }
