package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestV8RequestLimitsValidation(t *testing.T) {
	for _, family := range []string{"gemini", "interactions", "claude", "codex", "xai", "meta", "vertex", "openai-compatibility"} {
		for _, field := range []string{"concurrency-limit", "rpm-limit"} {
			for _, invalid := range []string{"-1", "1.5", `"2"`, "true"} {
				t.Run(family+"/"+field+"/"+invalid, func(t *testing.T) {
					data := []byte(fmt.Sprintf("api-keys:\n  %s:\n    - name: provider\n      base-url: https://example.com\n      %s: %s\n      keys:\n        - api-key: test\n", family, field, invalid))
					if _, err := ParseConfigBytes(data); err == nil {
						t.Fatal("accepted invalid grouped request limit")
					}
				})
			}
		}
	}
}

func TestV8ForkConfigurationSurvivesMigrationAndSave(t *testing.T) {
	data := []byte(`client-access:
  enabled: true
  database-path: data/clients.sqlite
  token-reservation: 42
routing:
  strategy: adaptive
  adaptive:
    top-k: 4
streaming:
  first-event-timeout-seconds: 20
  first-event-timeout-retries: 2
  send-keepalive-before-ft: true
openai-compatibility:
  - name: nvidia
    base-url: https://example.com/v1
    concurrency-limit: 3
    rpm-limit: 17
    nim-compat: true
    request-scoped-errors:
      - status: 400
        match: [custom-error]
        action: stop
`)
	migrated, _, err := NormalizeConfigLayout(data, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateV8Config(migrated); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err = os.WriteFile(path, migrated, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = SaveConfigPreserveComments(path, cfg); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ClientAccess.Enabled || cfg.ClientAccess.DatabasePath != "data/clients.sqlite" || cfg.ClientAccess.TokenReservation != 42 {
		t.Fatalf("client access settings lost: %+v", cfg.ClientAccess)
	}
	if cfg.Routing.Strategy != "adaptive" || cfg.Routing.Adaptive.TopK != 4 {
		t.Fatalf("adaptive routing settings lost: %+v", cfg.Routing)
	}
	if cfg.Streaming.FirstEventTimeoutSeconds != 20 || cfg.Streaming.FirstEventTimeoutRetries != 2 || !cfg.Streaming.SendKeepAliveBeforeFT {
		t.Fatalf("streaming settings lost: %+v", cfg.Streaming)
	}
	if len(cfg.OpenAICompatibility) != 1 {
		t.Fatalf("provider lost: %+v", cfg.OpenAICompatibility)
	}
	entry := cfg.OpenAICompatibility[0]
	if !entry.NIMCompat || entry.ConcurrencyLimit != 3 || entry.RPMLimit != 17 || len(entry.RequestScopedErrors) != 1 || entry.RequestScopedErrors[0].Status != 400 {
		t.Fatalf("provider customizations lost: %+v", entry)
	}
}
