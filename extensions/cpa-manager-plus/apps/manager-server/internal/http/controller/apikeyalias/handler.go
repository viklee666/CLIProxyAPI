package apikeyalias

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/middleware"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	apikeyaliassvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/apikeyalias"
)

type Handler struct {
	App *app.Context
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if !middleware.AuthorizePanel(w, r, h.App.AdminAuthService) {
		return
	}

	path := strings.TrimRight(r.URL.Path, "/")
	const basePath = "/v0/management/api-key-aliases"
	switch {
	case path == basePath && r.Method == http.MethodGet:
		page, pageSize, err := parseAliasPage(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err)
			return
		}
		aliases, err := h.App.APIKeyAliasService.ListPage(r.Context(), apikeyaliassvc.ListRequest{
			Search:   r.URL.Query().Get("search"),
			Page:     page,
			PageSize: pageSize,
		})
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err)
			return
		}
		response.JSON(w, http.StatusOK, aliases)
	case path == basePath && r.Method == http.MethodPut:
		var req apikeyaliassvc.SaveRequest
		if !response.DecodeJSON(w, r, &req, response.JSONDecodeOptions{}) {
			return
		}
		aliases, err := h.App.APIKeyAliasService.Save(
			r.Context(),
			req.Items,
			req.ActiveAPIKeyHashes,
			req.AllowOrphanAliasCleanup,
		)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err)
			return
		}
		response.JSON(w, http.StatusOK, map[string]any{"items": aliases, "updated": len(aliases)})
	case strings.HasPrefix(path, basePath+"/") && r.Method == http.MethodDelete:
		apiKeyHash := strings.TrimPrefix(path, basePath+"/")
		if err := h.App.APIKeyAliasService.Delete(r.Context(), apiKeyHash); err != nil {
			response.Error(w, http.StatusBadRequest, err)
			return
		}
		response.JSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		response.MethodNotAllowed(w)
	}
}

func parseAliasPage(r *http.Request) (int, int, error) {
	page, err := parsePositiveInt(r.URL.Query().Get("page"), 1)
	if err != nil {
		return 0, 0, errors.New("page must be a positive integer")
	}
	pageSize, err := parsePositiveInt(r.URL.Query().Get("page_size"), 100)
	if err != nil || pageSize > 200 {
		return 0, 0, errors.New("page_size must be between 1 and 200")
	}
	return page, pageSize, nil
}

func parsePositiveInt(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, errors.New("value must be a positive integer")
	}
	return value, nil
}
