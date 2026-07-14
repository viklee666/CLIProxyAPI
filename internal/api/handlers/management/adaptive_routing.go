package management

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func validateAdaptiveRoutingConfig(cfg config.AdaptiveRoutingConfig) bool {
	if cfg.TopK < 0 || cfg.TopK > 64 {
		return false
	}
	if cfg.EWMAAlpha < 0 || cfg.EWMAAlpha > 1 || cfg.TTFTTargetMS < 0 {
		return false
	}
	if cfg.Weights.Priority < 0 || cfg.Weights.Load < 0 || cfg.Weights.SuccessRate < 0 || cfg.Weights.TTFT < 0 {
		return false
	}
	sticky := cfg.StickyEscape
	return sticky.MinSamples >= 0 &&
		sticky.ErrorRateThreshold >= 0 && sticky.ErrorRateThreshold <= 1 &&
		sticky.TTFTThresholdMS >= 0 && sticky.ActiveRequestThreshold >= 0
}

func (h *Handler) GetAdaptiveRouting(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config unavailable"})
		return
	}
	configured := h.cfg.Routing.Adaptive
	if h.authManager != nil {
		snapshot := h.authManager.AdaptiveRoutingScores("", "")
		if snapshot.Enabled {
			configured = snapshot.Config
		}
	}
	c.JSON(http.StatusOK, configured)
}

func (h *Handler) PutAdaptiveRouting(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config unavailable"})
		return
	}
	var body config.AdaptiveRoutingConfig
	if errBind := c.ShouldBindJSON(&body); errBind != nil || !validateAdaptiveRoutingConfig(body) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid adaptive routing config"})
		return
	}
	h.cfg.Routing.Adaptive = body
	h.persist(c)
}

func (h *Handler) GetAdaptiveRoutingScores(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth manager unavailable"})
		return
	}
	provider := strings.ToLower(strings.TrimSpace(c.Query("provider")))
	model := strings.TrimSpace(c.Query("model"))
	c.JSON(http.StatusOK, h.authManager.AdaptiveRoutingScores(provider, model))
}
