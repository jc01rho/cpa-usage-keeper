package repository

import (
	"sort"
	"strings"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

// UsageProviderFilterOption 的 Count 是当前事件查询窗口中的请求数。
type UsageProviderFilterOption struct {
	Value string
	Label string
	Count int64
}

func ListUsageEventProviderFilterOptions(db *gorm.DB, filter dto.UsageQueryFilter) ([]UsageProviderFilterOption, error) {
	var rows []struct {
		Provider string
		Count    int64
	}
	query := applyUsageEventFilterOptionsQuery(db.Model(&entities.UsageEvent{}), filter)
	if err := query.Select("provider, COUNT(*) AS count").Where("provider <> ''").Group("provider").Scan(&rows).Error; err != nil {
		return nil, err
	}
	totals := make(map[string]*UsageProviderFilterOption)
	for _, row := range rows {
		value := NormalizeUsageProvider(row.Provider)
		if value == "" {
			continue
		}
		if totals[value] == nil {
			totals[value] = &UsageProviderFilterOption{Value: value, Label: value}
		}
		totals[value].Count += row.Count
	}
	var identities []entities.UsageIdentity
	query = db.Model(&entities.UsageIdentity{}).Select("DISTINCT provider").Where("is_deleted = ? AND provider <> ''", false)
	if filter.InstanceID != "" {
		query = query.Where("instance_id = ?", filter.InstanceID)
	}
	if err := query.Order("provider ASC").Find(&identities).Error; err != nil {
		return nil, err
	}
	for _, identity := range identities {
		value := NormalizeUsageProvider(identity.Provider)
		if value == "" {
			continue
		}
		if totals[value] == nil {
			totals[value] = &UsageProviderFilterOption{Value: value}
		}
		totals[value].Label = strings.TrimSpace(identity.Provider)
	}
	options := make([]UsageProviderFilterOption, 0, len(totals))
	for _, option := range totals {
		options = append(options, *option)
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i].Count != options[j].Count {
			return options[i].Count > options[j].Count
		}
		return options[i].Value < options[j].Value
	})
	return options, nil
}
