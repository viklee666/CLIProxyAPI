package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestAdaptiveRoutingScoresHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptive := coreauth.NewAdaptiveSelector(config.AdaptiveRoutingConfig{
		TopK: 1,
		Weights: config.AdaptiveRoutingWeights{
			Priority: 1,
		},
	})
	manager := coreauth.NewManager(nil, adaptive, nil)
	if _, errRegister := manager.Register(context.Background(), &coreauth.Auth{
		ID:         "auth-a",
		Provider:   "codex",
		Status:     coreauth.StatusActive,
		Attributes: map[string]string{"priority": "10"},
	}); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}
	handler := &Handler{
		cfg:         &config.Config{Routing: config.RoutingConfig{Strategy: "adaptive"}},
		authManager: manager,
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/routing/adaptive/scores?provider=codex", nil)
	handler.GetAdaptiveRoutingScores(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var snapshot struct {
		coreauth.AdaptiveRoutingSnapshot
		Total      int `json:"total"`
		Page       int `json:"page"`
		PageSize   int `json:"page_size"`
		TotalPages int `json:"total_pages"`
	}
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &snapshot); errDecode != nil {
		t.Fatalf("decode snapshot: %v", errDecode)
	}
	if !snapshot.Enabled || len(snapshot.Items) != 1 || snapshot.Items[0].AuthID != "auth-a" || snapshot.Items[0].Rank != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Total != 1 || snapshot.Page != 1 || snapshot.PageSize != defaultAdaptiveScorePageSize || snapshot.TotalPages != 1 {
		t.Fatalf("pagination metadata = %+v", snapshot)
	}
}

func TestAdaptiveRoutingScoresPaginatesAndFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptive := coreauth.NewAdaptiveSelector(config.AdaptiveRoutingConfig{
		TopK: 3,
		Weights: config.AdaptiveRoutingWeights{
			Priority: 1,
		},
	})
	manager := coreauth.NewManager(nil, adaptive, nil)
	for _, id := range []string{"auth-a", "auth-b", "auth-c"} {
		if _, errRegister := manager.Register(context.Background(), &coreauth.Auth{
			ID:       id,
			Provider: "codex",
			Status:   coreauth.StatusActive,
		}); errRegister != nil {
			t.Fatalf("Register(%s) error = %v", id, errRegister)
		}
	}
	handler := &Handler{
		cfg:         &config.Config{Routing: config.RoutingConfig{Strategy: "adaptive"}},
		authManager: manager,
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/routing/adaptive/scores?provider=codex&eligible=true&page=2&page_size=1", nil)
	handler.GetAdaptiveRoutingScores(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var page struct {
		Items      []coreauth.AdaptiveScore `json:"items"`
		Total      int                      `json:"total"`
		Page       int                      `json:"page"`
		PageSize   int                      `json:"page_size"`
		TotalPages int                      `json:"total_pages"`
		HasMore    bool                     `json:"has_more"`
	}
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &page); errDecode != nil {
		t.Fatalf("decode page: %v", errDecode)
	}
	if len(page.Items) != 1 || page.Total != 3 || page.Page != 2 || page.PageSize != 1 || page.TotalPages != 3 || !page.HasMore {
		t.Fatalf("page = %+v", page)
	}

	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/routing/adaptive/scores?search=auth-b", nil)
	handler.GetAdaptiveRoutingScores(ctx)
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &page); errDecode != nil {
		t.Fatalf("decode filtered page: %v", errDecode)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].AuthID != "auth-b" {
		t.Fatalf("filtered page = %+v", page)
	}
}

func TestAdaptiveRoutingScoresRejectsInvalidPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &Handler{authManager: coreauth.NewManager(nil, coreauth.NewAdaptiveSelector(config.AdaptiveRoutingConfig{}), nil)}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/routing/adaptive/scores?page_size=201", nil)
	handler.GetAdaptiveRoutingScores(ctx)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestAdaptiveRoutingValidationAndStrategy(t *testing.T) {
	if strategy, ok := normalizeRoutingStrategy("adaptive"); !ok || strategy != "adaptive" {
		t.Fatalf("normalizeRoutingStrategy(adaptive) = %q, %v", strategy, ok)
	}
	if validateAdaptiveRoutingConfig(config.AdaptiveRoutingConfig{TopK: 65}) {
		t.Fatal("TopK 65 should be rejected")
	}
	if validateAdaptiveRoutingConfig(config.AdaptiveRoutingConfig{EWMAAlpha: 1.1}) {
		t.Fatal("EWMA alpha 1.1 should be rejected")
	}
	if !validateAdaptiveRoutingConfig(config.AdaptiveRoutingConfig{
		TopK:      3,
		EWMAAlpha: 0.2,
		StickyEscape: config.AdaptiveStickyEscape{
			Enabled:            true,
			MinSamples:         3,
			ErrorRateThreshold: 0.5,
			TTFTThresholdMS:    30_000,
		},
	}) {
		t.Fatal("valid adaptive config rejected")
	}
}
