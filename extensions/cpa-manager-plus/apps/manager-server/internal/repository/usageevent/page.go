package usageevent

import (
	"context"
	"database/sql"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/usage"
)

const maxRecentUsagePageSize = 500

type RecentPageQuery struct {
	PageSize          int
	Offset            int64
	SnapshotMaxID     int64
	BeforeTimestampMS int64
	BeforeID          int64
	// APIKeyHashes is an optional mandatory caller predicate. nil preserves the
	// management query; a non-nil empty slice intentionally yields no rows.
	APIKeyHashes []string
}

type RecentPage struct {
	Items           []model.UsageEvent
	Total           int64
	SnapshotMaxID   int64
	LastTimestampMS int64
	LastID          int64
	HasMore         bool
}

type recentPageItem struct {
	id    int64
	event model.UsageEvent
}

type recentPageScanner interface {
	Scan(dest ...any) error
}

func (r *repository) ListRecentPage(ctx context.Context, query RecentPageQuery) (RecentPage, error) {
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 200
	}
	if pageSize > maxRecentUsagePageSize {
		pageSize = maxRecentUsagePageSize
	}
	if query.Offset < 0 {
		query.Offset = 0
	}

	apiKeyCondition, apiKeyArgs := recentPageAPIKeyCondition(query.APIKeyHashes)
	snapshotMaxID := query.SnapshotMaxID
	if snapshotMaxID <= 0 {
		query := `select coalesce(max(id), 0) from usage_events`
		if apiKeyCondition != "" {
			query += ` where ` + apiKeyCondition
		}
		if err := r.db.QueryRowContext(ctx, query, apiKeyArgs...).Scan(&snapshotMaxID); err != nil {
			return RecentPage{}, err
		}
	}
	if snapshotMaxID == 0 {
		return RecentPage{Items: []model.UsageEvent{}}, nil
	}

	countConditions := []string{"id <= ?"}
	countArgs := []any{snapshotMaxID}
	if apiKeyCondition != "" {
		countConditions = append(countConditions, apiKeyCondition)
		countArgs = append(countArgs, apiKeyArgs...)
	}
	var total int64
	if err := r.db.QueryRowContext(ctx, `select count(*) from usage_events where `+strings.Join(countConditions, " and "), countArgs...).Scan(&total); err != nil {
		return RecentPage{}, err
	}

	conditions := []string{"id <= ?"}
	args := []any{snapshotMaxID}
	if apiKeyCondition != "" {
		conditions = append(conditions, apiKeyCondition)
		args = append(args, apiKeyArgs...)
	}
	if query.BeforeTimestampMS > 0 && query.BeforeID > 0 {
		conditions = append(conditions, `(timestamp_ms < ? or (timestamp_ms = ? and id < ?))`)
		args = append(args, query.BeforeTimestampMS, query.BeforeTimestampMS, query.BeforeID)
		query.Offset = 0
	}
	args = append(args, pageSize+1, query.Offset)

	rows, err := r.db.QueryContext(ctx, `select
		id,
		request_id, event_hash, timestamp_ms, timestamp, provider, executor_type, model, endpoint, method, path,
		auth_type, auth_index, source, source_hash, api_key_hash,
		account_snapshot, auth_label_snapshot, auth_file_snapshot, auth_provider_snapshot, auth_project_id_snapshot, auth_snapshot_at_ms,
		requested_model, resolved_model, reasoning_effort, service_tier, request_service_tier, response_service_tier, cache_input_mode,
		input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_tokens, cache_read_tokens, cache_creation_tokens,
		normalized_uncached_input_tokens, normalized_total_input_tokens, normalized_cache_read_tokens, normalized_cache_creation_tokens, total_tokens,
		latency_ms, ttft_ms, failed, fail_status_code, fail_summary,
		coalesce(response_metadata_json, ''), header_quota_recover_at_ms, header_quota_used_percent, coalesce(header_quota_plan_type, ''), coalesce(header_error_kind, ''), coalesce(header_error_code, ''), coalesce(header_trace_id, ''),
		created_at_ms
	from usage_events
	where `+strings.Join(conditions, " and ")+`
	order by timestamp_ms desc, id desc
	limit ? offset ?`, args...)
	if err != nil {
		return RecentPage{}, err
	}
	defer rows.Close()

	items := make([]recentPageItem, 0, pageSize+1)
	for rows.Next() {
		item, err := scanRecentPageItem(rows)
		if err != nil {
			return RecentPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return RecentPage{}, err
	}

	hasMore := len(items) > pageSize
	if hasMore {
		items = items[:pageSize]
	}
	events := make([]model.UsageEvent, len(items))
	for index := range items {
		events[index] = items[index].event
	}
	page := RecentPage{
		Items:         events,
		Total:         total,
		SnapshotMaxID: snapshotMaxID,
		HasMore:       hasMore,
	}
	if len(items) > 0 {
		last := items[len(items)-1]
		page.LastTimestampMS = last.event.TimestampMS
		page.LastID = last.id
	}
	return page, nil
}

