package test

import (
	"context"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestArchiveRetentionCleanup(t *testing.T) {
	for _, days := range []int{0, -1, 30, 89, 90, 180, int(^uint(0) >> 1)} {
		t.Run(fmt.Sprint(days), func(t *testing.T) {
			db := openTestDatabase(t)
			now := time.Date(2026, 3, 9, 4, 30, 0, 0, time.Local)
			cutoff := time.Date(2026, 3, 9, 0, 0, 0, 0, time.Local).AddDate(0, 0, -180)
			rows := make([]entities.UsageEventArchive, 1200)
			for i := range rows {
				// 交错时间顺序，验证分批扫描不能假设 ID 与请求时间同序。
				at := cutoff.Add(-time.Second)
				if i%2 == 0 {
					at = cutoff
				}
				rows[i] = entities.UsageEventArchive{ID: int64(i + 1), EventKey: fmt.Sprint(i), Timestamp: at}
			}
			if err := db.CreateInBatches(rows, 10).Error; err != nil {
				t.Fatal(err)
			}
			hot := entities.UsageEvent{EventKey: "recent", Timestamp: now.AddDate(0, 0, -60)}
			if err := db.Create(&hot).Error; err != nil {
				t.Fatal(err)
			}
			// 热表窗口保持默认 90 天，因此这里覆盖的是总保留期与默认热表窗口的关系。
			result, err := repository.CleanupStorage(db, now, repository.StorageCleanupPolicy{RawRetentionDays: days})
			if err != nil {
				t.Fatal(err)
			}
			want := int64(0)
			if days == 90 {
				want = 1200
			}
			if days == 180 {
				want = 600
			}
			if result.UsageEventsArchiveDeleted != want {
				t.Fatalf("deleted %d, want %d", result.UsageEventsArchiveDeleted, want)
			}
			var count int64
			if err := db.Model(&entities.UsageEventArchive{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 1200-want {
				t.Fatalf("remaining %d", count)
			}
			if err := db.Model(&entities.UsageEvent{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatal("hot event deleted")
			}
			again, err := repository.CleanupStorage(db, now, repository.StorageCleanupPolicy{RawRetentionDays: days})
			if err != nil || again.UsageEventsArchiveDeleted != 0 {
				t.Fatalf("repeat: %+v %v", again, err)
			}
		})
	}
}

// 总保留期低于热表窗口时归档表一行都不能删，否则热表仍需要的窗口数据会被提前丢弃。
func TestArchiveRetentionBelowHotWindowDeletesNothing(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 3, 9, 4, 30, 0, 0, time.Local)
	cutoff := time.Date(2026, 3, 9, 0, 0, 0, 0, time.Local).AddDate(0, 0, -100)
	if err := db.Create(&entities.UsageEventArchive{ID: 7, EventKey: "cold", Timestamp: cutoff.Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	result, err := repository.CleanupStorage(db, now, repository.StorageCleanupPolicy{RawRetentionDays: 30, HotRetentionDays: 90})
	if err != nil {
		t.Fatal(err)
	}
	if result.UsageEventsArchiveDeleted != 0 {
		t.Fatalf("archived rows deleted below the hot window: %+v", result)
	}
	var count int64
	if err := db.Model(&entities.UsageEventArchive{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("archive rows changed below the hot window: %d", count)
	}
}

// 热表窗口本身可以被缩短：60 天窗口让 70/100 天前的事件进入归档表。
func TestCleanupStorageHonorsCustomHotWindow(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 3, 9, 4, 30, 0, 0, time.Local)
	if _, _, err := repository.InsertUsageEvents(db, requestEventFixtureWithZeroFees([]entities.UsageEvent{
		{EventKey: "beyond-window", Model: "claude-sonnet", Timestamp: now.AddDate(0, 0, -70), TotalTokens: 1},
		{EventKey: "inside-window", Model: "claude-sonnet", Timestamp: now.AddDate(0, 0, -10), TotalTokens: 2},
	})); err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageOverviewStats(context.Background(), db, now); err != nil {
		t.Fatal(err)
	}
	if err := repository.AggregateUsageActivityStats(context.Background(), db, now); err != nil {
		t.Fatal(err)
	}
	seedCaughtUpLatencyCheckpoint(t, db, now)

	result, err := repository.CleanupStorage(db, now, repository.StorageCleanupPolicy{HotRetentionDays: 60})
	if err != nil {
		t.Fatal(err)
	}
	if result.UsageEventsArchived != 1 {
		t.Fatalf("expected one event archived outside a 60-day window, got %+v", result)
	}
	var hotKeys []string
	if err := db.Model(&entities.UsageEvent{}).Order("event_key asc").Pluck("event_key", &hotKeys).Error; err != nil {
		t.Fatal(err)
	}
	if len(hotKeys) != 1 || hotKeys[0] != "inside-window" {
		t.Fatalf("expected only the in-window event to stay hot, got %v", hotKeys)
	}
}

// error_events 与小时级 Overview 各自按保留期清理，0 表示不清理。
func TestCleanupStoragePrunesErrorsAndHourlyRollups(t *testing.T) {
	for _, tc := range []struct {
		name           string
		policy         repository.StorageCleanupPolicy
		wantErrors     int64
		wantHourly     int64
		wantErrorsInDB int64
		wantHourlyInDB int64
	}{
		{
			name:           "retention configured",
			policy:         repository.StorageCleanupPolicy{ErrorEventsRetentionDays: 30, HourlyRetentionDays: 90},
			wantErrors:     1,
			wantHourly:     1,
			wantErrorsInDB: 1,
			wantHourlyInDB: 1,
		},
		{
			name:           "retention disabled",
			policy:         repository.StorageCleanupPolicy{},
			wantErrors:     0,
			wantHourly:     0,
			wantErrorsInDB: 2,
			wantHourlyInDB: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDatabase(t)
			now := time.Date(2026, 3, 9, 4, 30, 0, 0, time.Local)
			if err := db.Create(&[]entities.ErrorEvent{
				{Timestamp: now.AddDate(0, 0, -40), AuthIndex: "a", Body: "expired"},
				{Timestamp: now.AddDate(0, 0, -5), AuthIndex: "a", Body: "fresh"},
			}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&[]entities.UsageOverviewHourlyStat{
				{BucketStart: now.AddDate(0, 0, -120), APIGroupKey: "api", Model: "m", RequestCount: 1},
				{BucketStart: now.AddDate(0, 0, -3), APIGroupKey: "api", Model: "m", RequestCount: 1},
			}).Error; err != nil {
				t.Fatal(err)
			}
			result, err := repository.CleanupStorage(db, now, tc.policy)
			if err != nil {
				t.Fatal(err)
			}
			if result.ErrorEventsDeleted != tc.wantErrors || result.OverviewHourlyDeleted != tc.wantHourly {
				t.Fatalf("unexpected delete counts: %+v", result)
			}
			var errorsLeft, hourlyLeft int64
			if err := db.Model(&entities.ErrorEvent{}).Count(&errorsLeft).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&entities.UsageOverviewHourlyStat{}).Count(&hourlyLeft).Error; err != nil {
				t.Fatal(err)
			}
			if errorsLeft != tc.wantErrorsInDB || hourlyLeft != tc.wantHourlyInDB {
				t.Fatalf("rows left: error_events=%d hourly=%d", errorsLeft, hourlyLeft)
			}
		})
	}
}

// 用真实存储序列化覆盖 UTC 小数秒及夏令时切换后的自然日边界。
func TestArchiveRetentionPreservesCutoffDaySubseconds(t *testing.T) {
	for _, zone := range []string{"UTC", "Asia/Shanghai", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			location, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatal(err)
			}
			previous := time.Local
			time.Local = location
			t.Cleanup(func() { time.Local = previous })
			db := openTestDatabase(t)
			// 2026-03-08 是纽约夏令时起始日；固定自然日而非 180*24h。
			now := time.Date(2026, 9, 4, 4, 30, 0, 0, location)
			cutoff := time.Date(2026, 3, 8, 0, 0, 0, 0, location)
			offsets := []time.Duration{-time.Nanosecond, 0, time.Nanosecond, 500 * time.Millisecond, 23 * time.Hour}
			for i, offset := range offsets {
				row := entities.UsageEventArchive{ID: int64(i + 1), EventKey: fmt.Sprint(i), Timestamp: cutoff.Add(offset)}
				if err := db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
			}
			result, err := repository.CleanupStorage(db, now, repository.StorageCleanupPolicy{RawRetentionDays: 180})
			if err != nil {
				t.Fatal(err)
			}
			if result.UsageEventsArchiveDeleted != 1 {
				t.Fatalf("deleted %d, want only the record before cutoff", result.UsageEventsArchiveDeleted)
			}
			var ids []int64
			if err := db.Model(&entities.UsageEventArchive{}).Order("id").Pluck("id", &ids).Error; err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ids, []int64{2, 3, 4, 5}) {
				t.Fatalf("retained IDs %v, want [2 3 4 5]", ids)
			}
		})
	}
}
