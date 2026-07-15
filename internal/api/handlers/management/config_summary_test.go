package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestGetConfigSummaryReturnsCountsWithoutRecords(t *testing.T) {
	h := &Handler{cfg: &config.Config{
		SDKConfig:           config.SDKConfig{APIKeys: []string{"one", "two"}},
		GeminiKey:           []config.GeminiKey{{APIKey: "secret"}},
		CodexKey:            []config.CodexKey{{BaseURL: "https://codex.example"}},
		OpenAICompatibility: []config.OpenAICompatibility{{Name: "compat"}},
	}}
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodGet, "/v0/management/config/summary", nil)

	h.GetConfigSummary(ginContext)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var payload map[string]int
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if payload["api_keys"] != 2 || payload["gemini_api_keys"] != 1 || payload["codex_api_keys"] != 1 || payload["openai_compatibility"] != 1 {
		t.Fatalf("summary = %#v", payload)
	}
	if payload["provider_credentials"] != 3 {
		t.Fatalf("provider_credentials = %d, want 3", payload["provider_credentials"])
	}
	if strings.Contains(recorder.Body.String(), "secret") || len(recorder.Body.Bytes()) > 1024 {
		t.Fatalf("summary unexpectedly contains provider records: %s", recorder.Body.String())
	}
}
