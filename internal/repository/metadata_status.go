package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type MetadataSnapshotStatus struct {
	InstanceID   string
	InstanceName string
	Category     string
	Revision     int64
	BodyDigest   []byte
	ItemCount    int64
	GeneratedAt  time.Time
	AppliedAt    time.Time
	LastError    *string
}

func ListMetadataSnapshotStatus(ctx context.Context, db *gorm.DB, instanceID string) ([]MetadataSnapshotStatus, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	var rows []MetadataSnapshotStatus
	query := db.WithContext(ctx).
		Table("cpa_metadata_snapshots AS snapshots").
		Select("snapshots.instance_id, instances.display_name AS instance_name, snapshots.category, snapshots.revision, snapshots.body_digest, snapshots.item_count, snapshots.generated_at, snapshots.applied_at, NULL AS last_error").
		Joins("JOIN cpa_instances AS instances ON instances.id = snapshots.instance_id")
	if instanceID = strings.TrimSpace(instanceID); instanceID != "" {
		query = query.Where("snapshots.instance_id = ?", instanceID)
	}
	if err := query.Order("snapshots.instance_id asc, snapshots.category asc").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list metadata snapshot status: %w", err)
	}
	return rows, nil
}
