package backup

import (
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

// CompressedBackupSuffix 是压缩备份文件的后缀；未压缩备份保持 .db。
const CompressedBackupSuffix = ".gz"

type Writer struct {
	dir string
	// compress 控制备份完成后是否用 gzip 压缩；生产库实测压缩后约占未压缩体积的五分之一。
	compress bool
}

func NewWriter(dir string) *Writer {
	return NewWriterWithCompression(dir, false)
}

func NewWriterWithCompression(dir string, compress bool) *Writer {
	return &Writer{dir: strings.TrimSpace(dir), compress: compress}
}

func (w *Writer) WriteDatabase(ctx context.Context, db *sql.DB, backupAt time.Time) (string, error) {
	if w == nil {
		return "", fmt.Errorf("backup writer is nil")
	}
	if w.dir == "" {
		return "", fmt.Errorf("backup directory is required")
	}
	if db == nil {
		return "", fmt.Errorf("database is required")
	}

	stamp := backupAt.In(time.Local)
	if stamp.IsZero() {
		stamp = time.Now().In(time.Local)
	}
	dayDir := filepath.Join(w.dir, stamp.Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	if err := os.Chmod(dayDir, 0o700); err != nil {
		return "", fmt.Errorf("restrict backup directory permissions: %w", err)
	}

	fileName := fmt.Sprintf("database_%s.db", stamp.Format("20060102T150405.000000000"))
	fullPath := filepath.Join(dayDir, fileName)
	tempPath := fullPath + ".tmp"
	_ = os.Remove(tempPath)
	if err := copySQLiteDatabase(ctx, db, tempPath); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("restrict backup file permissions: %w", err)
	}
	if err := os.Rename(tempPath, fullPath); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("finalize backup file: %w", err)
	}
	if !w.compress {
		return fullPath, nil
	}
	compressedPath, err := compressBackupFile(fullPath)
	if err != nil {
		return "", err
	}
	return compressedPath, nil
}

// compressBackupFile 把已完成的备份就地压成 gzip，成功后才删除未压缩文件。
func compressBackupFile(sourcePath string) (string, error) {
	compressedPath := sourcePath + CompressedBackupSuffix
	if err := writeCompressedCopy(sourcePath, compressedPath); err != nil {
		return "", err
	}
	// 只有压缩文件落盘成功后才删除未压缩副本，失败时仍保留可用的 .db。
	// 源文件句柄已在 writeCompressedCopy 内关闭：Windows 上仍被占用的文件无法删除。
	if err := os.Remove(sourcePath); err != nil {
		return "", fmt.Errorf("remove uncompressed backup file: %w", err)
	}
	return compressedPath, nil
}

// writeCompressedCopy 写出 gzip 副本并原子改名，返回前关闭全部句柄，
// 让调用方可以立即删除源文件（Windows 不允许删除仍打开的文件）。
func writeCompressedCopy(sourcePath, compressedPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open backup file for compression: %w", err)
	}
	defer source.Close()

	tempPath := compressedPath + ".tmp"
	_ = os.Remove(tempPath)
	target, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create compressed backup file: %w", err)
	}
	writer, err := gzip.NewWriterLevel(target, gzip.BestSpeed)
	if err != nil {
		_ = target.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("create gzip writer: %w", err)
	}
	if _, err := io.Copy(writer, source); err != nil {
		_ = writer.Close()
		_ = target.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("compress backup file: %w", err)
	}
	if err := writer.Close(); err != nil {
		_ = target.Close()
		_ = os.Remove(tempPath)
		return fmt.Errorf("finish gzip stream: %w", err)
	}
	if err := target.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close compressed backup file: %w", err)
	}
	// Windows 的 Rename 不覆盖已存在的目标文件，重新压缩时先移除上一次的 .gz。
	// 此时新文件已完整落盘，替换只会丢掉同一份内容的旧副本。
	_ = os.Remove(compressedPath)
	if err := os.Rename(tempPath, compressedPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("finalize compressed backup file: %w", err)
	}
	return nil
}

