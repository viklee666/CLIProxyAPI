package codexinspection

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/middleware"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	codexsvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/codexinspection"
)

type Handler struct {
	App *app.Context
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if !middleware.AuthorizePanel(w, r, h.App.AdminAuthService) {
		return
	}

	path := strings.Trim(strings.TrimRight(r.URL.Path, "/"), " ")
	switch {
	case path == "/v0/management/codex-inspection/cooldown-disable":
		if r.Method != http.MethodPost {
			response.MethodNotAllowed(w)
			return
		}
		var req codexsvc.DisableUntilResetRequest
		if !response.DecodeJSON(w, r, &req, response.JSONDecodeOptions{}) {
			return
		}
		if err := h.App.CodexInspectionService.DisableUntilReset(r.Context(), req); err != nil {
			response.Error(w, codexInspectionErrorStatus(err), err)
			return
		}
		response.JSON(w, http.StatusOK, map[string]any{"ok": true, "recoverAtMs": req.RecoverAtMS})
	case path == "/v0/management/codex-inspection/run":
		if r.Method != http.MethodPost {
			response.MethodNotAllowed(w)
			return
		}
		result, err := h.App.CodexInspectionService.Run(context.WithoutCancel(r.Context()), codexsvc.RunRequest{
			TriggerType: "manual",
			TriggerKey:  "manual",
		})
		if err != nil {
			response.Error(w, codexInspectionErrorStatus(err), err)
			return
		}
		paged, err := h.App.CodexInspectionService.GetRunPage(r.Context(), result.Run.ID, defaultDetailPageRequest())
		if err != nil {
			response.Error(w, codexInspectionErrorStatus(err), err)
			return
		}
		response.JSON(w, http.StatusOK, paged)
	case path == "/v0/management/codex-inspection/runs":
		if r.Method != http.MethodGet {
			response.MethodNotAllowed(w)
			return
		}
		query := r.URL.Query()
		page, err := parsePositiveQueryInt(query.Get("page"), 1)
		if err != nil {
			response.Error(w, http.StatusBadRequest, errors.New("page must be a positive integer"))
			return
		}
		if page > 1_000_000 {
			response.Error(w, http.StatusBadRequest, errors.New("page must be less than or equal to 1000000"))
			return
		}
		pageSizeRaw := query.Get("page_size")
		if strings.TrimSpace(pageSizeRaw) == "" {
			pageSizeRaw = query.Get("limit")
		}
		pageSize, err := parsePositiveQueryInt(pageSizeRaw, 20)
		if err != nil {
			response.Error(w, http.StatusBadRequest, errors.New("page_size must be a positive integer"))
			return
		}
		if pageSize > 100 {
			pageSize = 100
		}
		runs, err := h.App.CodexInspectionService.ListRunsPage(r.Context(), page, pageSize)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err)
			return
		}
		response.JSON(w, http.StatusOK, runs)
	default:
		if !strings.HasPrefix(path, "/v0/management/codex-inspection/runs/") {
			response.MethodNotAllowed(w)
			return
		}
		idRaw := strings.TrimPrefix(path, "/v0/management/codex-inspection/runs/")
		actionPath := false
		if strings.HasSuffix(idRaw, "/actions") {
			actionPath = true
			idRaw = strings.TrimSuffix(idRaw, "/actions")
		}
		id, err := strconv.ParseInt(idRaw, 10, 64)
		if err != nil || id <= 0 {
			if err == nil {
				err = errors.New("run id is required")
			}
			response.Error(w, http.StatusBadRequest, err)
			return
		}
		if actionPath {
			if r.Method != http.MethodPost {
				response.MethodNotAllowed(w)
				return
			}
			var req codexsvc.ExecuteActionsRequest
			if !response.DecodeJSON(w, r, &req, response.JSONDecodeOptions{}) {
				return
			}
			result, err := h.App.CodexInspectionService.ExecuteManualActions(r.Context(), id, req)
			if err != nil {
				response.Error(w, codexInspectionErrorStatus(err), err)
				return
			}
			detail, err := h.App.CodexInspectionService.GetRunPage(r.Context(), id, defaultDetailPageRequest())
			if err != nil {
				response.Error(w, codexInspectionErrorStatus(err), err)
				return
			}
			result.Detail = detail
			response.JSON(w, http.StatusOK, result)
			return
		}
		if r.Method != http.MethodGet {
			response.MethodNotAllowed(w)
			return
		}
		pageRequest, err := parseDetailPageRequest(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err)
			return
		}
		detail, err := h.App.CodexInspectionService.GetRunPage(r.Context(), id, pageRequest)
		if err != nil {
			response.Error(w, codexInspectionErrorStatus(err), err)
			return
		}
		response.JSON(w, http.StatusOK, detail)
	}
}

