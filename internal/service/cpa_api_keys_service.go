package service

import (
	"context"
	"errors"
	"strings"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

var ErrInvalidID = errors.New("invalid id")

type CPAAPIKeyProvider interface {
	ListCPAAPIKeys(ctx context.Context) ([]entities.CPAAPIKey, error)
	FindActiveCPAAPIKeyByValue(ctx context.Context, apiKey string) (entities.CPAAPIKey, error)
	FindActiveCPAAPIKeyByID(ctx context.Context, id int64) (entities.CPAAPIKey, error)
	UpdateCPAAPIKeyAlias(ctx context.Context, id int64, keyAlias string) (entities.CPAAPIKey, error)
}

type cpaAPIKeyService struct {
	db *gorm.DB
}

func NewCPAAPIKeyService(db *gorm.DB) CPAAPIKeyProvider {
	return &cpaAPIKeyService{db: db}
}

func (s *cpaAPIKeyService) ListCPAAPIKeys(ctx context.Context) ([]entities.CPAAPIKey, error) {
	instanceID := InstanceFilterFromContext(ctx)
	rows, err := repository.ListActiveCPAAPIKeysForInstance(s.db, instanceID)
	if err != nil {
		return nil, err
	}
	// 单实例视图保持每个实例自己的行；all-instance 视图把同一 key 字符串合并成一行。
	if instanceID != "" {
		return rows, nil
	}
	return mergeCPAAPIKeysByValue(rows), nil
}

// mergeCPAAPIKeysByValue 把同一 key 字符串的多实例行合并成代表行，
// 代表行沿用排在最前的实例，别名取第一个非空值。
func mergeCPAAPIKeysByValue(rows []entities.CPAAPIKey) []entities.CPAAPIKey {
	merged := make([]entities.CPAAPIKey, 0, len(rows))
	indexByValue := make(map[string]int, len(rows))
	for _, row := range rows {
		index, seen := indexByValue[row.APIKey]
		if !seen {
			indexByValue[row.APIKey] = len(merged)
			merged = append(merged, row)
			continue
		}
		if strings.TrimSpace(merged[index].KeyAlias) == "" && strings.TrimSpace(row.KeyAlias) != "" {
			merged[index].KeyAlias = row.KeyAlias
		}
	}
	return merged
}

func (s *cpaAPIKeyService) ListCPAAPIKeysForInstance(_ context.Context, instanceID string) ([]entities.CPAAPIKey, error) {
	return repository.ListCPAAPIKeysForInstance(s.db, instanceID)
}

func (s *cpaAPIKeyService) FindActiveCPAAPIKeyByValue(_ context.Context, apiKey string) (entities.CPAAPIKey, error) {
	trimmed := strings.TrimSpace(apiKey)
	if trimmed == "" {
		return entities.CPAAPIKey{}, gorm.ErrRecordNotFound
	}
	return repository.FindActiveCPAAPIKeyByValue(s.db, trimmed)
}

func (s *cpaAPIKeyService) FindActiveCPAAPIKeyByID(_ context.Context, id int64) (entities.CPAAPIKey, error) {
	if id <= 0 {
		return entities.CPAAPIKey{}, gorm.ErrRecordNotFound
	}
	return repository.FindActiveCPAAPIKeyByID(s.db, id)
}

func (s *cpaAPIKeyService) UpdateCPAAPIKeyAlias(ctx context.Context, id int64, keyAlias string) (entities.CPAAPIKey, error) {
	if id <= 0 {
		return entities.CPAAPIKey{}, ErrInvalidID
	}
	// 写命令的首次回读同样必须留在 writer，否则占用读池时会在 UPDATE 之前就阻塞。
	row, err := repository.FindActiveCPAAPIKeyByID(s.db.Clauses(dbresolver.Write), id)
	if err != nil {
		return entities.CPAAPIKey{}, err
	}
	if InstanceFilterFromContext(ctx) == "" {
		// all-instance 视图只显示一行，因此一次编辑必须落到同一 key 的全部实例。
		if err := repository.UpdateCPAAPIKeyAliasByValue(s.db.Clauses(dbresolver.Write), row.APIKey, keyAlias); err != nil {
			return entities.CPAAPIKey{}, err
		}
	} else {
		// UPDATE 由 dbresolver 自动路由 writer；结果回读再用官方 Write clause 固定到同一物理池。
		if err := repository.UpdateCPAAPIKeyAlias(s.db, id, keyAlias); err != nil {
			return entities.CPAAPIKey{}, err
		}
	}
	return repository.FindActiveCPAAPIKeyByID(s.db.Clauses(dbresolver.Write), id)
}
