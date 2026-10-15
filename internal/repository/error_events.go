package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
)

const defaultErrorEventPageSize = 50

// ErrorEventCursor 使用错误发生时间和本地主键共同定位下一页，避免相同 timestamp 漏行或重复。
type ErrorEventCursor struct {
	// Timestamp 是上一页末行的 CPA 事件时间。
	Timestamp time.Time
	// ID 是上一页末行的 Keeper 本地主键，用于相同 Timestamp 下的稳定次序。
	ID int64
}

// ErrorEventPage 是单个 Credential 的 Error Events 游标页，不执行无用的 total count 查询。
type ErrorEventPage struct {
	// Events 最多包含请求的 pageSize 行，不包含用于探测下一页的额外行。
	Events []entities.ErrorEvent
	// HasMore 表示查询曾取到第 pageSize+1 行。
	HasMore bool
}

// InsertErrorEvent 将已经完成契约解码的 Error Event 直接写入最终表，不创建 Inbox 或重试状态。
func InsertErrorEvent(ctx context.Context, db *gorm.DB, event *entities.ErrorEvent) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	if event == nil {
		return fmt.Errorf("CPA error event is nil")
	}
	if err := db.WithContext(ctx).Create(event).Error; err != nil {
		return fmt.Errorf("insert CPA error event: %w", err)
	}
	return nil
}

// ListErrorEventsByAuthIndex 按稳定 auth_index 查询详情页，固定使用 timestamp/id 倒序游标。
func ListErrorEventsByAuthIndex(ctx context.Context, db *gorm.DB, authIndex string, cursor *ErrorEventCursor, pageSize int) (ErrorEventPage, error) {
	if db == nil {
		return ErrorEventPage{}, fmt.Errorf("database is nil")
	}
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return ErrorEventPage{}, fmt.Errorf("auth index is required")
	}
	if pageSize <= 0 {
		pageSize = defaultErrorEventPageSize
	}

	query := db.WithContext(ctx).Model(&entities.ErrorEvent{}).Where("auth_index = ?", authIndex)
	if cursor != nil {
		// storageTime serializer 把 time.Time 写成统一文本；游标比较使用同一格式避免驱动隐式转换差异。
		cursorTimestamp := timeutil.FormatStorageTime(cursor.Timestamp)
		query = query.Where(
			"(timestamp < ? OR (timestamp = ? AND id < ?))",
			cursorTimestamp,
			cursorTimestamp,
			cursor.ID,
		)
	}

	// 多取一行只用于判断 has_more；返回前裁掉探测行，不执行 total count。
	var events []entities.ErrorEvent
	if err := query.Order("timestamp DESC, id DESC").Limit(pageSize + 1).Find(&events).Error; err != nil {
		return ErrorEventPage{}, fmt.Errorf("list CPA error events: %w", err)
	}
	hasMore := len(events) > pageSize
	if hasMore {
		events = events[:pageSize]
	}
	return ErrorEventPage{Events: events, HasMore: hasMore}, nil
}

// CleanupUsageErrorEvents 按保留期分批删除过期的 error_events。
//
// error_events 没有聚合替代品（每次凭证错误都是最终业务数据），也没有 instance 维度
// （CPA error event 不带实例），因此这里不做实例过滤，多实例行一并收敛。
// retentionDays <= 0 表示不清理，与配置默认值一致。
// 删除按 id 升序游标推进，避免每批重复扫描保留记录；比较使用项目本地自然日的
// 日期前缀，因此保留日当天（含零点小数秒）的记录不会被删。
func CleanupUsageErrorEvents(db *gorm.DB, now time.Time, retentionDays int) (int64, error) {
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
	cutoff := day.AddDate(0, 0, -retentionDays).Format("2006-01-02")
	var deleted, afterID int64
	for {
		if err := databaseContext(db).Err(); err != nil {
			return deleted, err
		}
		var ids []int64
		if err := db.Model(&entities.ErrorEvent{}).
			Where("id > ? AND timestamp < ?", afterID, cutoff).
			Order("id ASC").Limit(sqliteVariableLimit).Pluck("id", &ids).Error; err != nil {
			return deleted, fmt.Errorf("load expired error event batch: %w", err)
		}
		if len(ids) == 0 {
			return deleted, nil
		}
		result := db.Where("id IN ? AND timestamp < ?", ids, cutoff).Delete(&entities.ErrorEvent{})
		if result.Error != nil {
			return deleted, fmt.Errorf("delete expired error event batch: %w", result.Error)
		}
		deleted += result.RowsAffected
		afterID = ids[len(ids)-1]
	}
}
