package management

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestGetAPIKeysUsesBoundedPaginationAndSearch(t *testing.T) {
	keys := make([]string, 250)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%03d", i)
	}
	cfg := &config.Config{}
	cfg.APIKeys = keys
	h := NewHandlerWithoutConfigFilePath(cfg, nil)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/api-keys?page=2&page_size=100", nil)
	h.GetAPIKeys(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var body struct {
		APIKeys    []string `json:"api-keys"`
		Page       int      `json:"page"`
		PageSize   int      `json:"page_size"`
		Total      int      `json:"total"`
		TotalPages int      `json:"total_pages"`
		HasMore    bool     `json:"has_more"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.APIKeys) != 100 || body.APIKeys[0] != "key-100" || body.APIKeys[99] != "key-199" {
		t.Fatalf("unexpected page: first=%q last=%q len=%d", body.APIKeys[0], body.APIKeys[len(body.APIKeys)-1], len(body.APIKeys))
	}
	if body.Page != 2 || body.PageSize != 100 || body.Total != 250 || body.TotalPages != 3 || !body.HasMore {
		t.Fatalf("unexpected metadata: %+v", body)
	}

	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/api-keys?search=249", nil)
	h.GetAPIKeys(ctx)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "key-249") || strings.Contains(recorder.Body.String(), "key-248") {
		t.Fatalf("unexpected filtered response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestGetConfigCollectionRejectsOversizedPage(t *testing.T) {
	cfg := &config.Config{}
	cfg.APIKeys = []string{"key"}
	h := NewHandlerWithoutConfigFilePath(cfg, nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/api-keys?page_size=201", nil)
	h.GetAPIKeys(ctx)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestPostAPIKeyAddsOneRecordAndRejectsDuplicate(t *testing.T) {
	cfg := &config.Config{}
	cfg.APIKeys = []string{"existing"}
	h := NewHandler(cfg, writeTestConfigFile(t), nil)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/api-keys", strings.NewReader(`{"value":"new-key"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.PostAPIKey(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := h.cfg.APIKeys; len(got) != 2 || got[1] != "new-key" {
		t.Fatalf("api keys = %#v", got)
	}

	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/api-keys", strings.NewReader(`{"api-key":"new-key"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.PostAPIKey(ctx)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want %d; body=%s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
}

func TestPutProviderCollectionRejectsOversizedBody(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/gemini-api-key", bytes.NewReader(bytes.Repeat([]byte(" "), maxConfigCollectionBodyBytes+1)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.PutGeminiKeys(ctx)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
}

func TestGetProviderCollectionUsesPagination(t *testing.T) {
	entries := make([]config.GeminiKey, 205)
	for i := range entries {
		entries[i] = config.GeminiKey{APIKey: fmt.Sprintf("gemini-%03d", i)}
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{GeminiKey: entries}, nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/gemini-api-key?page=3&page_size=100", nil)
	h.GetGeminiKeys(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var body struct {
		Items   []map[string]any `json:"gemini-api-key"`
		Total   int              `json:"total"`
		HasMore bool             `json:"has_more"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Items) != 5 || body.Total != 205 || body.HasMore {
		t.Fatalf("unexpected page: len=%d total=%d has_more=%v", len(body.Items), body.Total, body.HasMore)
	}
}

func TestGetOAuthCollectionsPaginateNestedItems(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{
		OAuthExcludedModels: map[string][]string{
			"codex":  {"c-1", "c-2", "c-3"},
			"claude": {"a-1", "a-2"},
		},
		OAuthModelAlias: map[string][]config.OAuthModelAlias{
			"codex": {
				{Name: "source-1", Alias: "alias-1"},
				{Name: "source-2", Alias: "alias-2"},
				{Name: "source-3", Alias: "alias-3"},
			},
		},
	}, nil)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/oauth-excluded-models?page=2&page_size=2", nil)
	h.GetOAuthExcludedModels(ctx)
	var excluded struct {
		Items map[string][]string `json:"oauth-excluded-models"`
		Total int                 `json:"total"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &excluded); err != nil {
		t.Fatalf("decode excluded response: %v", err)
	}
	if recorder.Code != http.StatusOK || excluded.Total != 5 || len(excluded.Items["codex"]) != 2 {
		t.Fatalf("unexpected excluded page: status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/oauth-model-alias?channel=codex&page=2&page_size=2", nil)
	h.GetOAuthModelAlias(ctx)
	var aliases struct {
		Items map[string][]config.OAuthModelAlias `json:"oauth-model-alias"`
		Total int                                 `json:"total"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &aliases); err != nil {
		t.Fatalf("decode alias response: %v", err)
	}
	if recorder.Code != http.StatusOK || aliases.Total != 3 || len(aliases.Items["codex"]) != 1 {
		t.Fatalf("unexpected alias page: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestGetStaticModelDefinitionsUsesPagination(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "channel", Value: "codex"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/model-definitions/codex?page=1&page_size=1", nil)
	h.GetStaticModelDefinitions(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var body struct {
		Models   []map[string]any `json:"models"`
		PageSize int              `json:"page_size"`
		Total    int              `json:"total"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Models) != 1 || body.PageSize != 1 || body.Total < 1 {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
}
