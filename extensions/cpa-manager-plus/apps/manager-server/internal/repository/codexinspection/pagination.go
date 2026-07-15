package codexinspection

import (
	"context"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type RunsPage struct {
	Items []model.CodexInspectionRun
	Total int64
}

type ResultsPage struct {
	Items []model.CodexInspectionResult
	Total int64
}

type LogsPage struct {
	Items []model.CodexInspectionLog
	Total int64
}

func (r *repository) ListRunsPage(ctx context.Context, limit, offset int) (RunsPage, error) {
	limit, offset = normalizePage(limit, offset)
	var total int64
	if err := r.db.QueryRowContext(ctx, `select count(*) from codex_inspection_runs`).Scan(&total); err != nil {
		return RunsPage{}, err
	}
	rows, err := r.db.QueryContext(ctx, `select
		id, trigger_type, trigger_key, status, started_at_ms, finished_at_ms,
		total_files, probe_set_count, sampled_count, disabled_count, enabled_count,
		delete_count, disable_count, enable_count, reauth_count, keep_count, error,
		settings_json, created_at_ms, updated_at_ms
	from codex_inspection_runs
	order by started_at_ms desc, id desc
	limit ? offset ?`, limit, offset)
	if err != nil {
		return RunsPage{}, err
	}
	defer rows.Close()
	items := make([]model.CodexInspectionRun, 0, limit)
	for rows.Next() {
		item, err := scanRun(rows)
		if err != nil {
			return RunsPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return RunsPage{}, err
	}
	return RunsPage{Items: items, Total: total}, nil
}

func (r *repository) ListResultsPage(ctx context.Context, runID int64, limit, offset int) (ResultsPage, error) {
	limit, offset = normalizePage(limit, offset)
	var total int64
	if err := r.db.QueryRowContext(ctx, `select count(*) from codex_inspection_results where run_id = ?`, runID).Scan(&total); err != nil {
		return ResultsPage{}, err
	}
	rows, err := r.db.QueryContext(ctx, `select
		id, run_id, account_key, file_name, display_account, auth_index, account_id,
		provider, disabled, status, state, action, action_reason, status_code,
		used_percent, is_quota, error, action_status, executed_action, action_error,
		plan_type, quota_windows_json, error_kind, error_detail, created_at_ms
	from codex_inspection_results
	where run_id = ?
	order by file_name asc, display_account asc, id asc
	limit ? offset ?`, runID, limit, offset)
	if err != nil {
		return ResultsPage{}, err
	}
	defer rows.Close()
	items := make([]model.CodexInspectionResult, 0, limit)
	for rows.Next() {
		item, err := scanResult(rows)
		if err != nil {
			return ResultsPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ResultsPage{}, err
	}
	return ResultsPage{Items: items, Total: total}, nil
}

func (r *repository) ListLogsPage(ctx context.Context, runID int64, limit, offset int) (LogsPage, error) {
	limit, offset = normalizePage(limit, offset)
	var total int64
	if err := r.db.QueryRowContext(ctx, `select count(*) from codex_inspection_logs where run_id = ?`, runID).Scan(&total); err != nil {
		return LogsPage{}, err
	}
	rows, err := r.db.QueryContext(ctx, `select id, run_id, level, message, detail_json, created_at_ms
	from codex_inspection_logs
	where run_id = ?
	order by created_at_ms asc, id asc
	limit ? offset ?`, runID, limit, offset)
	if err != nil {
		return LogsPage{}, err
	}
	defer rows.Close()
	items := make([]model.CodexInspectionLog, 0, limit)
	for rows.Next() {
		item, err := scanLog(rows)
		if err != nil {
			return LogsPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return LogsPage{}, err
	}
	return LogsPage{Items: items, Total: total}, nil
}

func normalizePage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
