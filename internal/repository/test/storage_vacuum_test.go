package test

import (
	"testing"

	"cpa-usage-keeper/internal/repository"
)

func TestStorageVacuumRequiredUsesFreeBytesRatioAndDiskSpace(t *testing.T) {
	const (
		pageSize = int64(4096)
		oneGiB   = uint64(1 << 30)
	)
	// 阈值是 128 MiB 空闲页与 10% 空闲占比的与关系，再叠加 2×库大小 + 阈值的临时磁盘要求。
	tests := []struct {
		name          string
		pageCount     int64
		freelistCount int64
		available     uint64
		want          bool
	}{
		{name: "all conditions", pageCount: 1_500_000, freelistCount: 300_000, available: 13 * oneGiB, want: true},
		{name: "free bytes below the floor", pageCount: 20_000, freelistCount: 16_000, available: 8 * oneGiB},
		{name: "free ratio below ten percent", pageCount: 1_500_000, freelistCount: 149_999, available: 13 * oneGiB},
		{name: "temporary disk space insufficient", pageCount: 1_500_000, freelistCount: 300_000, available: 11 * oneGiB},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stats := repository.StorageVacuumStats{PageSize: pageSize, PageCount: test.pageCount, FreelistCount: test.freelistCount}
			if got := repository.StorageVacuumRequired(stats, test.available); got != test.want {
				t.Fatalf("StorageVacuumRequired() = %v, want %v for %+v available=%d", got, test.want, stats, test.available)
			}
		})
	}
}

func TestStorageVacuumRequiredRejectsOverflowingSpaceEstimate(t *testing.T) {
	stats := repository.StorageVacuumStats{PageSize: 1 << 32, PageCount: 1 << 31, FreelistCount: 1 << 30}
	if repository.StorageVacuumRequired(stats, ^uint64(0)) {
		t.Fatal("expected overflowing temporary space estimate to skip vacuum")
	}
}
