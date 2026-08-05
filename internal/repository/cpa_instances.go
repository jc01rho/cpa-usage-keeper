package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"gorm.io/gorm"
)

var (
	ErrCPAInstanceNotFound   = errors.New("CPA instance not found")
	ErrCPACredentialNotFound = errors.New("CPA instance credential not found")
	ErrLegacyCPAInstance     = errors.New("legacy CPA instance cannot be deleted")
	ErrActiveCPACredentials  = errors.New("revoke all CPA instance credentials before deletion")
)

type CPAInstanceRepository struct {
	db *gorm.DB
}

func NewCPAInstanceRepository(db *gorm.DB) *CPAInstanceRepository {
	return &CPAInstanceRepository{db: db}
}

func (r *CPAInstanceRepository) CreateWithCredential(ctx context.Context, instance entities.CPAInstance, credential entities.CPAInstanceCredential) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&instance).Error; err != nil {
			return fmt.Errorf("create CPA instance: %w", err)
		}
		if err := tx.Create(&credential).Error; err != nil {
			return fmt.Errorf("create CPA instance credential: %w", err)
		}
		return nil
	})
}

func (r *CPAInstanceRepository) List(ctx context.Context) ([]entities.CPAInstance, error) {
	var rows []entities.CPAInstance
	if err := r.db.WithContext(ctx).Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list CPA instances: %w", err)
	}
	return rows, nil
}

func (r *CPAInstanceRepository) Get(ctx context.Context, instanceID string) (entities.CPAInstance, error) {
	var row entities.CPAInstance
	if err := r.db.WithContext(ctx).First(&row, "id = ?", instanceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return row, ErrCPAInstanceNotFound
		}
		return row, fmt.Errorf("get CPA instance: %w", err)
	}
	return row, nil
}

func (r *CPAInstanceRepository) Update(ctx context.Context, instanceID string, displayName *string, enabled *bool, now time.Time) (entities.CPAInstance, error) {
	var row entities.CPAInstance
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&row, "id = ?", instanceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCPAInstanceNotFound
			}
			return err
		}
		updates := map[string]interface{}{"updated_at": now}
		if displayName != nil {
			updates["display_name"] = *displayName
		}
		if enabled != nil {
			updates["enabled"] = *enabled
		}
		if err := tx.Model(&row).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&row, "id = ?", instanceID).Error
	})
	if err != nil {
		if errors.Is(err, ErrCPAInstanceNotFound) {
			return row, err
		}
		return row, fmt.Errorf("update CPA instance: %w", err)
	}
	return row, nil
}

func (r *CPAInstanceRepository) CreateCredential(ctx context.Context, instanceID string, credential entities.CPAInstanceCredential) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&entities.CPAInstance{}).Where("id = ?", instanceID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrCPAInstanceNotFound
		}
		return tx.Create(&credential).Error
	})
}

func (r *CPAInstanceRepository) ListCredentials(ctx context.Context, instanceID string) ([]entities.CPAInstanceCredential, error) {
	if _, err := r.Get(ctx, instanceID); err != nil {
		return nil, err
	}
	var rows []entities.CPAInstanceCredential
	if err := r.db.WithContext(ctx).Where("instance_id = ?", instanceID).Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list CPA instance credentials: %w", err)
	}
	return rows, nil
}

func (r *CPAInstanceRepository) CredentialByID(ctx context.Context, credentialID string) (entities.CPAInstanceCredential, error) {
	var row entities.CPAInstanceCredential
	if err := r.db.WithContext(ctx).First(&row, "id = ?", credentialID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return row, ErrCPACredentialNotFound
		}
		return row, fmt.Errorf("load CPA credential: %w", err)
	}
	return row, nil
}

func (r *CPAInstanceRepository) RotateCredential(ctx context.Context, instanceID, credentialID string, replacement entities.CPAInstanceCredential, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&entities.CPAInstanceCredential{}).
			Where("id = ? AND instance_id = ? AND revoked_at IS NULL", credentialID, instanceID).
			Updates(map[string]interface{}{"revoked_at": now, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCPACredentialNotFound
		}
		if err := tx.Create(&replacement).Error; err != nil {
			return err
		}
		return nil
	})
}

func (r *CPAInstanceRepository) RevokeCredential(ctx context.Context, instanceID, credentialID string, now time.Time) error {
	result := r.db.WithContext(ctx).Model(&entities.CPAInstanceCredential{}).
		Where("id = ? AND instance_id = ? AND revoked_at IS NULL", credentialID, instanceID).
		Updates(map[string]interface{}{"revoked_at": now, "updated_at": now})
	if result.Error != nil {
		return fmt.Errorf("revoke CPA instance credential: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrCPACredentialNotFound
	}
	return nil
}

func (r *CPAInstanceRepository) Delete(ctx context.Context, instanceID string) error {
	if instanceID == entities.LegacyCPAInstanceID {
		return ErrLegacyCPAInstance
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var instance entities.CPAInstance
		if err := tx.First(&instance, "id = ?", instanceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCPAInstanceNotFound
			}
			return err
		}
		var activeCredentials int64
		if err := tx.Model(&entities.CPAInstanceCredential{}).
			Where("instance_id = ? AND revoked_at IS NULL", instanceID).
			Count(&activeCredentials).Error; err != nil {
			return err
		}
		if activeCredentials != 0 {
			return ErrActiveCPACredentials
		}
		for _, table := range []string{
			"redis_usage_inboxes",
			"usage_events",
			"usage_events_archive",
			"usage_identities",
			"cpa_api_keys",
			"usage_overview_hourly_stats",
			"usage_overview_daily_stats",
			"usage_activity_stats",
			"usage_latency_stats",
			"usage_aggregation_checkpoints",
			"local_ranking_period_stats",
			"cpa_instance_credentials",
			"cpa_usage_deliveries",
			"cpa_usage_stream_watermarks",
		} {
			if err := tx.Exec("DELETE FROM "+table+" WHERE instance_id = ?", instanceID).Error; err != nil {
				return fmt.Errorf("delete instance data from %s: %w", table, err)
			}
		}
		if result := tx.Delete(&entities.CPAInstance{}, "id = ?", instanceID); result.Error != nil {
			return result.Error
		}
		return nil
	})
}
