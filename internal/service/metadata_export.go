package service

import (
	"context"
	"errors"
	"time"

	"cpa-usage-keeper/internal/protocol"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type MetadataExportProvider interface {
	IngestMetadataSnapshot(context.Context, string, protocol.MetadataCategory, *protocol.MetadataSnapshot, []byte) (*protocol.MetadataApplyResponse, *protocol.Error)
}

type MetadataExportService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewMetadataExportService(db *gorm.DB) *MetadataExportService {
	return &MetadataExportService{db: db, now: time.Now}
}

func (s *MetadataExportService) IngestMetadataSnapshot(ctx context.Context, instanceID string, category protocol.MetadataCategory, snapshot *protocol.MetadataSnapshot, body []byte) (*protocol.MetadataApplyResponse, *protocol.Error) {
	if s == nil || s.db == nil || s.now == nil || instanceID == "" || snapshot == nil {
		return nil, protocol.ErrorForCode("internal_error")
	}
	result, err := repository.CommitMetadataSnapshot(ctx, s.db, instanceID, category, snapshot, body, s.now())
	switch {
	case errors.Is(err, repository.ErrStaleMetadataRevision):
		return nil, protocol.ErrorForCode("stale_revision")
	case errors.Is(err, repository.ErrConflictingMetadataRevision):
		return nil, protocol.ErrorForCode("conflicting_revision")
	case err != nil:
		return nil, protocol.ErrorForCode("storage_error")
	}
	return &protocol.MetadataApplyResponse{Category: category, Revision: snapshot.Revision, Applied: result.Applied, ItemCount: result.ItemCount, ServerTime: protocolTimestamp(s.now())}, nil
}

func protocolTimestamp(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}
