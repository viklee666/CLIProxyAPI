package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestOpenAICompatNIMPayloadRulesRemainFinal(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		name := "nonstream"
		if streaming {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			received := make(chan []byte, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, errRead := io.ReadAll(r.Body)
				if errRead != nil {
					t.Error(errRead)
				}
				received <- body
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"nim-result","object":"chat.completion","model":"nim-model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
			}))
			defer server.Close()
			cfg := &config.Config{
				OpenAICompatibility: []config.OpenAICompatibility{{Name: "nim", BaseURL: server.URL, NIMCompat: true}},
				Payload: config.PayloadConfig{
					Override: []config.PayloadRule{{
						Models: []config.PayloadModelRule{{Name: "*"}},
						Params: map[string]any{
							"chat_template_kwargs.enable_thinking": false,
							"stream_options.include_usage":         true,
							"audit.-1":                             "once",
						},
					}},
					Filter: []config.PayloadFilterRule{{Models: []config.PayloadModelRule{{Name: "*"}}, Params: []string{"nvext.max_thinking_tokens", "stream"}}},
				},
			}
			executor := NewOpenAICompatExecutor("openai-compatible-nim", cfg)
			auth := &cliproxyauth.Auth{Provider: "openai-compatible-nim", Attributes: map[string]string{"base_url": server.URL, "api_key": "test", "compat_name": "nim"}}
			payload := []byte(`{"model":"nim-model","max_tokens":64,"stream":true,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":"hi"}]}`)
			req := cliproxyexecutor.Request{Model: "nim-model", Payload: payload}
			opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatClaude, ResponseFormat: sdktranslator.FormatClaude, Stream: streaming, OriginalRequest: payload}
			if streaming {
				result, errStream := executor.ExecuteStream(context.Background(), auth, req, opts)
				if errStream != nil {
					t.Fatal(errStream)
				}
				for chunk := range result.Chunks {
					if chunk.Err != nil {
						t.Fatal(chunk.Err)
					}
				}
			} else if _, errExecute := executor.Execute(context.Background(), auth, req, opts); errExecute != nil {
				t.Fatal(errExecute)
			}
			body := <-received
			if ojson.GetBytesNoCopy(body, "chat_template_kwargs.enable_thinking").Bool() || !ojson.GetBytesNoCopy(body, "stream_options.include_usage").Bool() {
				t.Fatalf("NIM normalization overwrote configured values: %s", body)
			}
			if ojson.GetBytesNoCopy(body, "nvext.max_thinking_tokens").Exists() || ojson.GetBytesNoCopy(body, "stream").Exists() {
				t.Fatalf("NIM normalization restored filtered fields: %s", body)
			}
			if got := ojson.GetBytesNoCopy(body, "audit").Raw; got != `["once"]` {
				t.Fatalf("payload rules must run exactly once: audit=%s", got)
			}
		})
	}
}
