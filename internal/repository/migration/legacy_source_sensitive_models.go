package migration

import (
	"time"

	"cpa-usage-keeper/internal/entities"
)

const legacyUsageAggregationEventProjectionColumns = "id, api_group_key, model, model_alias, auth_index, service_tier, response_service_tier, reasoning_effort, endpoint, executor_type, timestamp, failed, generate, latency_ms, ttft_ms, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_read_tokens, cache_creation_tokens, total_tokens"

type legacyUsageActivityStat struct {
	ID                  int64                       `gorm:"primaryKey"`
	Grain               entities.UsageActivityGrain `gorm:"type:text;not null;check:chk_usage_activity_stats_grain,grain IN ('short','medium','long','daily');uniqueIndex:uniq_usage_activity_stats_grain_start_api,priority:1"`
	BucketStart         time.Time                   `gorm:"serializer:sortableTime;not null;uniqueIndex:uniq_usage_activity_stats_grain_start_api,priority:2"`
	BucketEnd           time.Time                   `gorm:"serializer:sortableTime;not null"`
	APIGroupKey         string                      `gorm:"not null;uniqueIndex:uniq_usage_activity_stats_grain_start_api,priority:3"`
	SuccessCount        int64
	FailureCount        int64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
	CreatedAt           time.Time `gorm:"serializer:storageTime;not null"`
	UpdatedAt           time.Time `gorm:"serializer:storageTime;not null"`
}

func (legacyUsageActivityStat) TableName() string { return "usage_activity_stats" }

type legacyUsageAggregationCheckpoint struct {
	Name                       entities.UsageAggregationCheckpointName `gorm:"type:text;primaryKey;check:chk_usage_aggregation_checkpoints_name,name IN ('overview','activity','latency')"`
	LastAggregatedUsageEventID int64                                   `gorm:"not null;default:0"`
	StatsUpdatedAt             *time.Time                              `gorm:"serializer:storageTime"`
	CreatedAt                  time.Time                               `gorm:"serializer:storageTime;not null"`
	UpdatedAt                  time.Time                               `gorm:"serializer:storageTime;not null"`
}

func (legacyUsageAggregationCheckpoint) TableName() string { return "usage_aggregation_checkpoints" }

type legacyUsageLatencyStat struct {
	ID            int64                           `gorm:"primaryKey"`
	BucketType    entities.UsageLatencyBucketType `gorm:"type:text;not null;uniqueIndex:uniq_usage_latency_stats_bucket_api,priority:1"`
	BucketStart   time.Time                       `gorm:"serializer:storageTime;not null;uniqueIndex:uniq_usage_latency_stats_bucket_api,priority:2"`
	APIGroupKey   string                          `gorm:"not null;uniqueIndex:uniq_usage_latency_stats_bucket_api,priority:3"`
	SampleCount   int64
	MaxTTFTMS     int64 `gorm:"column:max_ttft_ms"`
	MaxLatencyMS  int64
	FormatVersion int
	TTFTSketch    []byte    `gorm:"type:blob;not null"`
	LatencySketch []byte    `gorm:"type:blob;not null"`
	SamplePoints  []byte    `gorm:"type:blob;not null"`
	CreatedAt     time.Time `gorm:"serializer:storageTime;not null"`
	UpdatedAt     time.Time `gorm:"serializer:storageTime;not null"`
}

func (legacyUsageLatencyStat) TableName() string { return "usage_latency_stats" }

type legacyUsageEventArchive struct {
	ID                  int64 `gorm:"primaryKey;autoIncrement:false"`
	EventKey            string
	APIGroupKey         string
	Provider            string
	Endpoint            string
	AuthType            string
	RequestID           string
	ClientIP            *string
	XForwardedFor       *string
	UserAgent           *string
	Model               string
	ModelAlias          *string
	ReasoningEffort     string
	ServiceTier         string
	ResponseServiceTier string
	ExecutorType        string
	Timestamp           time.Time `gorm:"serializer:storageTime"`
	Source              string
	AuthIndex           string
	Failed              bool
	Generate            *bool `gorm:"not null;default:true"`
	LatencyMS           int64
	TTFTMS              *int64
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
	CreatedAt           time.Time `gorm:"serializer:storageTime"`
}

func (legacyUsageEventArchive) TableName() string { return "usage_events_archive" }

type legacyLocalRankingPeriodStat struct {
	PeriodKind         entities.LocalRankingPeriodKind `gorm:"type:text;primaryKey"`
	PeriodKey          string                          `gorm:"type:text;primaryKey"`
	APIKeyID           int64                           `gorm:"primaryKey;index:idx_local_ranking_period_stats_api_key"`
	RequestCount       int64
	SuccessCount       int64
	FailureCount       int64
	InputTokens        int64
	CacheReadTokens    int64
	TotalTokens        int64
	TTFTSumMS          int64 `gorm:"column:ttft_sum_ms"`
	TTFTSampleCount    int64 `gorm:"column:ttft_sample_count"`
	LatencySumMS       int64
	LatencySampleCount int64
	Peak5MRequestCount int64     `gorm:"column:peak_5m_request_count"`
	Peak5MTotalTokens  int64     `gorm:"column:peak_5m_total_tokens"`
	UpdatedAt          time.Time `gorm:"serializer:storageTime;not null"`
}

func (legacyLocalRankingPeriodStat) TableName() string { return "local_ranking_period_stats" }
