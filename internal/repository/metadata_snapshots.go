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

var (
	ErrStaleMetadataRevision       = errors.New("metadata revision is stale")
	ErrConflictingMetadataRevision = errors.New("metadata revision conflicts")
)

// RevisionConflictError wraps ErrStaleMetadataRevision or ErrConflictingMetadataRevision
// and exposes the keeper-side current revision so callers can surface it to the
// exporter as a transport-level recovery hint.
type RevisionConflictError struct {
	Kind            error
	CurrentRevision int64
}

func (e *RevisionConflictError) Error() string { return e.Kind.Error() }
func (e *RevisionConflictError) Unwrap() error { return e.Kind }

type MetadataSnapshotCommitResult struct {
	Applied   bool
	ItemCount int64
}

// CommitMetadataSnapshot atomically replaces one fixed category for one
// trusted instance and advances its exact-body revision ledger.
func CommitMetadataSnapshot(ctx context.Context, db *gorm.DB, instanceID string, category protocol.MetadataCategory, snapshot *protocol.MetadataSnapshot, body []byte, appliedAt time.Time) (MetadataSnapshotCommitResult, error) {
	result := MetadataSnapshotCommitResult{}
	if db == nil || instanceID == "" || snapshot == nil || appliedAt.IsZero() {
		return result, fmt.Errorf("invalid metadata snapshot commit input")
	}
	digest := sha256.Sum256(body)
	appliedAt = timeutil.NormalizeStorageTime(appliedAt)
	generatedAt, err := time.Parse("2006-01-02T15:04:05.000Z07:00", snapshot.GeneratedAt)
	if err != nil {
		return result, fmt.Errorf("parse metadata generated time: %w", err)
	}
	generatedAt = timeutil.NormalizeStorageTime(generatedAt)
	result.ItemCount = metadataSnapshotItemCount(category, snapshot)

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current entities.CPAMetadataSnapshot
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("instance_id = ? AND category = ?", instanceID, category).Take(&current).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			current = entities.CPAMetadataSnapshot{}
		case err != nil:
			return fmt.Errorf("load metadata revision: %w", err)
		}
		if current.Revision > 0 {
			switch {
			case snapshot.Revision < current.Revision:
				return &RevisionConflictError{Kind: ErrStaleMetadataRevision, CurrentRevision: current.Revision}
			case snapshot.Revision == current.Revision && !bytes.Equal(current.BodyDigest, digest[:]):
				return &RevisionConflictError{Kind: ErrConflictingMetadataRevision, CurrentRevision: current.Revision}
			case snapshot.Revision == current.Revision:
				return nil
			}
		}

		switch category {
		case protocol.CategoryAuthFiles:
			if err := replaceAuthFileSnapshot(ctx, tx, instanceID, snapshot.AuthFiles, appliedAt); err != nil {
				return err
			}
		case protocol.CategoryAPIKeys:
			if err := replaceAPIKeySnapshot(tx, instanceID, snapshot.APIKeys, appliedAt); err != nil {
				return err
			}
		case protocol.CategoryProviderIdentities:
			if err := replaceProviderIdentitySnapshot(ctx, tx, instanceID, snapshot.ProviderIdentities, appliedAt); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported metadata category %q", category)
		}

		ledger := entities.CPAMetadataSnapshot{InstanceID: instanceID, Category: string(category), Revision: snapshot.Revision, BodyDigest: digest[:], ItemCount: result.ItemCount, GeneratedAt: generatedAt, AppliedAt: appliedAt, UpdatedAt: appliedAt}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "instance_id"}, {Name: "category"}}, DoUpdates: clause.AssignmentColumns([]string{"revision", "body_digest", "item_count", "generated_at", "applied_at", "updated_at"})}).Create(&ledger).Error; err != nil {
			return fmt.Errorf("persist metadata revision: %w", err)
		}
		result.Applied = true
		return nil
	})
	return result, err
}

func metadataSnapshotItemCount(category protocol.MetadataCategory, snapshot *protocol.MetadataSnapshot) int64 {
	switch category {
	case protocol.CategoryAuthFiles:
		return int64(len(snapshot.AuthFiles))
	case protocol.CategoryAPIKeys:
		return int64(len(snapshot.APIKeys))
	case protocol.CategoryProviderIdentities:
		return int64(len(snapshot.ProviderIdentities))
	default:
		return 0
	}
}

