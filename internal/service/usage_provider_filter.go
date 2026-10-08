package service

import (
	"context"

	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

// UsageProviderFilterProvider 单独提供供应商候选项，不增加现有模型筛选查询的开销。
type UsageProviderFilterProvider interface {
	ListUsageEventProviderFilterOptions(context.Context, servicedto.UsageFilter) ([]repository.UsageProviderFilterOption, error)
}

func (s *usageService) ListUsageEventProviderFilterOptions(ctx context.Context, filter servicedto.UsageFilter) ([]repository.UsageProviderFilterOption, error) {
	return repository.ListUsageEventProviderFilterOptions(s.db.WithContext(usageServiceContext(ctx)), repodto.UsageQueryFilter{
		InstanceID:   filter.InstanceID,
		StartTime:    filter.StartTime,
		EndTime:      filter.EndTime,
		EndExclusive: filter.EndExclusive,
	})
}
