// Package tenant serves the read-only Manager Plus observability endpoints.
package tenant

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/middleware"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	monitoringsvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/monitoring"
	usagesvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/usage"
)

// Hashes are lowercase hex strings, so this sentinel can never match a real
// client key hash. It prevents an empty tenant key set becoming an unfiltered
// `IN` condition in downstream query layers.
const noTenantKeyHash = "__tenant_without_api_keys__"

type Handler struct {
	App *app.Context
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.App == nil || h.App.TenantAuthService == nil {
		response.Error(w, http.StatusServiceUnavailable, errors.New("tenant observability service is unavailable"))
		return
	}
	subject, ok := middleware.TenantSubject(r.Context())
	if !ok {
		response.Error(w, http.StatusUnauthorized, errors.New("invalid tenant session"))
		return
	}
	hashes, errHashes := h.App.TenantAuthService.APIKeyHashes(r.Context(), subject.TenantID)
	if errHashes != nil {
		response.Error(w, http.StatusServiceUnavailable, errHashes)
		return
	}
	if len(hashes) == 0 {
		hashes = []string{noTenantKeyHash}
	}

	path := strings.TrimRight(r.URL.Path, "/")
	switch path {
	case "/v0/tenant/monitoring/analytics":
		h.analytics(w, r, hashes)
	case "/v0/tenant/dashboard/summary":
		h.dashboard(w, r, hashes)
	case "/v0/tenant/usage":
		h.usage(w, r, hashes)
	default:
		response.MethodNotAllowed(w)
	}
}

func (h *Handler) analytics(w http.ResponseWriter, r *http.Request, hashes []string) {
	if r.Method != http.MethodPost {
		response.MethodNotAllowed(w)
		return
	}
	var request monitoringsvc.Request
	if !response.DecodeJSON(w, r, &request, response.JSONDecodeOptions{}) {
		return
	}
	if request.FromMS <= 0 || request.ToMS <= 0 || request.FromMS >= request.ToMS {
		response.Error(w, http.StatusBadRequest, errors.New("from_ms and to_ms are required and from_ms must be less than to_ms"))
		return
	}
	request.Filters.APIKeyHashes = append([]string(nil), hashes...)
	result, errAnalytics := h.App.MonitoringService.Analytics(r.Context(), request)
	if errAnalytics != nil {
		response.Error(w, http.StatusInternalServerError, errAnalytics)
		return
	}
	response.JSON(w, http.StatusOK, result)
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request, hashes []string) {
	if r.Method != http.MethodGet {
		response.MethodNotAllowed(w)
		return
	}
	query := r.URL.Query()
	todayStartMS, errToday := parsePositiveInt64(query.Get("today_start_ms"), "today_start_ms")
	if errToday != nil {
		response.Error(w, http.StatusBadRequest, errToday)
		return
	}
	nowMS, errNow := parseOptionalInt64(query.Get("now_ms"), "now_ms")
	if errNow != nil {
		response.Error(w, http.StatusBadRequest, errNow)
		return
	}
	if nowMS <= 0 {
		nowMS = time.Now().UTC().UnixMilli()
	}
	if nowMS <= todayStartMS {
		response.Error(w, http.StatusBadRequest, errors.New("now_ms must be greater than today_start_ms"))
		return
	}
	result, errAnalytics := h.App.MonitoringService.Analytics(r.Context(), monitoringsvc.Request{
		FromMS: todayStartMS,
		ToMS:   nowMS,
		NowMS:  nowMS,
		Filters: monitoringsvc.Filters{
			APIKeyHashes:  append([]string(nil), hashes...),
			IncludeFailed: boolPointer(true),
		},
		Include: monitoringsvc.Include{
			Summary:            true,
			SummaryProfile:     "compact",
			Timeline:           true,
			ModelShare:         true,
			RecentFailures:     5,
			ChannelShare:       true,
			FailureSources:     true,
			Granularity:        "hour",
			SummaryPercentiles: false,
		},
	})
	if errAnalytics != nil {
		response.Error(w, http.StatusInternalServerError, errAnalytics)
		return
	}
	response.JSON(w, http.StatusOK, result)
}

func (h *Handler) usage(w http.ResponseWriter, r *http.Request, hashes []string) {
	if r.Method != http.MethodGet {
		response.MethodNotAllowed(w)
		return
	}
	query := r.URL.Query()
	page, errPage := positiveQueryInt(query.Get("page"), 1)
	if errPage != nil || page > 1_000_000 {
		response.Error(w, http.StatusBadRequest, errors.New("page must be a positive integer less than or equal to 1000000"))
		return
	}
	pageSizeRaw := query.Get("page_size")
	if strings.TrimSpace(pageSizeRaw) == "" {
		pageSizeRaw = query.Get("limit")
	}
	pageSize, errPageSize := positiveQueryInt(pageSizeRaw, 200)
	if errPageSize != nil {
		response.Error(w, http.StatusBadRequest, errors.New("page_size must be a positive integer"))
		return
	}
	if pageSize > 500 {
		pageSize = 500
	}
	result, errUsage := h.App.UsageService.CompatibleUsage(r.Context(), usagesvc.CompatibleUsageRequest{
		Page:         page,
		PageSize:     pageSize,
		Cursor:       query.Get("cursor"),
		APIKeyHashes: append([]string(nil), hashes...),
	})
	if errUsage != nil {
		status := http.StatusInternalServerError
		if errors.Is(errUsage, usagesvc.ErrInvalidUsageCursor) {
			status = http.StatusBadRequest
		}
		response.Error(w, status, errUsage)
		return
	}
	response.JSON(w, http.StatusOK, result)
}

func boolPointer(value bool) *bool { return &value }

func parsePositiveInt64(raw, name string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, errors.New(name + " must be a positive integer")
	}
	return value, nil
}

func parseOptionalInt64(raw, name string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, errors.New(name + " must be an integer")
	}
	return value, nil
}

func positiveQueryInt(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0, errors.New("query value must be a positive integer")
	}
	return value, nil
}
