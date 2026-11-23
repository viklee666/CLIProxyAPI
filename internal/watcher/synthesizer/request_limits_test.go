package synthesizer

import (
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestConfigRequestLimitsEveryProvider(t *testing.T) {
	limits := config.RequestLimits{ConcurrencyLimit: 2, RPMLimit: 15}
	cfg := &config.Config{
		GeminiKey:           []config.GeminiKey{{APIKey: "gemini", RequestLimits: limits}},
		InteractionsKey:     []config.GeminiKey{{APIKey: "interactions", RequestLimits: limits}},
		ClaudeKey:           []config.ClaudeKey{{APIKey: "claude", RequestLimits: limits}},
		CodexKey:            []config.CodexKey{{APIKey: "codex", BaseURL: "https://example.com", RequestLimits: limits}},
		XAIKey:              []config.XAIKey{{APIKey: "xai", RequestLimits: limits}},
		MetaKey:             []config.MetaKey{{APIKey: "meta", RequestLimits: limits}},
		VertexCompatAPIKey:  []config.VertexCompatKey{{APIKey: "vertex", RequestLimits: limits}},
		OpenAICompatibility: []config.OpenAICompatibility{{Name: "custom", BaseURL: "https://example.com", RequestLimits: limits, APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "first"}, {APIKey: "second"}}}},
	}
	auths, errSynthesize := NewConfigSynthesizer().Synthesize(&SynthesisContext{Config: cfg, Now: time.Now(), IDGenerator: NewStableIDGenerator()})
	if errSynthesize != nil {
		t.Fatal(errSynthesize)
	}
	if len(auths) != 9 {
		t.Fatalf("auth count = %d, want 9", len(auths))
	}
	for _, auth := range auths {
		if got := auth.RequestLimits(); got != limits {
			t.Errorf("%s limits = %+v, want %+v", auth.Provider, got, limits)
		}
	}
	firstGroup := auths[6].Attributes["request_limit_group"]
	secondGroup := auths[7].Attributes["request_limit_group"]
	if firstGroup == "" || firstGroup != secondGroup {
		t.Fatal("OpenAI keys do not share provider admission group")
	}
}
