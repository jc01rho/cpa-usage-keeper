package test

import "testing"

// 152 v1->v2: cpa_instances의 랭킹 delete 트리거가 남은 채 remove_ranking이 테이블을 먼저 지우면
// 뒤이은 ALTER TABLE cpa_api_keys DROP COLUMN이 "no such table"로 실패했다.
func TestRemoveRankingSucceedsWithLegacyRankingInstanceTrigger(t *testing.T) {
	db := openUnmigratedTestDatabase(t)
	for _, stmt := range []string{
		"CREATE TABLE cpa_instances (id TEXT PRIMARY KEY)",
		"CREATE TABLE cpa_api_keys (id INTEGER PRIMARY KEY, name TEXT, local_ranking_avatar_id TEXT)",
		"CREATE TABLE app_settings (setting_key TEXT PRIMARY KEY, setting_value TEXT)",
		"CREATE TABLE local_ranking_period_stats (id INTEGER PRIMARY KEY, instance_id TEXT)",
		`CREATE TRIGGER fk_local_ranking_period_stats_instance_id_delete BEFORE DELETE ON cpa_instances
			FOR EACH ROW WHEN EXISTS (SELECT 1 FROM local_ranking_period_stats WHERE instance_id = OLD.id)
			BEGIN SELECT RAISE(ABORT, 'foreign key constraint failed: instance_id'); END`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	runOnlyMigration(t, db, "20261009_remove_ranking")

	var triggers int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = 'fk_local_ranking_period_stats_instance_id_delete'").Scan(&triggers).Error; err != nil {
		t.Fatalf("count trigger: %v", err)
	}
	if triggers != 0 || db.Migrator().HasTable("local_ranking_period_stats") || db.Migrator().HasColumn("cpa_api_keys", "local_ranking_avatar_id") {
		t.Fatalf("ranking leftovers remain: triggers=%d", triggers)
	}
}
