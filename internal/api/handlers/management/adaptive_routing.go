package management

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

const (
	defaultAdaptiveScorePageSize = 100
	maxAdaptiveScorePageSize     = 200
)

type adaptiveScoreQuery struct {
	page     int
	pageSize int
	search   string
	eligible *bool
}

func parseAdaptiveScoreQuery(c *gin.Context) (adaptiveScoreQuery, error) {
	query := adaptiveScoreQuery{
		page:     1,
		pageSize: defaultAdaptiveScorePageSize,
	}
	if c == nil {
		return query, nil
	}
	if rawPage := strings.TrimSpace(c.Query("page")); rawPage != "" {
		page, errPage := strconv.Atoi(rawPage)
		if errPage != nil || page <= 0 || page > 1_000_000 {
			return query, fmt.Errorf("page must be an integer between 1 and 1000000")
		}
		query.page = page
	}
	if rawPageSize := strings.TrimSpace(c.Query("page_size")); rawPageSize != "" {
		pageSize, errPageSize := strconv.Atoi(rawPageSize)
		if errPageSize != nil || pageSize <= 0 || pageSize > maxAdaptiveScorePageSize {
			return query, fmt.Errorf("page_size must be an integer between 1 and %d", maxAdaptiveScorePageSize)
		}
		query.pageSize = pageSize
	}
	query.search = strings.ToLower(strings.TrimSpace(c.Query("search")))
	if rawEligible := strings.TrimSpace(c.Query("eligible")); rawEligible != "" {
		eligible, errEligible := strconv.ParseBool(rawEligible)
		if errEligible != nil {
			return query, fmt.Errorf("eligible must be true or false")
		}
		query.eligible = &eligible
	}
	return query, nil
}

func adaptiveScoreMatchesQuery(item coreauth.AdaptiveScore, query adaptiveScoreQuery) bool {
	if query.eligible != nil && item.Eligible != *query.eligible {
		return false
	}
	if query.search == "" {
		return true
	}
	return strings.Contains(strings.ToLower(item.AuthID), query.search) ||
		strings.Contains(strings.ToLower(item.AuthIndex), query.search) ||
		strings.Contains(strings.ToLower(item.Provider), query.search)
}

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
	query, errQuery := parseAdaptiveScoreQuery(c)
	if errQuery != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errQuery.Error()})
		return
	}
	provider := strings.ToLower(strings.TrimSpace(c.Query("provider")))
	model := strings.TrimSpace(c.Query("model"))
	snapshot := h.authManager.AdaptiveRoutingScores(provider, model)
	filtered := make([]coreauth.AdaptiveScore, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		if adaptiveScoreMatchesQuery(item, query) {
			filtered = append(filtered, item)
		}
	}
	total := len(filtered)
	totalPages := 0
	if total > 0 {
		totalPages = (total + query.pageSize - 1) / query.pageSize
		if query.page > totalPages {
			query.page = totalPages
		}
	}
	start := (query.page - 1) * query.pageSize
	if start > total {
		start = total
	}
	end := start + query.pageSize
	if end > total {
		end = total
	}
	items := filtered[start:end]
	if items == nil {
		items = []coreauth.AdaptiveScore{}
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled":     snapshot.Enabled,
		"config":      snapshot.Config,
		"items":       items,
		"total":       total,
		"page":        query.page,
		"page_size":   query.pageSize,
		"total_pages": totalPages,
		"has_more":    query.page < totalPages,
	})
}
