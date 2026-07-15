package quotacooldown

import (
	"context"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type ListQuery struct {
	Provider string
	Auth     string
	Search   string
	Limit    int
	Offset   int
}

type ListPage struct {
	Items []model.QuotaCooldown
	Total int64
}

func (r *repository) ListActivePage(ctx context.Context, query ListQuery) (ListPage, error) {
	if query.Limit <= 0 {
		query.Limit = 100
	}
	if query.Limit > 200 {
		query.Limit = 200
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	conditions := []string{"status = ?"}
	args := []any{model.QuotaCooldownStatusActive}
	if provider := strings.ToLower(strings.TrimSpace(query.Provider)); provider != "" {
		conditions = append(conditions, "lower(provider) = ?")
		args = append(args, provider)
	}
	if auth := strings.ToLower(strings.TrimSpace(query.Auth)); auth != "" {
		conditions = append(conditions, "(lower(auth_file_name) = ? or lower(auth_index) = ?)")
		args = append(args, auth, auth)
	}
	if search := strings.ToLower(strings.TrimSpace(query.Search)); search != "" {
		conditions = append(conditions, `(lower(auth_file_name) like ? or lower(coalesce(auth_index, '')) like ? or lower(coalesce(account_snapshot, '')) like ? or lower(coalesce(provider, '')) like ? or lower(owner) like ?)`)
		pattern := "%" + search + "%"
		args = append(args, pattern, pattern, pattern, pattern, pattern)
	}
	where := " where " + strings.Join(conditions, " and ")
	var total int64
	if err := r.db.QueryRowContext(ctx, `select count(*) from quota_cooldowns`+where, args...).Scan(&total); err != nil {
		return ListPage{}, err
	}
	pageArgs := append(append([]any{}, args...), query.Limit, query.Offset)
	rows, err := r.db.QueryContext(ctx, selectQuotaCooldowns+where+` order by recover_at_ms asc, id asc limit ? offset ?`, pageArgs...)
	if err != nil {
		return ListPage{}, err
	}
	defer rows.Close()
	items, err := scanList(rows)
	if err != nil {
		return ListPage{}, err
	}
	return ListPage{Items: items, Total: total}, nil
}
