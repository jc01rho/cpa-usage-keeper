package service

import (
	"context"
	"errors"
	"time"

	"cpa-usage-keeper/internal/protocol"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

type UsageExportProvider interface {
	IngestUsageBatch(context.Context, string, *protocol.UsageBatch) (*protocol.UsageAck, *protocol.Error)
}

type UsageExportService struct {
	db    *gorm.DB
	now   func() time.Time
	hooks repository.UsageBatchCommitHooks
}

func NewUsageExportService(db *gorm.DB) *UsageExportService {
	return &UsageExportService{db: db, now: time.Now}
}

// SetCommitHooks installs deterministic transaction hooks for tests and
// fault-injection QA. Production leaves the zero value in place.
func (s *UsageExportService) SetCommitHooks(hooks repository.UsageBatchCommitHooks) {
	s.hooks = hooks
}

func (s *UsageExportService) IngestUsageBatch(ctx context.Context, instanceID string, batch *protocol.UsageBatch) (*protocol.UsageAck, *protocol.Error) {
	if s == nil || s.db == nil || s.now == nil || instanceID == "" || batch == nil {
		return nil, protocol.ErrorForCode("internal_error")
	}
	result, err := repository.CommitUsageBatch(ctx, s.db, instanceID, batch, s.now(), s.hooks)
	if errors.Is(err, repository.ErrConflictingUsageReplay) {
		return nil, protocol.ErrorForCode("conflicting_replay")
	}
	if err != nil {
		return nil, protocol.ErrorForCode("storage_error")
	}
	acknowledged := int64(result.AcknowledgedThrough)
	return &protocol.UsageAck{
		StreamID:             batch.StreamID,
		AcknowledgedThrough:  acknowledged,
		NextExpectedSequence: acknowledged + 1,
		AcceptedCount:        int64(result.AcceptedCount),
		ReplayedCount:        int64(result.ReplayedCount),
	}, nil
}
