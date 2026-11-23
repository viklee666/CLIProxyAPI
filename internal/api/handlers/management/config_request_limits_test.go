package management

import (
	"context"
	"fmt"
	"github.com/MoeForge/moego/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestProviderRequestLimitsPatchEveryFamily(t *testing.T) {
	cfg := &config.Config{
		GeminiKey:           []config.GeminiKey{{APIKey: "key"}},
		InteractionsKey:     []config.GeminiKey{{APIKey: "key"}},
		ClaudeKey:           []config.ClaudeKey{{APIKey: "key"}},
		CodexKey:            []config.CodexKey{{APIKey: "key", BaseURL: "https://example.com"}},
		XAIKey:              []config.XAIKey{{APIKey: "key", BaseURL: "https://example.com"}},
		MetaKey:             []config.MetaKey{{APIKey: "key", BaseURL: "https://example.com"}},
		VertexCompatAPIKey:  []config.VertexCompatKey{{APIKey: "key", BaseURL: "https://example.com"}},
		OpenAICompatibility: []config.OpenAICompatibility{{Name: "custom", BaseURL: "https://example.com"}},
	}
	h := &Handler{cfg: cfg, configFilePath: writeTestConfigFile(t)}
	for _, test := range []struct {
		name    string
		patch   func(*Handler, *gin.Context)
		section string
	}{
		{"gemini", (*Handler).PatchGeminiKey, "gemini-api-key"},
		{"interactions", (*Handler).PatchInteractionsKey, "interactions-api-key"},
		{"claude", (*Handler).PatchClaudeKey, "claude-api-key"},
		{"codex", (*Handler).PatchCodexKey, "codex-api-key"},
		{"xai", (*Handler).PatchXAIKey, "xai-api-key"},
		{"meta", (*Handler).PatchMetaKey, "meta-api-key"},
		{"vertex", (*Handler).PatchVertexCompatKey, "vertex-api-key"},
		{"openai", (*Handler).PatchOpenAICompat, "openai-compatibility"},
	} {
		t.Run(test.name, func(t *testing.T) {
			patch := func(value string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(rec)
				ctx.Request = httptest.NewRequest(http.MethodPatch, "/key", strings.NewReader(`{"index":0,"value":`+value+`}`))
				ctx.Request.Header.Set("Content-Type", "application/json")
				test.patch(h, ctx)
				return rec
			}
			for _, raw := range []string{`-1`, `1.5`, `"3"`} {
				if rec := patch(`{"concurrency-limit":` + raw + `}`); rec.Code != http.StatusBadRequest {
					t.Fatalf("invalid limit %s: status=%d body=%s", raw, rec.Code, rec.Body.String())
				}
			}
			if rec := patch(`{"concurrency-limit":2,"rpm-limit":30}`); rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			readLimits := func() config.RequestLimits {
				t.Helper()
				raw, errMarshal := json.Marshal(cfg)
				if errMarshal != nil {
					t.Fatal(errMarshal)
				}
				var sections map[string]json.RawMessage
				if errDecode := json.Unmarshal(raw, &sections); errDecode != nil {
					t.Fatal(errDecode)
				}
				var records []config.RequestLimits
				if errDecode := json.Unmarshal(sections[test.section], &records); errDecode != nil {
					t.Fatal(errDecode)
				}
				return records[0]
			}
			if got := readLimits(); got != (config.RequestLimits{ConcurrencyLimit: 2, RPMLimit: 30}) {
				t.Fatalf("saved limits = %+v", got)
			}
			if rec := patch(`{"rpm-limit":0}`); rec.Code != http.StatusOK {
				t.Fatalf("reset status=%d body=%s", rec.Code, rec.Body.String())
			}
			if got := readLimits(); got != (config.RequestLimits{ConcurrencyLimit: 2}) {
				t.Fatalf("reset changed omitted limit: %+v", got)
			}
		})
	}
}

func TestOpenAIRequestLimitsReturnedByManagementAPI(t *testing.T) {
	limits := config.RequestLimits{ConcurrencyLimit: 2, RPMLimit: 15}
	h := &Handler{cfg: &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{Name: "limited", BaseURL: "https://example.com", RequestLimits: limits}}}}
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/openai-compatibility", nil)
	h.GetOpenAICompat(ctx)
	var result struct {
		Providers []config.OpenAICompatibility `json:"openai-compatibility"`
	}
	if errDecode := json.Unmarshal(rec.Body.Bytes(), &result); errDecode != nil {
		t.Fatal(errDecode)
	}
	if len(result.Providers) != 1 || result.Providers[0].RequestLimits != limits {
		t.Fatalf("management response lost limits: %s", rec.Body.String())
	}
}

func TestAuthFileRequestLimitsPatchAndBatch(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprintf("batch=%t", batch), func(t *testing.T) {
			manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
			auth := &coreauth.Auth{ID: "limits.json", FileName: "limits.json", Provider: "qwen", Metadata: map[string]any{"type": "qwen"}}
			if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
				t.Fatal(errRegister)
			}
			h := NewHandlerWithoutConfigFilePath(&config.Config{}, manager)
			patch := func(fields string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(rec)
				body := `{"name":"limits.json",` + fields + `}`
				if batch {
					body = `{"items":[{"name":"limits.json"}],"fields":{` + fields + `}}`
				}
				ctx.Request = httptest.NewRequest(http.MethodPatch, "/auth-files/fields", strings.NewReader(body))
				ctx.Request.Header.Set("Content-Type", "application/json")
				if batch {
					h.PatchAuthFileFieldsBatch(ctx)
				} else {
					h.PatchAuthFileFields(ctx)
				}
				return rec
			}
			for _, fields := range []string{`"rpm_limit":-1`, `"rpm_limit":1.5`, `"concurrency_limit":"2"`, `"rpm_limit.nested":1`} {
				if rec := patch(fields); rec.Code != http.StatusBadRequest {
					t.Fatalf("invalid %s status=%d body=%s", fields, rec.Code, rec.Body.String())
				}
			}
			if rec := patch(`"concurrency-limit":2,"rpm_limit":15`); rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			updated, _ := manager.GetByID(auth.ID)
			if updated.RequestLimits() != (config.RequestLimits{ConcurrencyLimit: 2, RPMLimit: 15}) {
				t.Fatalf("limits = %+v", updated.RequestLimits())
			}
			if rec := patch(`"concurrency_limit":0,"rpm_limit":0`); rec.Code != http.StatusOK {
				t.Fatalf("reset status=%d", rec.Code)
			}
			updated, _ = manager.GetByID(auth.ID)
			if updated.RequestLimits() != (config.RequestLimits{}) {
				t.Fatal("zero did not reset limits")
			}
		})
	}
}
