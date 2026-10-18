package test

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/backup"
)

func TestWriterCompressesBackupWhenEnabled(t *testing.T) {
	root := t.TempDir()
	source := newSourceDatabase(t)
	// 重复文本让压缩比接近真实 usage 数据，避免小样本压缩反而变大影响断言。
	payload := strings.Repeat("usage-event-payload-", 400)
	for i := 0; i < 200; i++ {
		if _, err := source.Exec(`INSERT INTO records (name) VALUES (?)`, payload); err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}
	writer := backup.NewWriterWithCompression(root, true)
	backupAt := time.Date(2026, 4, 16, 12, 34, 56, 0, time.UTC)

	path, err := writer.WriteDatabase(context.Background(), source, backupAt)
	if err != nil {
		t.Fatalf("WriteDatabase returned error: %v", err)
	}

	if !strings.HasSuffix(path, ".db"+backup.CompressedBackupSuffix) {
		t.Fatalf("compressed backup path = %q, want .db.gz suffix", path)
	}
	if _, err := os.Stat(strings.TrimSuffix(path, backup.CompressedBackupSuffix)); !os.IsNotExist(err) {
		t.Fatalf("uncompressed backup still present after compression: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat compressed backup: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("compressed backup is empty")
	}

	restoredPath := filepath.Join(t.TempDir(), "restored.db")
	restoreGzip(t, path, restoredPath)
	restored := openTestSQLiteDB(t, restoredPath)
	var count int
	if err := restored.QueryRow(`SELECT COUNT(*) FROM records`).Scan(&count); err != nil {
		t.Fatalf("query restored rows: %v", err)
	}
	if count != 200 {
		t.Fatalf("restored row count = %d, want 200", count)
	}
}

func TestListFilesIncludesCompressedBackups(t *testing.T) {
	root := t.TempDir()
	source := newSourceDatabase(t)
	if _, err := source.Exec(`INSERT INTO records (name) VALUES (?)`, "saved"); err != nil {
		t.Fatalf("insert row: %v", err)
	}
	path, err := backup.NewWriterWithCompression(root, true).WriteDatabase(context.Background(), source, time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("WriteDatabase returned error: %v", err)
	}

	files, err := backup.ListFiles(root)
	if err != nil {
		t.Fatalf("ListFiles returned error: %v", err)
	}
	if len(files) != 1 || files[0] != path {
		t.Fatalf("ListFiles = %v, want [%s]", files, path)
	}
}

func TestWriterKeepsUncompressedBackupByDefault(t *testing.T) {
	root := t.TempDir()
	source := newSourceDatabase(t)
	if _, err := source.Exec(`INSERT INTO records (name) VALUES (?)`, "saved"); err != nil {
		t.Fatalf("insert row: %v", err)
	}
	path, err := backup.NewWriter(root).WriteDatabase(context.Background(), source, time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("WriteDatabase returned error: %v", err)
	}
	if filepath.Ext(path) != ".db" {
		t.Fatalf("default backup path = %q, want .db suffix", path)
	}
}

func restoreGzip(t *testing.T, sourcePath, destPath string) {
	t.Helper()
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatalf("open compressed backup: %v", err)
	}
	defer source.Close()
	reader, err := gzip.NewReader(source)
	if err != nil {
		t.Fatalf("open gzip reader: %v", err)
	}
	defer reader.Close()
	dest, err := os.Create(destPath)
	if err != nil {
		t.Fatalf("create restored backup: %v", err)
	}
	defer dest.Close()
	if _, err := io.Copy(dest, reader); err != nil {
		t.Fatalf("decompress backup: %v", err)
	}
}

// TestCleanupCompressesExistingBackups 覆盖升级后的存量回收：已有未压缩备份在首次维护时被补压。
func TestCleanupCompressesExistingBackups(t *testing.T) {
	root := t.TempDir()
	source := newSourceDatabase(t)
	payload := strings.Repeat("historical-usage-event-", 300)
	for i := 0; i < 100; i++ {
		if _, err := source.Exec(`INSERT INTO records (name) VALUES (?)`, payload); err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}
	// 旧版本写入的未压缩备份，模拟升级前的存量目录。
	legacyPath, err := backup.NewWriter(root).WriteDatabase(context.Background(), source, time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("write legacy backup: %v", err)
	}

	if _, err := backup.NewWriterWithCompression(root, true).Cleanup(7, time.Date(2026, 4, 16, 9, 0, 0, 0, time.Local)); err != nil {
		t.Fatalf("Cleanup returned error: %v", err)
	}

	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy uncompressed backup still present: %v", err)
	}
	info, err := os.Stat(legacyPath + backup.CompressedBackupSuffix)
	if err != nil {
		t.Fatalf("stat compressed legacy backup: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("compressed legacy backup is empty")
	}
}
