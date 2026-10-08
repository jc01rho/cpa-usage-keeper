package repository

import (
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

// Activity 原表不保存身份维度；供应商筛选使用事件细粒度和长期 daily 汇总。
func loadUsageActivityProviderRows(db *gorm.DB, filter dto.UsageQueryFilter, grain entities.UsageActivityGrain, start, end time.Time) ([]entities.UsageActivityStat, error) {
	if grain == entities.UsageActivityGrainDaily {
		query := applyUsageAPIKeyScope(db.Model(&entities.UsageOverviewDailyStat{}), filter)
		query = applyUsageAuthIndexScope(query, filter)
		if filter.InstanceID != "" {
			query = query.Where("instance_id = ?", filter.InstanceID)
		}
		var rows []usageOverviewStatProjection
		if err := query.Select("bucket_start, "+usageOverviewStatProjectionAggregateColumns).
			Where("bucket_start >= ? AND bucket_start < ?", timeutil.FormatStorageTime(start), timeutil.FormatStorageTime(end)).
			Group("bucket_start").Scan(&rows).Error; err != nil {
			return nil, err
		}
		result := make([]entities.UsageActivityStat, 0, len(rows))
		for _, row := range rows {
			bucket, err := UsageActivityBucketForTimestamp(grain, row.BucketStart)
			if err != nil {
				return nil, err
			}
			result = append(result, entities.UsageActivityStat{
				Grain: grain, BucketStart: bucket.Start, BucketEnd: bucket.End,
				SuccessCount: row.SuccessCount, FailureCount: row.FailureCount,
				InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, ReasoningTokens: row.ReasoningTokens,
				CacheReadTokens: row.CacheReadTokens, CacheCreationTokens: row.CacheCreationTokens, TotalTokens: row.TotalTokens,
			})
		}
		return result, nil
	}
	filter.StartTime, filter.EndTime, filter.EndExclusive = &start, &end, true
	query := applyUsageEventListQuery(db.Model(&entities.UsageEvent{}), filter).
		Select("timestamp, failed, input_tokens, output_tokens, reasoning_tokens, cache_read_tokens, cache_creation_tokens, total_tokens")
	rows, err := query.Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	totals := make(map[int64]*entities.UsageActivityStat)
	for rows.Next() {
		var event entities.UsageEvent
		if err := db.ScanRows(rows, &event); err != nil {
			return nil, err
		}
		bucket, err := UsageActivityBucketForTimestamp(grain, event.Timestamp)
		if err != nil {
			return nil, err
		}
		key := bucket.Start.Unix()
		row := totals[key]
		if row == nil {
			row = &entities.UsageActivityStat{Grain: grain, BucketStart: bucket.Start, BucketEnd: bucket.End}
			totals[key] = row
		}
		if event.Failed {
			row.FailureCount++
		} else {
			row.SuccessCount++
		}
		row.InputTokens += event.InputTokens
		row.OutputTokens += event.OutputTokens
		row.ReasoningTokens += event.ReasoningTokens
		row.CacheReadTokens += event.CacheReadTokens
		row.CacheCreationTokens += event.CacheCreationTokens
		row.TotalTokens += event.TotalTokens
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]entities.UsageActivityStat, 0, len(totals))
	for _, row := range totals {
		result = append(result, *row)
	}
	return result, nil
}
