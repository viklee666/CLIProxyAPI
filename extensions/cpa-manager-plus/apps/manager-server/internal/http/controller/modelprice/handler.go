package modelprice

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/middleware"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	modelpricesvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/modelprice"
)

type Handler struct {
	App *app.Context
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if !middleware.AuthorizePanel(w, r, h.App.AdminAuthService) {
		return
	}

	path := strings.TrimRight(r.URL.Path, "/")
	switch {
	case path == "/v0/management/model-prices/usage-summary" && r.Method == http.MethodGet:
		summary, err := h.App.ModelPriceService.UsageSummary(r.Context(), h.App.Config.QueryLimit)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err)
			return
		}
		response.JSON(w, http.StatusOK, summary)
	case path == "/v0/management/model-prices" && r.Method == http.MethodGet:
		page, pageSize, err := parseModelPricePage(r)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err)
			return
		}
		prices, err := h.App.ModelPriceService.ListPage(r.Context(), modelpricesvc.ListRequest{
			Search:   r.URL.Query().Get("search"),
			Page:     page,
			PageSize: pageSize,
		})
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err)
			return
		}
		response.JSON(w, http.StatusOK, prices)
	case path == "/v0/management/model-prices" && r.Method == http.MethodPut:
		var req modelpricesvc.UpdateRequest
		if !response.DecodeJSON(w, r, &req, response.JSONDecodeOptions{}) {
			return
		}
		prices, err := h.App.ModelPriceService.Replace(r.Context(), req.Prices)
		if err != nil {
			response.Error(w, http.StatusBadRequest, err)
			return
		}
		response.JSON(w, http.StatusOK, map[string]any{"prices": prices})
	case path == "/v0/management/model-prices/sync" && r.Method == http.MethodPost:
		var req modelpricesvc.SyncRequest
		if !response.DecodeJSON(w, r, &req, response.JSONDecodeOptions{AllowEmpty: true}) {
			return
		}
		if len(req.Models) > modelpricesvc.MaxSyncModels {
			response.Error(w, http.StatusBadRequest, errors.New("too many models requested for price sync"))
			return
		}
		result, err := h.App.ModelPriceService.Sync(r.Context(), req)
		if err != nil {
			response.Error(w, response.ModelPriceErrorStatus(err), err)
			return
		}
		response.JSON(w, http.StatusOK, result)
	default:
		response.MethodNotAllowed(w)
	}
}

func parseModelPricePage(r *http.Request) (int, int, error) {
	page, err := parseModelPricePositiveInt(r.URL.Query().Get("page"), 1)
	if err != nil {
		return 0, 0, errors.New("page must be a positive integer")
	}
	pageSize, err := parseModelPricePositiveInt(r.URL.Query().Get("page_size"), 100)
	if err != nil || pageSize > 200 {
		return 0, 0, errors.New("page_size must be between 1 and 200")
	}
	return page, pageSize, nil
}

func parseModelPricePositiveInt(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, errors.New("value must be a positive integer")
	}
	return value, nil
}
