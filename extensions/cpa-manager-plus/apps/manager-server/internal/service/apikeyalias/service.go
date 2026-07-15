package apikeyalias

import (
	"context"
	"errors"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

const MaxSaveItems = 1000

type ListRequest struct {
	Search   string
	Page     int
	PageSize int
}

type ListResponse struct {
	Items      []store.APIKeyAlias `json:"items"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"page_size"`
	Total      int64               `json:"total"`
	TotalPages int                 `json:"total_pages"`
	HasMore    bool                `json:"has_more"`
}

type SaveRequest struct {
	Items                   []store.APIKeyAlias `json:"items"`
	ActiveAPIKeyHashes      []string            `json:"activeApiKeyHashes,omitempty"`
	AllowOrphanAliasCleanup bool                `json:"allowOrphanAliasCleanup,omitempty"`
}

type Service struct {
	store *store.Store
}

func New(store *store.Store) *Service {
	return &Service{store: store}
}

func (s *Service) List(ctx context.Context) ([]store.APIKeyAlias, error) {
	return s.store.LoadAPIKeyAliases(ctx)
}

func (s *Service) ListPage(ctx context.Context, req ListRequest) (ListResponse, error) {
	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	result, err := s.store.ListAPIKeyAliasesPage(ctx, store.APIKeyAliasListQuery{
		Search: req.Search,
		Limit:  pageSize,
		Offset: (page - 1) * pageSize,
	})
	if err != nil {
		return ListResponse{}, err
	}
	totalPages := 0
	if result.Total > 0 {
		totalPages = int((result.Total + int64(pageSize) - 1) / int64(pageSize))
	}
	return ListResponse{
		Items:      result.Items,
		Page:       page,
		PageSize:   pageSize,
		Total:      result.Total,
		TotalPages: totalPages,
		HasMore:    int64(page*pageSize) < result.Total,
	}, nil
}

func (s *Service) Save(ctx context.Context, items []store.APIKeyAlias, activeHashes []string, allowOrphanCleanup bool) ([]store.APIKeyAlias, error) {
	if items == nil {
		return nil, errors.New("api key aliases are required")
	}
	if len(items) > MaxSaveItems {
		return nil, errors.New("too many api key aliases")
	}
	if len(activeHashes) > 5000 {
		return nil, errors.New("too many active api key hashes")
	}
	if err := s.store.UpsertAPIKeyAliasesWithActiveHashes(ctx, items, activeHashes, allowOrphanCleanup); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) Delete(ctx context.Context, apiKeyHash string) error {
	return s.store.DeleteAPIKeyAlias(ctx, apiKeyHash)
}