func recentPageAPIKeyCondition(values []string) (string, []any) {
	if values == nil {
		return "", nil
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, rawValue := range values {
		value := strings.ToLower(strings.TrimSpace(rawValue))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	if len(normalized) == 0 {
		return "1 = 0", nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(normalized)), ",")
	args := make([]any, len(normalized))
	for index := range normalized {
		args[index] = normalized[index]
	}
	return "lower(coalesce(api_key_hash, '')) in (" + placeholders + ")", args
}

func scanRecentPageItem(row recentPageScanner) (recentPageItem, error) {
	var id int64
	var event model.UsageEvent
	var requestID, provider, executorType, endpoint, method, path, authType, authIndex, source, sourceHash, apiKeyHash, accountSnapshot, authLabelSnapshot, authFileSnapshot, authProviderSnapshot, authProjectIDSnapshot, requestedModel, resolvedModel, reasoningEffort, serviceTier, requestServiceTier, responseServiceTier, cacheInputMode, failSummary sql.NullString
	var responseMetadataJSON, quotaPlanType, errorKind, errorCode, traceID string
	var authSnapshotAt sql.NullInt64
	var latency, ttft sql.NullInt64
	var failStatusCode sql.NullInt64
	var quotaRecoverAt sql.NullInt64
	var quotaUsedPercent sql.NullFloat64
	var normalizedUncachedInput, normalizedTotalInput, normalizedCacheRead, normalizedCacheCreation sql.NullInt64
	var failed int
	if err := row.Scan(
		&id,
		&requestID,
		&event.EventHash,
		&event.TimestampMS,
		&event.Timestamp,
		&provider,
		&executorType,
		&event.Model,
		&endpoint,
		&method,
		&path,
		&authType,
		&authIndex,
		&source,
		&sourceHash,
		&apiKeyHash,
		&accountSnapshot,
		&authLabelSnapshot,
		&authFileSnapshot,
		&authProviderSnapshot,
		&authProjectIDSnapshot,
		&authSnapshotAt,
		&requestedModel,
		&resolvedModel,
		&reasoningEffort,
		&serviceTier,
		&requestServiceTier,
		&responseServiceTier,
		&cacheInputMode,
		&event.InputTokens,
		&event.OutputTokens,
		&event.ReasoningTokens,
		&event.CachedTokens,
		&event.CacheTokens,
		&event.CacheReadTokens,
		&event.CacheCreationTokens,
		&normalizedUncachedInput,
		&normalizedTotalInput,
		&normalizedCacheRead,
		&normalizedCacheCreation,
		&event.TotalTokens,
		&latency,
		&ttft,
		&failed,
		&failStatusCode,
		&failSummary,
		&responseMetadataJSON,
		&quotaRecoverAt,
		&quotaUsedPercent,
		&quotaPlanType,
		&errorKind,
		&errorCode,
		&traceID,
		&event.CreatedAtMS,
	); err != nil {
		return recentPageItem{}, err
	}
	event.RequestID = requestID.String
	event.Provider = provider.String
	event.ExecutorType = executorType.String
	event.Endpoint = endpoint.String
	event.Method = method.String
	event.Path = path.String
	event.AuthType = authType.String
	event.AuthIndex = authIndex.String
	event.Source = source.String
	event.SourceHash = sourceHash.String
	event.APIKeyHash = apiKeyHash.String
	event.AccountSnapshot = accountSnapshot.String
	event.AuthLabelSnapshot = authLabelSnapshot.String
	event.AuthFileSnapshot = authFileSnapshot.String
	event.AuthProviderSnapshot = authProviderSnapshot.String
	event.AuthProjectIDSnapshot = authProjectIDSnapshot.String
	event.RequestedModel = requestedModel.String
	event.ResolvedModel = resolvedModel.String
	event.ReasoningEffort = reasoningEffort.String
	event.ServiceTier = serviceTier.String
	event.RequestServiceTier = requestServiceTier.String
	event.ResponseServiceTier = responseServiceTier.String
	event.CacheInputMode = cacheInputMode.String
	accounting := usage.NormalizeCacheAccounting(event.CacheInputMode, event.Provider, event.ExecutorType, event.ResolvedModel, event.InputTokens, event.CachedTokens, event.CacheTokens, event.CacheReadTokens, event.CacheCreationTokens)
	event.CacheInputMode = accounting.Mode
	event.NormalizedUncachedInputTokens = accounting.UncachedInputTokens
	event.NormalizedTotalInputTokens = accounting.TotalInputTokens
	event.NormalizedCacheReadTokens = accounting.CacheReadTokens
	event.NormalizedCacheCreationTokens = accounting.CacheCreationTokens
	if normalizedUncachedInput.Valid {
		event.NormalizedUncachedInputTokens = normalizedUncachedInput.Int64
	}
	if normalizedTotalInput.Valid {
		event.NormalizedTotalInputTokens = normalizedTotalInput.Int64
	}
	if normalizedCacheRead.Valid {
		event.NormalizedCacheReadTokens = normalizedCacheRead.Int64
	}
	if normalizedCacheCreation.Valid {
		event.NormalizedCacheCreationTokens = normalizedCacheCreation.Int64
	}
	if authSnapshotAt.Valid {
		event.AuthSnapshotAtMS = authSnapshotAt.Int64
	}
	if failStatusCode.Valid {
		event.FailStatusCode = int(failStatusCode.Int64)
	}
	event.FailSummary = failSummary.String
	event.ResponseMetadataJSON = responseMetadataJSON
	event.ResponseMetadata = usage.ResponseHeaderMetadataFromJSON(responseMetadataJSON)
	if quotaRecoverAt.Valid {
		event.HeaderQuotaRecoverAtMS = quotaRecoverAt.Int64
	}
	if quotaUsedPercent.Valid {
		value := quotaUsedPercent.Float64
		event.HeaderQuotaUsedPercent = &value
	}
	event.HeaderQuotaPlanType = quotaPlanType
	event.HeaderErrorKind = errorKind
	event.HeaderErrorCode = errorCode
	event.HeaderTraceID = traceID
	event.Failed = failed != 0
	if latency.Valid {
		value := latency.Int64
		event.LatencyMS = &value
	}
	if ttft.Valid {
		value := ttft.Int64
		event.TTFTMS = &value
	}
	return recentPageItem{id: id, event: event}, nil
}
