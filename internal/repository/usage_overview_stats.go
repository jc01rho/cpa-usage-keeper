package repository

import (
	"context"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/overview"
	"cpa-usage-keeper/internal/repository/overviewstore"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

const (
	// usageOverviewAggregationCheckpointName 保持兼容调用方使用的 Overview 名称不变。
	usageOverviewAggregationCheckpointName = "overview"
	// usageOverviewAggregationBatchSize 独立限制 Overview 兼容入口的事件页大小。
	usageOverviewAggregationBatchSize = 1000
)

// AggregateUsageOverviewStats 按 usage_events 自增 ID 增量推进既有 Overview 小时/天统计。
func AggregateUsageOverviewStats(ctx context.Context, db *gorm.DB, now time.Time) error {
	return AggregateUsageOverviewStatsForInstance(ctx, db, entities.LegacyCPAInstanceID, now)
}

func AggregateUsageOverviewStatsForInstance(ctx context.Context, db *gorm.DB, instanceID string, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	// 整次 catch-up 固定同一个项目时区 now，保持不同批次的写入时间一致。
	normalizedNow := timeutil.NormalizeStorageTime(now)
	for {
		// 完整入口只循环调用新的 Load、BuildRows、Apply 单批边界，不保留第二套查询。
		processed, err := AggregateUsageOverviewStatsBatchForInstance(ctx, db, instanceID, normalizedNow)
		if err != nil {
			return err
		}
		// 少于满页表示当前已经追到该批开始时可见的最大事件 ID。
		if processed < usageOverviewAggregationBatchSize {
			return nil
		}
	}
}

// AggregateUsageOverviewStatsBatch 执行一次 Reader 事件页和一次独立 Overview 写事务。
func AggregateUsageOverviewStatsBatch(ctx context.Context, db *gorm.DB, now time.Time) (int, error) {
	return AggregateUsageOverviewStatsBatchForInstance(ctx, db, entities.LegacyCPAInstanceID, now)
}

func AggregateUsageOverviewStatsBatchForInstance(ctx context.Context, db *gorm.DB, instanceID string, now time.Time) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database is nil")
	}
	normalizedNow := timeutil.NormalizeStorageTime(now)
	return aggregateUsageOverviewStatsBatch(ctx, db, instanceID, normalizedNow, usageOverviewAggregationBatchSize)
}

// UsageOverviewAggregationCursor 返回已提交 Overview checkpoint，供兼容 gate 调用。
func UsageOverviewAggregationCursor(ctx context.Context, db *gorm.DB) (int64, error) {
	// 兼容入口复用通用三行读取，不能继续访问 migration 已删除的旧表。
	snapshot, err := LoadUsageAggregationCheckpointSnapshot(ctx, db)
	if err != nil {
		return 0, err
	}
	return snapshot.OverviewCursor, nil
}

// HasPendingUsageOverviewAggregation 比较 Reader 看到的固定 target 与已提交 Overview cursor。
func HasPendingUsageOverviewAggregation(ctx context.Context, db *gorm.DB) (bool, error) {
	targetEventID, err := LoadUsageAggregationTargetEventID(ctx, db)
	if err != nil {
		return false, err
	}
	if targetEventID == 0 {
		return false, nil
	}
	snapshot, err := LoadUsageAggregationCheckpointSnapshot(ctx, db)
	if err != nil {
		return false, err
	}
	return snapshot.OverviewCursor < targetEventID, nil
}

// aggregateUsageOverviewStatsBatch 保留可变 limit 供既有内部测试使用，生产上限仍为 1000。
func aggregateUsageOverviewStatsBatch(ctx context.Context, db *gorm.DB, instanceID string, now time.Time, limit int) (int, error) {
	// 每个兼容 batch 固定开始时可见的 target，不在单页内追逐后来提交的事件。
	targetEventID, err := LoadUsageAggregationTargetEventIDForInstance(ctx, db, instanceID)
	if err != nil {
		return 0, err
	}
	snapshot, err := LoadUsageAggregationCheckpointSnapshotForInstance(ctx, db, instanceID)
	if err != nil {
		return 0, err
	}
	events, err := LoadUsageAggregationEventPageForInstance(ctx, db, instanceID, snapshot.OverviewCursor, targetEventID, limit)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}

	// Overview 纯计算仍由原 BuildRows 唯一负责，输入事件页不被修改。
	hourlyRows, dailyRows, maxEventID, buildErr := overview.BuildRows(events)
	if buildErr != nil {
		return 0, buildErr
	}
	if err := ApplyUsageOverviewAggregationPageForInstance(ctx, db, instanceID, snapshot.OverviewCursor, maxEventID, hourlyRows, dailyRows, now); err != nil {
		return 0, err
	}
	return len(events), nil
}

