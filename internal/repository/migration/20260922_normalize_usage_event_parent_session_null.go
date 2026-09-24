package migration

import (
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

// normalizeUsageEventParentSessionNullMigration 统一父会话缺失值，避免根会话同时使用 NULL 与空字符串。
func normalizeUsageEventParentSessionNullMigration(tx *gorm.DB) error {
	for _, table := range []struct {
		model any
		name  string
	}{
		{model: &entities.UsageEvent{}, name: "usage_events"},
		{model: &entities.UsageEventArchive{}, name: "usage_events_archive"},
	} {
		if !tx.Migrator().HasTable(table.model) || !tx.Migrator().HasColumn(table.model, "parent_session_id") {
			continue
		}
		for _, suffix := range []string{"insert", "update", "delete"} {
			if err := tx.Exec("DROP TRIGGER IF EXISTS fk_" + table.name + "_instance_id_" + suffix).Error; err != nil {
				return fmt.Errorf("drop %s instance %s trigger: %w", table.name, suffix, err)
			}
		}
		var createSQL string
		if err := tx.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", table.name).Scan(&createSQL).Error; err != nil {
			return fmt.Errorf("read %s schema: %w", table.name, err)
		}
		if err := makeParentSessionNullable(tx, table.name, createSQL); err != nil {
			return err
		}
		if tx.Migrator().HasTable("cpa_instances") {
			statements := []string{
				fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS fk_%s_instance_id_insert
				BEFORE INSERT ON %s
				FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM cpa_instances WHERE id = NEW.instance_id)
				BEGIN SELECT RAISE(ABORT, 'foreign key constraint failed: instance_id'); END`, table.name, table.name),
				fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS fk_%s_instance_id_update
				BEFORE UPDATE OF instance_id ON %s
				FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM cpa_instances WHERE id = NEW.instance_id)
				BEGIN SELECT RAISE(ABORT, 'foreign key constraint failed: instance_id'); END`, table.name, table.name),
				fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS fk_%s_instance_id_delete
				BEFORE DELETE ON cpa_instances
				FOR EACH ROW WHEN EXISTS (SELECT 1 FROM %s WHERE instance_id = OLD.id)
				BEGIN SELECT RAISE(ABORT, 'foreign key constraint failed: instance_id'); END`, table.name, table.name),
			}
			for _, statement := range statements {
				if err := tx.Exec(statement).Error; err != nil {
					return fmt.Errorf("restore %s instance integrity trigger: %w", table.name, err)
				}
			}
		}
		if err := tx.Table(table.name).
			Where("parent_session_id = ?", "").
			UpdateColumn("parent_session_id", nil).Error; err != nil {
			return fmt.Errorf("normalize %s.parent_session_id: %w", table.name, err)
		}
	}
	return nil
}

func makeParentSessionNullable(tx *gorm.DB, tableName, createSQL string) error {
	upperSQL := strings.ToUpper(createSQL)
	columnIndex := strings.Index(upperSQL, "PARENT_SESSION_ID")
	if columnIndex < 0 {
		return nil
	}
	columnEnd := strings.Index(upperSQL[columnIndex:], ",")
	if columnEnd < 0 {
		columnEnd = strings.Index(upperSQL[columnIndex:], ")")
	}
	if columnEnd < 0 {
		return fmt.Errorf("make %s.parent_session_id nullable: malformed column definition", tableName)
	}
	columnDefinition := createSQL[columnIndex : columnIndex+columnEnd]
	upperDefinition := strings.ToUpper(columnDefinition)
	if !strings.Contains(upperDefinition, "NOT NULL") {
		return nil
	}
	columnDefinition = strings.Replace(columnDefinition, " NOT NULL DEFAULT ''", "", 1)
	columnDefinition = strings.Replace(columnDefinition, " NOT NULL", "", 1)
	newSQL := createSQL[:columnIndex] + columnDefinition + createSQL[columnIndex+columnEnd:]
	if newSQL == createSQL {
		return fmt.Errorf("make %s.parent_session_id nullable: unsupported column definition", tableName)
	}
	tempName := tableName + "__nullable_new"
	openParen := strings.Index(newSQL, "(")
	if openParen < 0 {
		return fmt.Errorf("make %s.parent_session_id nullable: malformed table definition", tableName)
	}
	tempSQL := "CREATE TABLE " + tempName + " " + newSQL[openParen:]
	var indexSQL []string
	if err := tx.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND sql IS NOT NULL", tableName).Scan(&indexSQL).Error; err != nil {
		return fmt.Errorf("read %s indexes: %w", tableName, err)
	}
	for _, statement := range []string{
		tempSQL,
		"INSERT INTO " + tempName + " SELECT * FROM " + tableName,
		"DROP TABLE " + tableName,
		"ALTER TABLE " + tempName + " RENAME TO " + tableName,
	} {
		if err := tx.Exec(statement).Error; err != nil {
			return fmt.Errorf("rebuild %s table: %w", tableName, err)
		}
	}
	for _, statement := range indexSQL {
		if err := tx.Exec(statement).Error; err != nil {
			return fmt.Errorf("restore %s index: %w", tableName, err)
		}
	}
	return nil
}
