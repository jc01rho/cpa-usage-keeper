package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/protocol"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const RedisUsageInboxSourceKeeperPush = "keeper_push"

var ErrConflictingUsageReplay = errors.New("conflicting usage replay")

type UsageBatchCommitResult struct {
	AcknowledgedThrough uint64
	AcceptedCount       int
	ReplayedCount       int
}

type UsageBatchCommitHooks struct {
	BeforeCommit func() error
}

func CommitUsageBatch(ctx context.Context, db *gorm.DB, instanceID string, batch *protocol.UsageBatch, acceptedAt time.Time, hooks UsageBatchCommitHooks) (UsageBatchCommitResult, error) {
	if db == nil {
		return UsageBatchCommitResult{}, fmt.Errorf("database is nil")
	}
	if batch == nil {
		return UsageBatchCommitResult{}, fmt.Errorf("usage batch is nil")
	}
	acceptedAt = timeutil.NormalizeStorageTime(acceptedAt)
	result := UsageBatchCommitResult{}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		watermark := entities.CPAUsageStreamWatermark{InstanceID: instanceID, StreamID: batch.StreamID, CreatedAt: acceptedAt, UpdatedAt: acceptedAt}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&watermark).Error; err != nil {
			return fmt.Errorf("create usage stream watermark: %w", err)
		}
		if err := tx.Where("instance_id = ? AND stream_id = ?", instanceID, batch.StreamID).Take(&watermark).Error; err != nil {
			return fmt.Errorf("load usage stream watermark: %w", err)
		}

		for _, event := range batch.Events {
			digest := sha256.Sum256(event.RawPayload)
			var existing entities.CPAUsageDelivery
			err := tx.Where("instance_id = ? AND stream_id = ? AND sequence = ?", instanceID, batch.StreamID, event.Sequence).Take(&existing).Error
			switch {
			case err == nil:
				if !bytes.Equal(existing.PayloadDigest, digest[:]) {
					return ErrConflictingUsageReplay
				}
				result.ReplayedCount++
				continue
			case !errors.Is(err, gorm.ErrRecordNotFound):
				return fmt.Errorf("load usage delivery: %w", err)
			}

			inbox := entities.RedisUsageInbox{
				InstanceID:  instanceID,
				Source:      RedisUsageInboxSourceKeeperPush,
				MessageHash: fmt.Sprintf("%x", digest),
				RawMessage:  string(event.RawPayload),
				Status:      RedisUsageInboxStatusPending,
				PoppedAt:    acceptedAt,
			}
			if err := tx.Create(&inbox).Error; err != nil {
				return fmt.Errorf("insert usage inbox: %w", err)
			}
			delivery := entities.CPAUsageDelivery{
				InstanceID:    instanceID,
				StreamID:      batch.StreamID,
				Sequence:      uint64(event.Sequence),
				PayloadDigest: append([]byte(nil), digest[:]...),
				InboxID:       inbox.ID,
				AcceptedAt:    acceptedAt,
			}
			if err := tx.Create(&delivery).Error; err != nil {
				return fmt.Errorf("insert usage delivery: %w", err)
			}
			result.AcceptedCount++
		}

		highest := watermark.AcknowledgedThrough
		var sequences []uint64
		if err := tx.Model(&entities.CPAUsageDelivery{}).
			Where("instance_id = ? AND stream_id = ? AND sequence > ?", instanceID, batch.StreamID, highest).
			Order("sequence ASC").Pluck("sequence", &sequences).Error; err != nil {
			return fmt.Errorf("list usage delivery sequences: %w", err)
		}
		for _, sequence := range sequences {
			if sequence != highest+1 {
				break
			}
			highest = sequence
		}
		if highest > watermark.AcknowledgedThrough {
			if err := tx.Model(&entities.CPAUsageStreamWatermark{}).
				Where("instance_id = ? AND stream_id = ?", instanceID, batch.StreamID).
				Updates(map[string]any{"acknowledged_through": highest, "updated_at": acceptedAt}).Error; err != nil {
				return fmt.Errorf("advance usage stream watermark: %w", err)
			}
		}
		result.AcknowledgedThrough = highest
		if hooks.BeforeCommit != nil {
			if err := hooks.BeforeCommit(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return UsageBatchCommitResult{}, err
	}
	return result, nil
}
