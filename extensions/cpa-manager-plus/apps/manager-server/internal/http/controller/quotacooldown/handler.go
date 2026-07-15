package quotacooldown

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/middleware"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	quotacooldownrepo "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/quotacooldown"
)

type Handler struct {
	App *app.Context
}

// cooldownItem is the minimal, read-only view of an active quota cooldown that
// the panel needs to render a derived hint on the auth file card. It deliberately
// omits internal/account-snapshot fields.
type cooldownItem struct {
	AuthFileName string `json:"authFileName"`
	AuthIndex    string `json:"authIndex"`
	Provider     string `json:"provider"`
	Owner        string `json:"owner"`
	RecoverAtMs  int64  `json:"recoverAtMs"`
	DisabledAtMs int64  `json:"disabledAtMs"`
	CreatedAtMs  int64  `json:"createdAtMs"`
}

type listResponse struct {
	Items      []cooldownItem `json:"items"`
	Total      int64          `json:"total"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalPages int            `json:"total_pages"`
	HasMore    bool           `json:"has_more"`
}

// Handle exposes the currently active quota cooldowns so the panel can show a
// derived "CPAMP cooldown in progress" hint next to the affected auth files.
// It is read-only and never modifies cooldown ownership or the native CPA
// disabled state.
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	path := strings.TrimRight(r.URL.Path, "/")
	if path != "/usage-service/quota-cooldowns" {
		response.MethodNotAllowed(w)
		return
	}
	if r.Method != http.MethodGet {
		response.MethodNotAllowed(w)
		return
	}
	if !middleware.AuthorizePanel(w, r, h.App.AdminAuthService) {
		return
	}

	query := r.URL.Query()
	page, err := parsePositiveInt(query.Get("page"), 1)
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
	pageSize, err := parsePositiveInt(pageSizeRaw, 100)
	if err != nil {
		response.Error(w, http.StatusBadRequest, errors.New("page_size must be a positive integer"))
		return
	}
	if pageSize > 200 {
		pageSize = 200
	}
	cooldowns, err := h.App.Store.QuotaCooldowns.ListActivePage(r.Context(), quotacooldownrepo.ListQuery{
		Provider: query.Get("provider"),
		Auth:     query.Get("auth"),
		Search:   query.Get("search"),
		Limit:    pageSize,
		Offset:   (page - 1) * pageSize,
	})
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err)
		return
	}

	items := make([]cooldownItem, 0, len(cooldowns.Items))
	for _, c := range cooldowns.Items {
		items = append(items, mapCooldown(c))
	}
	totalPages := 0
	if cooldowns.Total > 0 {
		totalPages = int((cooldowns.Total + int64(pageSize) - 1) / int64(pageSize))
	}
	response.JSON(w, http.StatusOK, listResponse{
		Items:      items,
		Total:      cooldowns.Total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
		HasMore:    int64(page)*int64(pageSize) < cooldowns.Total,
	})
}

func parsePositiveInt(raw string, fallback int) (int, error) {
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

func mapCooldown(c model.QuotaCooldown) cooldownItem {
	return cooldownItem{
		AuthFileName: c.AuthFileName,
		AuthIndex:    c.AuthIndex,
		Provider:     c.Provider,
		Owner:        c.Owner,
		RecoverAtMs:  c.RecoverAtMS,
		DisabledAtMs: c.DisabledAtMS,
		CreatedAtMs:  c.CreatedAtMS,
	}
}
