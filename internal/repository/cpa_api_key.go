package repository

import (
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"

	"gorm.io/gorm"
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

	return db.Transaction(func(tx *gorm.DB) error {
		var existingRows []struct {
			ID        int64
			APIKey    string
			IsDeleted bool
		}
		if err := tx.Model(&entities.CPAAPIKey{}).Select("id, api_key, is_deleted").Where("instance_id = ?", instanceID).Find(&existingRows).Error; err != nil {
			return err
		}

		existingByKey := make(map[string]struct {
			ID        int64
			IsDeleted bool
		}, len(existingRows))
		for _, row := range existingRows {
			existingByKey[row.APIKey] = struct {
				ID        int64
				IsDeleted bool
			}{ID: row.ID, IsDeleted: row.IsDeleted}
		}

		incoming := make(map[string]struct{}, len(uniqueKeys))
		toCreate := make([]entities.CPAAPIKey, 0)
		for _, key := range uniqueKeys {
			incoming[key] = struct{}{}
			if existing, ok := existingByKey[key]; ok {
				updates := map[string]any{
					"display_key":    helper.RedactSensitiveValue(key),
					"is_deleted":     false,
					"last_synced_at": &syncedAt,
					"updated_at":     syncedAt,
				}
				if err := tx.Model(&entities.CPAAPIKey{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
					return err
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
		if len(toCreate) > 0 {
			if err := tx.Create(&toCreate).Error; err != nil {
				return err
			}
		}

		staleIDs := make([]int64, 0)
		for _, row := range existingRows {
			if row.IsDeleted {
				continue
			}
			if _, ok := incoming[row.APIKey]; ok {
				continue
			}
			staleIDs = append(staleIDs, row.ID)
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