func copySQLiteDatabase(ctx context.Context, sourceDB *sql.DB, destPath string) error {
	sourceConn, err := sourceDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open source database connection: %w", err)
	}
	defer sourceConn.Close()

	destDB, err := sql.Open("sqlite3", destPath)
	if err != nil {
		return fmt.Errorf("open backup database: %w", err)
	}
	defer destDB.Close()
	destConn, err := destDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open backup database connection: %w", err)
	}
	defer destConn.Close()

	return sourceConn.Raw(func(sourceDriverConn any) error {
		sourceSQLite, ok := sourceDriverConn.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("source database connection is not sqlite3")
		}
		return destConn.Raw(func(destDriverConn any) error {
			destSQLite, ok := destDriverConn.(*sqlite3.SQLiteConn)
			if !ok {
				return fmt.Errorf("backup database connection is not sqlite3")
			}
			backup, err := destSQLite.Backup("main", sourceSQLite, "main")
			if err != nil {
				return fmt.Errorf("start sqlite backup: %w", err)
			}
			var backupErr error
			for {
				if err := ctx.Err(); err != nil {
					backupErr = err
					break
				}
				done, err := backup.Step(100)
				if err != nil {
					backupErr = fmt.Errorf("copy sqlite backup: %w", err)
					break
				}
				if done {
					break
				}
			}
			if err := backup.Close(); err != nil {
				backupErr = errors.Join(backupErr, fmt.Errorf("close sqlite backup: %w", err))
			}
			return backupErr
		})
	})
}

func (w *Writer) Cleanup(retentionDays int, now time.Time) (int, error) {
	if w == nil {
		return 0, fmt.Errorf("backup writer is nil")
	}
	// 开启压缩时先把历史未压缩备份补压，升级后首次维护就能回收存量空间。
	if w.compress {
		if _, err := compressExistingBackups(w.dir); err != nil {
			return 0, err
		}
	}
	return Cleanup(w.dir, retentionDays, now)
}

// compressExistingBackups 把目录内已完成的未压缩备份逐个压成 gzip，返回实际压缩的文件数。
// 备份 runner 在同一 goroutine 里先写备份再清理，因此这里不会与进行中的写入竞争。
func compressExistingBackups(dir string) (int, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read backup directory: %w", err)
	}
	compressed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dayDir := filepath.Join(dir, entry.Name())
		files, err := os.ReadDir(dayDir)
		if err != nil {
			return compressed, fmt.Errorf("read backup day directory %s: %w", entry.Name(), err)
		}
		for _, file := range files {
			if file.IsDir() || !strings.EqualFold(filepath.Ext(file.Name()), ".db") {
				continue
			}
			if _, err := compressBackupFile(filepath.Join(dayDir, file.Name())); err != nil {
				return compressed, err
			}
			compressed++
		}
	}
	return compressed, nil
}

func Cleanup(dir string, retentionDays int, now time.Time) (int, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" || retentionDays <= 0 {
		return 0, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read backup directory: %w", err)
	}

	localNow := now.In(time.Local)
	localDayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local)
	keepStart := localDayStart.AddDate(0, 0, -(retentionDays - 1))
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		backupDay, err := time.ParseInLocation("2006-01-02", entry.Name(), time.Local)
		if err != nil {
			continue
		}
		if backupDay.Before(keepStart) {
			if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
				return removed, fmt.Errorf("remove expired backup directory %s: %w", entry.Name(), err)
			}
			removed++
		}
	}

	return removed, nil
}

func ListFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// 压缩备份同样计入备份历史，否则 LastBackupAt 会漏掉最新备份并触发重复备份。
		if strings.EqualFold(filepath.Ext(d.Name()), ".db") || strings.HasSuffix(strings.ToLower(d.Name()), ".db"+CompressedBackupSuffix) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}
