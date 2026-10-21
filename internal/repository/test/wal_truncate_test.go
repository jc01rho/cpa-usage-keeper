package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/repository"
)

func TestCleanupStorageTruncatesWriteAheadLog(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "app.db")
	db, err := repository.OpenDatabase(config.Config{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("OpenDatabase returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.Exec("CREATE TABLE wal_filler (id INTEGER PRIMARY KEY, payload BLOB)").Error; err != nil {
		t.Fatalf("create filler table: %v", err)
	}
	payload := strings.Repeat("x", 20_000)
	for i := 0; i < 50; i++ {
		if err := db.Exec("INSERT INTO wal_filler (payload) VALUES (?)", payload).Error; err != nil {
			t.Fatalf("insert filler row: %v", err)
		}
	}

	walPath := dbPath + "-wal"
	before, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat wal before cleanup: %v", err)
	}
	if before.Size() == 0 {
		t.Fatalf("wal did not grow before cleanup")
	}

	now := time.Date(2026, 3, 9, 4, 30, 0, 0, time.Local)
	if _, err := repository.CleanupStorage(db, now, repository.StorageCleanupPolicy{}); err != nil {
		t.Fatalf("CleanupStorage returned error: %v", err)
	}

	after, err := os.Stat(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("stat wal after cleanup: %v", err)
	}
	if after.Size() != 0 {
		t.Fatalf("wal size = %d after cleanup, want 0", after.Size())
	}
}
