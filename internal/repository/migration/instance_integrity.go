package migration

import (
	"fmt"

	"gorm.io/gorm"
)

// EnsureCPAInstanceIntegrity adds SQLite-enforced RESTRICT semantics without
// rebuilding every large historical table solely to add a REFERENCES clause.
// Inserts and instance moves fail unless cpa_instances already contains the ID.
// It is shared by the upgrade migration and fresh-schema initialization.
func EnsureCPAInstanceIntegrity(db *gorm.DB) error {
	for _, table := range keeperInstanceTables {
		for _, operation := range []string{"INSERT", "UPDATE OF instance_id"} {
			suffix := "insert"
			if operation != "INSERT" {
				suffix = "update"
			}
			statement := fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS fk_%s_instance_id_%s
				BEFORE %s ON %s
				FOR EACH ROW WHEN NOT EXISTS (SELECT 1 FROM cpa_instances WHERE id = NEW.instance_id)
				BEGIN SELECT RAISE(ABORT, 'foreign key constraint failed: instance_id'); END`, table, suffix, operation, table)
			if err := db.Exec(statement).Error; err != nil {
				return fmt.Errorf("create %s instance integrity trigger: %w", table, err)
			}
		}
		deleteStatement := fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS fk_%s_instance_id_delete
			BEFORE DELETE ON cpa_instances
			FOR EACH ROW WHEN EXISTS (SELECT 1 FROM %s WHERE instance_id = OLD.id)
			BEGIN SELECT RAISE(ABORT, 'foreign key constraint failed: instance_id'); END`, table, table)
		if err := db.Exec(deleteStatement).Error; err != nil {
			return fmt.Errorf("create %s instance delete integrity trigger: %w", table, err)
		}
	}
	return nil
}
