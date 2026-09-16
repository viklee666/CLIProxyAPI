package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestOpenAICompatNIMStreamBuffersUpstreamAndEmitsEagerStart(t *testing.T) {
	received := make(chan []byte, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); !strings.Contains(got, "application/json") {
			t.Errorf("Accept = %q, want application/json", got)
		}
		body, _ := io.ReadAll(r.Body)
		received <- body
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","model":"z-ai/glm5","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"Read","arguments":"{}"}}]}}]}`))
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{
		Name:      "nvidia-nim",
		BaseURL:   server.URL,
		NIMCompat: true,
		Models:    []config.OpenAICompatibilityModel{{Name: "z-ai/glm5", Alias: "glm5"}},
	}}}
	executor := NewOpenAICompatExecutor("openai-compatible-nvidia-nim", cfg)
	auth := &cliproxyauth.Auth{
		Provider: "openai-compatible-nvidia-nim",
		Attributes: map[string]string{
			"base_url":    server.URL,
			"api_key":     "nvapi-test",
			"compat_name": "nvidia-nim",
		},
	}
	payload := []byte(`{"model":"z-ai/glm5","max_tokens":128,"stream":true,"thinking":{"type":"enabled","budget_tokens":1024},"tools":[{"name":"Read","description":"read a file","input_schema":{"type":"object","properties":{}}}],"messages":[{"role":"user","content":"hi"}]}`)
	result, errStream := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "z-ai/glm5",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FormatClaude,
		ResponseFormat:  sdktranslator.FormatClaude,
		Stream:          true,
		OriginalRequest: payload,
	})
	if errStream != nil {
		t.Fatalf("ExecuteStream error: %v", errStream)
	}

	first := <-result.Chunks
	if first.Err != nil {
		t.Fatalf("first chunk error: %v", first.Err)
	}
	if !bytes.Contains(first.Payload, []byte(`"message_start"`)) {
		t.Fatalf("first chunk missing message_start: %s", first.Payload)
	}

	upstream := <-received
	if gjson.GetBytes(upstream, "stream").Bool() {
		t.Fatalf("upstream stream = true, want false; body=%s", upstream)
	}
	if gjson.GetBytes(upstream, "stream_options").Exists() {
		t.Fatalf("upstream still has stream_options: %s", upstream)
	}
	if gjson.GetBytes(upstream, "reasoning_effort").Exists() {
		t.Fatalf("upstream still has reasoning_effort: %s", upstream)
	}
	if !gjson.GetBytes(upstream, "chat_template_kwargs.enable_thinking").Bool() {
		t.Fatalf("enable_thinking missing: %s", upstream)
	}
	if gjson.GetBytes(upstream, "nvext.max_thinking_tokens").Int() != 1024 {
		t.Fatalf("max_thinking_tokens = %s", upstream)
	}

	close(release)

	var rest bytes.Buffer
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("later chunk error: %v", chunk.Err)
		}
		rest.Write(chunk.Payload)
	}
	if !bytes.Contains(rest.Bytes(), []byte(`"tool_use"`)) {
		t.Fatalf("translated stream missing tool_use: %s", rest.Bytes())
	}
	if !bytes.Contains(rest.Bytes(), []byte(`"message_stop"`)) {
		t.Fatalf("translated stream missing message_stop: %s", rest.Bytes())
	}
}

func TestOpenAICompatExecutorIgnoresNIMCompatWhenDisabled(t *testing.T) {
	var upstream []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{
		Name:    "openrouter",
		BaseURL: server.URL,
		Models:  []config.OpenAICompatibilityModel{{Name: "kimi", Alias: "kimi"}},
	}}}
	executor := NewOpenAICompatExecutor("openai-compatible-openrouter", cfg)
	auth := &cliproxyauth.Auth{
		Provider: "openai-compatible-openrouter",
		Attributes: map[string]string{
			"base_url":    server.URL,
			"api_key":     "sk-test",
			"compat_name": "openrouter",
		},
	}
	payload := []byte(`{"model":"kimi","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	result, errStream := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "kimi",
		Payload: payload,
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FormatClaude,
		ResponseFormat:  sdktranslator.FormatClaude,
		Stream:          true,
		OriginalRequest: payload,
	})
	if errStream != nil {
		t.Fatalf("ExecuteStream error: %v", errStream)
	}
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("chunk error: %v", chunk.Err)
		}
	}
	if !gjson.GetBytes(upstream, "stream").Bool() {
		t.Fatalf("non-NIM provider should still stream; body=%s", upstream)
	}
	if !gjson.GetBytes(upstream, "stream_options.include_usage").Bool() {
		t.Fatalf("non-NIM provider should request include_usage; body=%s", upstream)
	}
}
