package config

import (
	"encoding/json"
	"testing"
)

func TestProviderRemarkNamesRoundTrip(t *testing.T) {
	cfg, err := ParseConfigBytes([]byte(`gemini-api-key:
  - name: "  Gemini primary  "
    api-key: gemini-key
interactions-api-key:
  - name: "  Interactions primary  "
    api-key: interactions-key
codex-api-key:
  - name: "  Codex primary  "
    api-key: codex-key
    base-url: https://codex.example/v1
claude-api-key:
  - name: "  Claude primary  "
    api-key: claude-key
vertex-api-key:
  - name: "  Vertex primary  "
    api-key: vertex-key
    base-url: https://vertex.example/v1
`))
	if err != nil {
		t.Fatalf("ParseConfigBytes() error = %v", err)
	}

	got := []string{
		cfg.GeminiKey[0].Name,
		cfg.InteractionsKey[0].Name,
		cfg.CodexKey[0].Name,
		cfg.ClaudeKey[0].Name,
		cfg.VertexCompatAPIKey[0].Name,
	}
	want := []string{
		"Gemini primary",
		"Interactions primary",
		"Codex primary",
		"Claude primary",
		"Vertex primary",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("name[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded Config
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if decoded.GeminiKey[0].Name != "Gemini primary" || decoded.VertexCompatAPIKey[0].Name != "Vertex primary" {
		t.Fatalf("remark names did not survive JSON round trip: %+v", decoded)
	}
}
