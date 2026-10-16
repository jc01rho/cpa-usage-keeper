package dto

type UsageEventArchiveStatus string

const (
	UsageEventArchiveStatusArchived           UsageEventArchiveStatus = "archived"
	UsageEventArchiveStatusEmpty              UsageEventArchiveStatus = "empty"
	UsageEventArchiveStatusAggregationLagging UsageEventArchiveStatus = "aggregation_lagging"
)

// UsageEventArchiveResult 记录本轮 raw event 归档数量与停止原因。
type UsageEventArchiveResult struct {
	Archived int64
	Status   UsageEventArchiveStatus
}

// StorageCleanupResult 是仓储层每日清理的结果。
type StorageCleanupResult struct {
	RedisInbox                RedisUsageInboxCleanupResult
	UsageEventsArchived       int64
	UsageEventsArchiveDeleted int64
	UsageEventsArchiveStatus  UsageEventArchiveStatus
	// ErrorEventsDeleted 是本次按保留期删除的 error_events 行数；保留期为 0 时始终为 0。
	ErrorEventsDeleted int64
	// OverviewHourlyDeleted 是本次按保留期删除的 usage_overview_hourly_stats 行数；保留期为 0 时始终为 0。
	OverviewHourlyDeleted int64
	Vacuum                StorageVacuumResult
}

// StorageVacuumResult 记录每日维护对 SQLite 空闲页的条件式整理决定。
type StorageVacuumResult struct {
	Performed          bool
	SkippedReason      string
	PageSize           int64
	PageCount          int64
	FreelistCount      int64
	FreeBytes          uint64
	FreeRatio          float64
	AvailableDiskBytes uint64
}
