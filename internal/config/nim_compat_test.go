package config

import "testing"

func TestOpenAICompatibilityNIMCompatConfigDecoding(t *testing.T) {
	cfg, errParse := ParseConfigBytes([]byte(`
openai-compatibility:
  - name: nvidia-nim
    base-url: https://integrate.api.nvidia.com/v1
    nim-compat: true
    api-key-entries:
      - api-key: nvapi-test
    models:
      - name: z-ai/glm5
        alias: glm5
  - name: openrouter
    base-url: https://openrouter.ai/api/v1
    models:
      - name: kimi
`))
	if errParse != nil {
		t.Fatalf("ParseConfigBytes() error = %v", errParse)
	}
	if len(cfg.OpenAICompatibility) != 2 {
		t.Fatalf("len(OpenAICompatibility) = %d, want 2", len(cfg.OpenAICompatibility))
	}
	if !cfg.OpenAICompatibility[0].NIMCompat {
		t.Fatal("nvidia-nim NIMCompat = false, want true")
	}
	if cfg.OpenAICompatibility[1].NIMCompat {
		t.Fatal("openrouter NIMCompat = true, want default false")
	}
}