// ApplyUsageOverviewAggregationPage 在独立短事务内写两种 rows 并条件推进 Overview 水位。
func ApplyUsageOverviewAggregationPage(ctx context.Context, db *gorm.DB, expectedCursor, nextCursor int64, hourly []entities.UsageOverviewHourlyStat, daily []entities.UsageOverviewDailyStat, now time.Time) error {
	return ApplyUsageOverviewAggregationPageForInstance(ctx, db, entities.LegacyCPAInstanceID, expectedCursor, nextCursor, hourly, daily, now)
}

func ApplyUsageOverviewAggregationPageForInstance(ctx context.Context, db *gorm.DB, instanceID string, expectedCursor, nextCursor int64, hourly []entities.UsageOverviewHourlyStat, daily []entities.UsageOverviewDailyStat, now time.Time) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	normalizedNow := timeutil.NormalizeStorageTime(now)
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 复用既有累计 SQL；本次只改变 rows 的来源和 checkpoint 表。
		if err := overviewstore.ApplyRows(tx, hourly, daily, normalizedNow); err != nil {
			return err
		}
		// cursor 最后推进，冲突错误会让 GORM 回滚刚写入的两类 rows。
		return AdvanceUsageAggregationCheckpointForInstance(ctx, tx, instanceID, entities.UsageAggregationCheckpointOverview, expectedCursor, nextCursor, normalizedNow)
	})
}

// CleanupUsageOverviewHourlyStats 按保留期分批删除过期的小时级 Overview 聚合。
//
// 小时表只服务最近窗口的边界补偿（长范围查询由 daily rollup 承担完整自然日），
// 因此它的保留期可以远短于 daily。删除按 bucket_start 判定，不做实例过滤，
// 多实例行一并收敛。retentionDays <= 0 表示不清理，与配置默认值一致。
//
// 不使用 id 游标：迟到的增量聚合可能给更早的 bucket 写入更大的 id，
// 用 id 游标会永久跳过这些行。每轮重新按 bucket_start 索引取前 N 行即可。
func CleanupUsageOverviewHourlyStats(db *gorm.DB, now time.Time, retentionDays int) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("database is nil")
	}
	if retentionDays <= 0 {
		return 0, nil
	}
	local := now.In(time.Local)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	// 极大保留值覆盖所有有效历史，避免 AddDate 的整数溢出反向删除数据。
	if retentionDays/366 > day.Year() {
		return 0, nil
	}
	// bucket_start 使用 storageTime 保存项目本地墙钟时间；清理参数必须保持同一种文本格式。
	cutoff := timeutil.FormatStorageTime(day.AddDate(0, 0, -retentionDays))
	var deleted int64
	for {
		if err := databaseContext(db).Err(); err != nil {
			return deleted, err
		}
		var ids []int64
		if err := db.Model(&entities.UsageOverviewHourlyStat{}).
			Where("bucket_start < ?", cutoff).
			Order("id ASC").Limit(sqliteVariableLimit).Pluck("id", &ids).Error; err != nil {
			return deleted, fmt.Errorf("load expired overview hourly batch: %w", err)
		}
		if len(ids) == 0 {
			return deleted, nil
		}
		result := db.Where("id IN ? AND bucket_start < ?", ids, cutoff).Delete(&entities.UsageOverviewHourlyStat{})
		if result.Error != nil {
			return deleted, fmt.Errorf("delete expired overview hourly batch: %w", result.Error)
		}
		deleted += result.RowsAffected
	}
}