func replaceAuthFileSnapshot(ctx context.Context, tx *gorm.DB, instanceID string, items []protocol.AuthFileItem, now time.Time) error {
	identities := make([]entities.UsageIdentity, 0, len(items))
	for _, item := range items {
		priority := intPtrFromInt64(item.Priority)
		identities = append(identities, entities.UsageIdentity{Identity: item.AuthIndex, Name: item.DisplayName, AuthTypeName: "oauth", Type: item.Type, Provider: item.Provider, Prefix: item.Prefix, FileName: metadataStringPtr(item.Name), Priority: priority, Disabled: item.Disabled, Note: item.Note, AccountID: item.AccountID, ProjectID: item.ProjectID, XAIUserID: item.XAIUserID, ActiveStart: protocolTimePtr(item.ActiveStart), ActiveUntil: protocolTimePtr(item.ActiveUntil), PlanType: item.PlanType})
	}
	return ReplaceUsageIdentitiesForAuthTypeForInstance(ctx, tx, instanceID, identities, entities.UsageIdentityAuthTypeAuthFile, now)
}

func replaceProviderIdentitySnapshot(ctx context.Context, tx *gorm.DB, instanceID string, items []protocol.ProviderIdentityItem, now time.Time) error {
	identities := make([]entities.UsageIdentity, 0, len(items))
	for _, item := range items {
		baseURL := ""
		if item.BaseURL != nil {
			baseURL = *item.BaseURL
		}
		lookup := ""
		if item.APIKeyFingerprint != nil {
			lookup = *item.APIKeyFingerprint
		}
		identities = append(identities, entities.UsageIdentity{Identity: item.AuthIndex, Name: item.DisplayName, AuthTypeName: "apikey", Type: item.ProviderType, Provider: item.DisplayName, LookupKey: lookup, Prefix: item.Prefix, BaseURL: baseURL, Priority: intPtrFromInt64(item.Priority), Disabled: item.Disabled, Note: item.Note})
	}
	return ReplaceUsageIdentitiesForAuthTypeForInstance(ctx, tx, instanceID, identities, entities.UsageIdentityAuthTypeAIProvider, now)
}

func replaceAPIKeySnapshot(tx *gorm.DB, instanceID string, items []protocol.APIKeyItem, now time.Time) error {
	var existing []entities.CPAAPIKey
	if err := tx.Where("instance_id = ?", instanceID).Find(&existing).Error; err != nil {
		return fmt.Errorf("list api keys for snapshot: %w", err)
	}
	byKey := make(map[string]entities.CPAAPIKey, len(existing))
	for _, row := range existing {
		byKey[row.APIKey] = row
	}
	incoming := make(map[string]struct{}, len(items))
	for _, item := range items {
		incoming[item.Fingerprint] = struct{}{}
		if row, ok := byKey[item.Fingerprint]; ok {
			if err := tx.Model(&entities.CPAAPIKey{}).Where("id = ? AND instance_id = ?", row.ID, instanceID).Updates(map[string]any{"display_key": item.DisplayKey, "key_alias": item.Alias, "is_deleted": false, "last_synced_at": now, "updated_at": now}).Error; err != nil {
				return fmt.Errorf("update api key snapshot: %w", err)
			}
			continue
		}
		row := entities.CPAAPIKey{InstanceID: instanceID, APIKey: item.Fingerprint, DisplayKey: item.DisplayKey, KeyAlias: item.Alias, LastSyncedAt: &now, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("create api key snapshot: %w", err)
		}
	}
	stale := make([]int64, 0)
	for _, row := range existing {
		if !row.IsDeleted {
			if _, ok := incoming[row.APIKey]; !ok {
				stale = append(stale, row.ID)
			}
		}
	}
	if len(stale) > 0 {
		if err := tx.Model(&entities.CPAAPIKey{}).Where("instance_id = ? AND id IN ?", instanceID, stale).Updates(map[string]any{"is_deleted": true, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("delete stale api key snapshot rows: %w", err)
		}
	}
	return nil
}

func intPtrFromInt64(value *int64) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
}
func metadataStringPtr(value string) *string { converted := value; return &converted }
func protocolTimePtr(value *string) *time.Time {
	if value == nil {
		return nil
	}
	parsed, err := time.Parse("2006-01-02T15:04:05.000Z07:00", *value)
	if err != nil {
		return nil
	}
	parsed = timeutil.NormalizeStorageTime(parsed)
	return &parsed
}