func defaultDetailPageRequest() codexsvc.RunDetailPageRequest {
	return codexsvc.RunDetailPageRequest{
		IncludeResults:  true,
		IncludeLogs:     true,
		ResultsPage:     1,
		ResultsPageSize: 100,
		LogsPage:        1,
		LogsPageSize:    100,
	}
}

func parseDetailPageRequest(r *http.Request) (codexsvc.RunDetailPageRequest, error) {
	query := r.URL.Query()
	result := defaultDetailPageRequest()
	var err error
	result.IncludeResults, err = parseOptionalBool(query.Get("include_results"), true)
	if err != nil {
		return codexsvc.RunDetailPageRequest{}, errors.New("include_results must be a boolean")
	}
	result.IncludeLogs, err = parseOptionalBool(query.Get("include_logs"), true)
	if err != nil {
		return codexsvc.RunDetailPageRequest{}, errors.New("include_logs must be a boolean")
	}
	result.ResultsPage, err = parsePositiveQueryInt(query.Get("results_page"), 1)
	if err != nil {
		return codexsvc.RunDetailPageRequest{}, errors.New("results_page must be a positive integer")
	}
	if result.ResultsPage > 1_000_000 {
		return codexsvc.RunDetailPageRequest{}, errors.New("results_page must be less than or equal to 1000000")
	}
	result.ResultsPageSize, err = parsePositiveQueryInt(query.Get("results_page_size"), 100)
	if err != nil {
		return codexsvc.RunDetailPageRequest{}, errors.New("results_page_size must be a positive integer")
	}
	if result.ResultsPageSize > 200 {
		result.ResultsPageSize = 200
	}
	result.LogsPage, err = parsePositiveQueryInt(query.Get("logs_page"), 1)
	if err != nil {
		return codexsvc.RunDetailPageRequest{}, errors.New("logs_page must be a positive integer")
	}
	if result.LogsPage > 1_000_000 {
		return codexsvc.RunDetailPageRequest{}, errors.New("logs_page must be less than or equal to 1000000")
	}
	result.LogsPageSize, err = parsePositiveQueryInt(query.Get("logs_page_size"), 100)
	if err != nil {
		return codexsvc.RunDetailPageRequest{}, errors.New("logs_page_size must be a positive integer")
	}
	if result.LogsPageSize > 200 {
		result.LogsPageSize = 200
	}
	return result, nil
}

func parsePositiveQueryInt(raw string, fallback int) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil || value <= 0 {
		return 0, errors.New("query value must be a positive integer")
	}
	return value, nil
}

func parseOptionalBool(raw string, fallback bool) (bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(trimmed)
	if err != nil {
		return false, err
	}
	return value, nil
}

func codexInspectionErrorStatus(err error) int {
	switch {
	case errors.Is(err, codexsvc.ErrRunNotFound):
		return http.StatusNotFound
	case errors.Is(err, codexsvc.ErrRunAlreadyActive),
		errors.Is(err, codexsvc.ErrRunNotCompleted):
		return http.StatusConflict
	case errors.Is(err, codexsvc.ErrNotConfigured):
		return http.StatusPreconditionFailed
	case errors.Is(err, codexsvc.ErrActionIDsRequired),
		errors.Is(err, codexsvc.ErrTooManyActionIDs),
		errors.Is(err, codexsvc.ErrNoActionableResults),
		errors.Is(err, codexsvc.ErrCooldownResetRequired),
		errors.Is(err, codexsvc.ErrAuthFileNotFound):
		return http.StatusBadRequest
	case errors.Is(err, codexsvc.ErrAuthFileAlreadyDisabled):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
