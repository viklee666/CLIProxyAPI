package management

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

type apiKeyUsageTestResponse struct {
	Items      []apiKeyUsageItem `json:"items"`
	Total      int               `json:"total"`
	Page       int               `json:"page"`
	PageSize   int               `json:"page_size"`
	TotalPages int               `json:"total_pages"`
	HasMore    bool              `json:"has_more"`
}

func decodeAPIKeyUsageResponse(t *testing.T, recorder *httptest.ResponseRecorder) apiKeyUsageTestResponse {
	t.Helper()
	var payload apiKeyUsageTestResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return payload
}

func findAPIKeyUsageItem(payload apiKeyUsageTestResponse, provider, compositeKey string) *apiKeyUsageItem {
	for i := range payload.Items {
		if payload.Items[i].Provider == provider && payload.Items[i].CompositeKey == compositeKey {
			return &payload.Items[i]
		}
	}
	return nil
}

func sumRecentRequestBuckets(buckets []coreauth.RecentRequestBucket) (int64, int64) {
	var success int64
	var failed int64
	for _, bucket := range buckets {
		success += bucket.Success
		failed += bucket.Failed
	}
	return success, failed
}

func TestGetAPIKeyUsage_GroupsByProviderAndAPIKey(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")

	manager := coreauth.NewManager(nil, nil, nil)
	if _, err := manager.Register(context.Background(), &coreauth.Auth{
		ID:       "codex-auth",
		Provider: "codex",
		Attributes: map[string]string{
			"api_key":  "codex-key",
			"base_url": "https://codex.example.com",
		},
	}); err != nil {
		t.Fatalf("register codex auth: %v", err)
	}
	if _, err := manager.Register(context.Background(), &coreauth.Auth{
		ID:       "claude-auth",
		Provider: "claude",
		Attributes: map[string]string{
			"api_key":  "claude-key",
			"base_url": "https://claude.example.com",
		},
	}); err != nil {
		t.Fatalf("register claude auth: %v", err)
	}

	manager.MarkResult(context.Background(), coreauth.Result{AuthID: "codex-auth", Provider: "codex", Model: "gpt-5", Success: true})
	manager.MarkResult(context.Background(), coreauth.Result{AuthID: "codex-auth", Provider: "codex", Model: "gpt-5", Success: false})
	manager.MarkResult(context.Background(), coreauth.Result{AuthID: "claude-auth", Provider: "claude", Model: "claude-4", Success: true})

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/v0/management/api-key-usage", nil)
	ginCtx.Request = req
	h.GetAPIKeyUsage(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	payload := decodeAPIKeyUsageResponse(t, rec)
	if payload.Total != 2 || payload.Page != 1 || payload.PageSize != defaultAPIKeyUsagePageSize || payload.TotalPages != 1 || payload.HasMore {
		t.Fatalf("pagination metadata = %+v", payload)
	}

	codexEntry := findAPIKeyUsageItem(payload, "codex", "https://codex.example.com|codex-key")
	if codexEntry == nil {
		t.Fatalf("missing codex entry: %+v", payload.Items)
	}
	if codexEntry.Success != 1 || codexEntry.Failed != 1 {
		t.Fatalf("codex totals = %d/%d, want 1/1", codexEntry.Success, codexEntry.Failed)
	}
	if len(codexEntry.RecentRequests) != 20 {
		t.Fatalf("codex buckets len = %d, want 20", len(codexEntry.RecentRequests))
	}
	codexSuccess, codexFailed := sumRecentRequestBuckets(codexEntry.RecentRequests)
	if codexSuccess != 1 || codexFailed != 1 {
		t.Fatalf("codex totals = %d/%d, want 1/1", codexSuccess, codexFailed)
	}

	claudeEntry := findAPIKeyUsageItem(payload, "claude", "https://claude.example.com|claude-key")
	if claudeEntry == nil {
		t.Fatalf("missing claude entry: %+v", payload.Items)
	}
	if claudeEntry.Success != 1 || claudeEntry.Failed != 0 {
		t.Fatalf("claude totals = %d/%d, want 1/0", claudeEntry.Success, claudeEntry.Failed)
	}
	if len(claudeEntry.RecentRequests) != 20 {
		t.Fatalf("claude buckets len = %d, want 20", len(claudeEntry.RecentRequests))
	}
	claudeSuccess, claudeFailed := sumRecentRequestBuckets(claudeEntry.RecentRequests)
	if claudeSuccess != 1 || claudeFailed != 0 {
		t.Fatalf("claude totals = %d/%d, want 1/0", claudeSuccess, claudeFailed)
	}
}

func TestGetAPIKeyUsage_GroupsOpenAICompatibleByCompatName(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")

	manager := coreauth.NewManager(nil, nil, nil)
	if _, err := manager.Register(context.Background(), &coreauth.Auth{
		ID:       "vast-auth",
		Provider: "openai-compatible-vast",
		Attributes: map[string]string{
			"api_key":     "vast-key",
			"base_url":    "https://www.vastnum.com/v1",
			"compat_name": "VAST",
		},
	}); err != nil {
		t.Fatalf("register vast auth: %v", err)
	}

	manager.MarkResult(context.Background(), coreauth.Result{AuthID: "vast-auth", Provider: "openai-compatible-vast", Model: "gpt-5", Success: true})

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/v0/management/api-key-usage", nil)
	ginCtx.Request = req
	h.GetAPIKeyUsage(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	payload := decodeAPIKeyUsageResponse(t, rec)
	vastEntry := findAPIKeyUsageItem(payload, "vast", "https://www.vastnum.com/v1|vast-key")
	if vastEntry == nil {
		t.Fatalf("missing compat provider entry in payload: %+v", payload.Items)
	}
	if vastEntry.Success != 1 || vastEntry.Failed != 0 {
		t.Fatalf("vast totals = %d/%d, want 1/0", vastEntry.Success, vastEntry.Failed)
	}
}

func TestGetAPIKeyUsage_PaginatesAndFiltersBeforeRecentSnapshots(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	manager := coreauth.NewManager(nil, nil, nil)
	providers := []string{"gemini", "codex", "claude"}
	for index, provider := range providers {
		if _, err := manager.Register(context.Background(), &coreauth.Auth{
			ID:       fmt.Sprintf("auth-%d", index),
			Provider: provider,
			Attributes: map[string]string{
				"api_key":  fmt.Sprintf("key-%d", index),
				"base_url": fmt.Sprintf("https://%s.example.com", provider),
			},
		}); err != nil {
			t.Fatalf("register %s auth: %v", provider, err)
		}
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)
	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/api-key-usage?provider=codex,gemini&search=key&page=2&page_size=1", nil)
	h.GetAPIKeyUsage(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	payload := decodeAPIKeyUsageResponse(t, rec)
	if payload.Total != 2 || payload.Page != 2 || payload.PageSize != 1 || payload.TotalPages != 2 || payload.HasMore {
		t.Fatalf("pagination metadata = %+v", payload)
	}
	if len(payload.Items) != 1 || payload.Items[0].Provider != "gemini" {
		t.Fatalf("items = %+v, want second provider-filtered item", payload.Items)
	}
}

func TestGetAPIKeyUsage_RejectsPageSizeAboveMaximum(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, coreauth.NewManager(nil, nil, nil))
	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/api-key-usage?page_size=201", nil)
	h.GetAPIKeyUsage(ginCtx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
