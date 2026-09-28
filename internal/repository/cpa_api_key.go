package repository

import (
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

func SyncCPAAPIKeys(db *gorm.DB, keys []string, syncedAt time.Time) error {
	return SyncCPAAPIKeysForInstance(db, entities.LegacyCPAInstanceID, keys, syncedAt)
}

func SyncCPAAPIKeysForInstance(db *gorm.DB, instanceID string, keys []string, syncedAt time.Time) error {
	seen := make(map[string]struct{}, len(keys))
	uniqueKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		uniqueKeys = append(uniqueKeys, key)
	}

	var existingRows []struct {
		ID         int64
		APIKey     string
		DisplayKey string
		IsDeleted  bool
	}
	// 先在独立 reader 上比较完整 key 集合，只有变化才进入 writer 事务。
	if err := db.Clauses(dbresolver.Read).Model(&entities.CPAAPIKey{}).
		Select("id, api_key, display_key, is_deleted").Where("instance_id = ?", instanceID).Find(&existingRows).Error; err != nil {
		return err
	}
	type keyUpdate struct {
		id     int64
		fields map[string]any
	}
	existingByKey := make(map[string]int, len(existingRows))
	for index, row := range existingRows {
		existingByKey[row.APIKey] = index
	}
	incoming := make(map[string]struct{}, len(uniqueKeys))
	toCreate := make([]entities.CPAAPIKey, 0)
	toUpdate := make([]keyUpdate, 0)
	for _, key := range uniqueKeys {
		incoming[key] = struct{}{}
		if index, ok := existingByKey[key]; ok {
			row := existingRows[index]
			fields := make(map[string]any)
			if display := helper.RedactSensitiveValue(key); row.DisplayKey != display {
				fields["display_key"] = display
			}
			if row.IsDeleted {
				fields["is_deleted"] = false
			}
			if len(fields) > 0 {
				fields["last_synced_at"] = &syncedAt
				fields["updated_at"] = syncedAt
				toUpdate = append(toUpdate, keyUpdate{id: row.ID, fields: fields})
			}
			continue
		}
		toCreate = append(toCreate, entities.CPAAPIKey{
			InstanceID:   instanceID,
			APIKey:       key,
			DisplayKey:   helper.RedactSensitiveValue(key),
			IsDeleted:    false,
			LastSyncedAt: &syncedAt,
		})
	}
	staleIDs := make([]int64, 0)
	for _, row := range existingRows {
		if row.IsDeleted {
			continue
		}
		if _, ok := incoming[row.APIKey]; !ok {
			staleIDs = append(staleIDs, row.ID)
		}
	}
	if len(toCreate) == 0 && len(toUpdate) == 0 && len(staleIDs) == 0 {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, change := range toUpdate {
			if err := tx.Model(&entities.CPAAPIKey{}).Where("instance_id = ? AND id = ?", instanceID, change.id).Updates(change.fields).Error; err != nil {
				return err
			}
		}
		if len(toCreate) > 0 {
			if err := tx.Create(&toCreate).Error; err != nil {
				return err
			}
		}
		if len(staleIDs) == 0 {
			return nil
		}
		return tx.Model(&entities.CPAAPIKey{}).Where("instance_id = ? AND id IN ?", instanceID, staleIDs).Updates(map[string]any{"is_deleted": true, "updated_at": syncedAt}).Error
	})
}

func ListActiveCPAAPIKeys(db *gorm.DB) ([]entities.CPAAPIKey, error) {
	return ListActiveCPAAPIKeysForInstance(db, "")
}

func ListCPAAPIKeysForInstance(db *gorm.DB, instanceID string) ([]entities.CPAAPIKey, error) {
	var rows []entities.CPAAPIKey
	query := db
	if instanceID = strings.TrimSpace(instanceID); instanceID != "" {
		query = query.Where("instance_id = ?", instanceID)
	}
	err := query.Order("instance_id asc, id asc").Find(&rows).Error
	return rows, err
}

func ListActiveCPAAPIKeysForInstance(db *gorm.DB, instanceID string) ([]entities.CPAAPIKey, error) {
	var rows []entities.CPAAPIKey
	query := db.Where("is_deleted = ?", false)
	if instanceID = strings.TrimSpace(instanceID); instanceID != "" {
		query = query.Where("instance_id = ?", instanceID)
	}
	err := query.Order("instance_id asc, id asc").Find(&rows).Error
	return rows, err
}

func FindActiveCPAAPIKeyByID(db *gorm.DB, id int64) (entities.CPAAPIKey, error) {
	return FindActiveCPAAPIKeyByIDForInstance(db, id, "")
}

func FindActiveCPAAPIKeyByIDForInstance(db *gorm.DB, id int64, instanceID string) (entities.CPAAPIKey, error) {
	var row entities.CPAAPIKey
	query := db.Where("id = ? AND is_deleted = ?", id, false)
	if instanceID = strings.TrimSpace(instanceID); instanceID != "" {
		query = query.Where("instance_id = ?", instanceID)
	}
	err := query.First(&row).Error
	return row, err
}

func FindActiveCPAAPIKeyByValue(db *gorm.DB, apiKey string) (entities.CPAAPIKey, error) {
	return FindActiveCPAAPIKeyByValueForInstance(db, apiKey, "")
}

func FindActiveCPAAPIKeyByValueForInstance(db *gorm.DB, apiKey, instanceID string) (entities.CPAAPIKey, error) {
	var row entities.CPAAPIKey
	query := db.Where("api_key = ? AND is_deleted = ?", apiKey, false)
	if instanceID = strings.TrimSpace(instanceID); instanceID != "" {
		query = query.Where("instance_id = ?", instanceID)
	}
	err := query.Order("instance_id asc, id asc").First(&row).Error
	return row, err
}

func UpdateCPAAPIKeyAlias(db *gorm.DB, id int64, keyAlias string) error {
	result := db.Model(&entities.CPAAPIKey{}).Where("id = ? AND is_deleted = ?", id, false).Update("key_alias", strings.TrimSpace(keyAlias))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateCPAAPIKeyLocalRankingProfile 在同一写事务中保存并回读 Key 的本地展示资料。
func UpdateCPAAPIKeyLocalRankingProfile(db *gorm.DB, id int64, keyAlias string, avatarID uint8) (entities.CPAAPIKey, error) {
	var row entities.CPAAPIKey
	err := db.Clauses(dbresolver.Write).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&entities.CPAAPIKey{}).Where("id = ?", id).Updates(map[string]any{
			"key_alias":               strings.TrimSpace(keyAlias),
			"local_ranking_avatar_id": avatarID,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Where("id = ?", id).First(&row).Error
	})
	return row, err
}
