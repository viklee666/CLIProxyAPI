package usageevent

import (
	"context"
	"fmt"
	"strings"
)

const (
	defaultAggregatePageSize = 50
	maxAggregatePageSize     = 200
)

const accountAggregateKeyExpr = `case
	when trim(coalesce(account_snapshot, '')) <> '' then account_snapshot
	when trim(coalesce(auth_label_snapshot, '')) <> '' then auth_label_snapshot
	when trim(coalesce(source, '')) <> '' then source
	when trim(coalesce(auth_index, '')) <> '' then auth_index
	else '-'
end`

const apiKeyAggregateKeyExpr = `case
	when trim(coalesce(api_key_hash, '')) <> '' then lower(trim(api_key_hash))
	else 'unknown-client-api-key:' ||
		coalesce(nullif(trim(coalesce(source_hash, '')), ''), '-') || ':' ||
		coalesce(nullif(trim(coalesce(auth_index, '')), ''), '-') || ':' ||
		coalesce(nullif(trim(coalesce(source, '')), ''), '-') || ':' ||
		coalesce(nullif(trim(coalesce(nullif(auth_provider_snapshot, ''), provider, '')), ''), '-')
end`

type ModelStatsPage struct {
	Items []ModelStat
	Total int64
	Count int
}

type AccountModelStatsPage struct {
	Items []AccountModelStat
	Total int64
	Count int
}

type CredentialModelStatsPage struct {
	Items []CredentialModelStat
	Total int64
	Count int
}

type APIKeyModelStatsPage struct {
	Items []APIKeyModelStat
	Total int64
	Count int
}

func normalizeAggregatePage(limit int, offset int) (int, int) {
	if limit <= 0 {
		limit = defaultAggregatePageSize
	}
	if limit > maxAggregatePageSize {
		limit = maxAggregatePageSize
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (r *repository) ModelStatsPageWithFilter(ctx context.Context, filter AnalyticsFilter, limit int, offset int) (ModelStatsPage, error) {
	limit, offset = normalizeAggregatePage(limit, offset)
	keys, total, err := r.aggregatePageKeys(ctx, filter, "model", "count(*) desc, max(timestamp_ms) desc, entity_key", limit, offset)
	if err != nil || len(keys) == 0 {
		return ModelStatsPage{Total: total}, err
	}
	filter.modelAggregateKeys = keys
	items, err := r.modelStatsWithFilter(ctx, filter)
	return ModelStatsPage{Items: items, Total: total, Count: len(keys)}, err
}

func (r *repository) AccountModelStatsPageWithFilter(ctx context.Context, filter AnalyticsFilter, limit int, offset int) (AccountModelStatsPage, error) {
	limit, offset = normalizeAggregatePage(limit, offset)
	keys, total, err := r.aggregatePageKeys(ctx, filter, accountAggregateKeyExpr, "max(timestamp_ms) desc, count(*) desc, entity_key", limit, offset)
	if err != nil || len(keys) == 0 {
		return AccountModelStatsPage{Total: total}, err
	}
	filter.accountAggregateKeys = keys
	items, err := r.accountModelStatsWithFilter(ctx, filter)
	return AccountModelStatsPage{Items: items, Total: total, Count: len(keys)}, err
}

func (r *repository) CredentialModelStatsPageWithFilter(ctx context.Context, filter AnalyticsFilter, limit int, offset int) (CredentialModelStatsPage, error) {
	limit, offset = normalizeAggregatePage(limit, offset)
	keys, total, err := r.aggregatePageKeys(ctx, filter, credentialIDExpr, "sum(total_tokens) desc, count(*) desc, max(timestamp_ms) desc, entity_key", limit, offset)
	if err != nil || len(keys) == 0 {
		return CredentialModelStatsPage{Total: total}, err
	}
	filter.credentialAggregateKeys = keys
	items, err := r.credentialModelStatsWithFilter(ctx, filter)
	return CredentialModelStatsPage{Items: items, Total: total, Count: len(keys)}, err
}

func (r *repository) APIKeyModelStatsPageWithFilter(ctx context.Context, filter AnalyticsFilter, limit int, offset int) (APIKeyModelStatsPage, error) {
	limit, offset = normalizeAggregatePage(limit, offset)
	keys, total, err := r.aggregatePageKeys(ctx, filter, apiKeyAggregateKeyExpr, "max(timestamp_ms) desc, count(*) desc, entity_key", limit, offset)
	if err != nil || len(keys) == 0 {
		return APIKeyModelStatsPage{Total: total}, err
	}
	filter.apiKeyAggregateKeys = keys
	items, err := r.apiKeyModelStatsWithFilter(ctx, filter)
	return APIKeyModelStatsPage{Items: items, Total: total, Count: len(keys)}, err
}

func (r *repository) aggregatePageKeys(ctx context.Context, filter AnalyticsFilter, expression string, orderBy string, limit int, offset int) ([]string, int64, error) {
	where, args := analyticsWhere(filter)
	countQuery := `select count(*) from (
		select ` + expression + ` as entity_key
		from usage_events ` + where + `
		group by entity_key
	)`
	var total int64
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 || int64(offset) >= total {
		return nil, total, nil
	}
	query := `select ` + expression + ` as entity_key
		from usage_events ` + where + `
		group by entity_key
		order by ` + orderBy + `
		limit ? offset ?`
	pageArgs := append(append([]any{}, args...), limit, offset)
	rows, err := r.db.QueryContext(ctx, query, pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	keys := make([]string, 0, limit)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, 0, err
		}
		keys = append(keys, key)
	}
	return keys, total, rows.Err()
}

func addAggregateKeysCondition(expression string, values []string, conditions *[]string, args *[]any) {
	if len(values) == 0 {
		return
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(values)), ",")
	*conditions = append(*conditions, fmt.Sprintf("(%s) in (%s)", expression, placeholders))
	for _, value := range values {
		*args = append(*args, value)
	}
}
