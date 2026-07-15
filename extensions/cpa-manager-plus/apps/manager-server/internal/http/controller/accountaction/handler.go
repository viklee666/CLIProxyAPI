package accountaction

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/middleware"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	accountactionsvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/accountaction"
)

type Handler struct {
	App *app.Context
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if !middleware.AuthorizePanel(w, r, h.App.AdminAuthService) {
		return
	}

	path := strings.TrimRight(r.URL.Path, "/")
	if path == "/v0/management/account-action-candidates" {
		h.handleList(w, r)
		return
	}

	if !strings.HasPrefix(path, "/v0/management/account-action-candidates/") {
		response.MethodNotAllowed(w)
		return
	}
	idRaw := strings.TrimPrefix(path, "/v0/management/account-action-candidates/")
	parts := strings.Split(idRaw, "/")
	if len(parts) != 2 {
		response.MethodNotAllowed(w)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		response.Error(w, http.StatusBadRequest, errors.New("candidate id is required"))
		return
	}

	switch parts[1] {
	case "ignore":
		if r.Method != http.MethodPost {
			response.MethodNotAllowed(w)
			return
		}
		item, err := h.App.AccountActionService.Ignore(r.Context(), id)
		h.writeCandidateResult(w, item, err)
	case "resolve":
		if r.Method != http.MethodPost {
			response.MethodNotAllowed(w)
			return
		}
		item, err := h.App.AccountActionService.Resolve(r.Context(), id)
		h.writeCandidateResult(w, item, err)
	case "enable":
		if r.Method != http.MethodPost {
			response.MethodNotAllowed(w)
			return
		}
		item, err := h.App.AccountActionService.Enable(r.Context(), id)
		h.writeCandidateResult(w, item, err)
	case "auth-file":
		if r.Method != http.MethodDelete {
			response.MethodNotAllowed(w)
			return
		}
		item, err := h.App.AccountActionService.DeleteAuthFile(r.Context(), id)
		h.writeCandidateResult(w, item, err)
	default:
		response.MethodNotAllowed(w)
	}
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
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
	pageSizeRaw := query.Get("page_size")
	if strings.TrimSpace(pageSizeRaw) == "" {
		pageSizeRaw = query.Get("limit")
	}
	pageSize, err := parsePositiveQueryInt(pageSizeRaw, 50)
	if err != nil {
		response.Error(w, http.StatusBadRequest, errors.New("page_size must be a positive integer"))
		return
	}
	if pageSize > 200 {
		pageSize = 200
	}
	result, err := h.App.AccountActionService.ListPage(r.Context(), accountactionsvc.ListRequest{
		Status:   query.Get("status"),
		Search:   query.Get("search"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err)
		return
	}
	response.JSON(w, http.StatusOK, result)
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

func (h *Handler) writeCandidateResult(w http.ResponseWriter, item any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, accountactionsvc.ErrCandidateNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, accountactionsvc.ErrCandidateConflict) || errors.Is(err, accountactionsvc.ErrCandidateNotPending) {
			status = http.StatusConflict
		} else if strings.Contains(err.Error(), "usage service is not configured") {
			status = http.StatusPreconditionRequired
		}
		response.Error(w, status, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"item": item})
}
