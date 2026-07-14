package panel

import (
	"net/http"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
)

type Handler struct {
	App *app.Context
}

func (h *Handler) ManagementHTML(w http.ResponseWriter, r *http.Request) {
	h.App.PanelService.ServeManagementHTML(w, r, response.Error)
}

func (h *Handler) ManagementAsset(w http.ResponseWriter, r *http.Request) {
	h.App.PanelService.ServeManagementAsset(w, r, response.Error)
}
