package service

import (
	"context"

	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type MetadataStatusProvider interface {
	ListMetadataStatus(context.Context, string) ([]repository.MetadataSnapshotStatus, error)
}

type metadataStatusService struct{ db *gorm.DB }

func NewMetadataStatusService(db *gorm.DB) MetadataStatusProvider {
	return &metadataStatusService{db: db}
}
func (s *metadataStatusService) ListMetadataStatus(ctx context.Context, instanceID string) ([]repository.MetadataSnapshotStatus, error) {
	return repository.ListMetadataSnapshotStatus(ctx, s.db, instanceID)
}
