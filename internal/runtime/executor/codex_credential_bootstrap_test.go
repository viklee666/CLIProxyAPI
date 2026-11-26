package executor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

const (
	codexCredentialRateLimitEvent = `{"type":"error","status":429,"error":{"type":"rate_limit_error","code":"INVALID_AUTH_RATE_LIMITED","message":"Credential is rate limited","headers":{"retry-after":"23"}}}`
	codexCredentialOverloadEvent  = `{"type":"error","status":502,"error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"Our servers are currently overloaded.","headers":{"retry-after":"326","retry-after-ms":"326000"}}}`
)

func codexCredentialBootstrapAuth(baseURL string) *cliproxyauth.Auth {
	return &cliproxyauth.Auth{
		ID: "credential-bootstrap", Provider: "codex", FileName: "credential-bootstrap.json",
		Status:     cliproxyauth.StatusActive,
		Attributes: map[string]string{"base_url": baseURL, "websockets": "true"},
		Metadata:   map[string]any{"access_token": "test"},
	}
}

func codexCredentialBootstrapExecutor(t *testing.T, transport string, cfg *config.Config) cliproxyauth.ProviderExecutor {
	t.Helper()
	if transport == "http" {
		return NewCodexExecutor(cfg)
	}
	exec := NewCodexWebsocketsExecutor(cfg)
	exec.store = &codexWebsocketSessionStore{sessions: make(map[string]*codexWebsocketSession)}
	return exec
}

func codexCredentialBootstrapContext(t *testing.T, transport string) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	if transport == "duplex" {
		ctx = cliproxyexecutor.WithWebsocketInput(cliproxyexecutor.WithDownstreamWebsocket(ctx), make(chan cliproxyexecutor.WebsocketInput))
	}
	return ctx, cancel
}

func TestCodexCredentialBootstrapSourcePolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		auth *cliproxyauth.Auth
		want bool
	}{
		{"nil", nil, false},
		{"unspecified", &cliproxyauth.Auth{}, false},
		{"filename", &cliproxyauth.Auth{FileName: "credential.json"}, true},
		{"path", &cliproxyauth.Auth{Attributes: map[string]string{"path": "auths/credential.json"}}, true},
		{"legacy_source", &cliproxyauth.Auth{Attributes: map[string]string{"source": "credential.json"}}, true},
		{"file", &cliproxyauth.Auth{Attributes: map[string]string{"source_backend": "file"}}, true},
		{"git", &cliproxyauth.Auth{Attributes: map[string]string{"source_backend": "git"}}, true},
		{"objectstore", &cliproxyauth.Auth{Attributes: map[string]string{"source_backend": "objectstore"}}, true},
		{"postgres", &cliproxyauth.Auth{Attributes: map[string]string{"source_backend": "postgres"}}, true},
		{"config", &cliproxyauth.Auth{FileName: "synthetic.json", Attributes: map[string]string{"source": "config:codex:0"}}, false},
		{"memory", &cliproxyauth.Auth{FileName: "synthetic.json", Attributes: map[string]string{"source_backend": "memory"}}, false},
		{"runtime_only", &cliproxyauth.Auth{FileName: "credential.json", Attributes: map[string]string{"source_backend": "file", "runtime_only": "true"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, cfg := range []*config.Config{nil, codexBufferingConfig(false), codexBufferingConfig(true)} {
				enabled, credentialFile := NewCodexExecutor(cfg).streamBootstrapPolicy(tc.auth)
				wantEnabled := tc.want || cfg != nil && cfg.Codex.StreamBootstrapBuffering
				if credentialFile != tc.want || enabled != wantEnabled {
					t.Fatalf("policy = (%t, %t), want (%t, %t)", enabled, credentialFile, wantEnabled, tc.want)
				}
			}
		})
	}
}

func TestCodexCredentialBootstrapInitialFailure(t *testing.T) {
	for _, transport := range []string{"http", "websocket", "duplex"} {
		for _, tc := range []struct {
			name, event string
			status      int
			delay       time.Duration
		}{
			{"rate_limit", codexCredentialRateLimitEvent, 429, 23 * time.Second},
			{"rate_limit_without_hint", `{"type":"error","status":429,"error":{"code":"INVALID_AUTH_RATE_LIMITED","message":"Credential is rate limited"}}`, 429, 0},
			{"failed_rate_limit", `{"type":"response.failed","response":{"error":{"status":429,"code":"INVALID_AUTH_RATE_LIMITED","message":"Credential is rate limited","headers":{"retry-after":"23"}}}}`, 429, 23 * time.Second},
			{"overload", codexCredentialOverloadEvent, 502, 326 * time.Second},
			{"failed_overload", `{"type":"response.failed","response":{"error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"Overloaded","headers":{"retry-after":"326"}}}}`, 502, 326 * time.Second},
			{"quota", `{"type":"response.failed","response":{"error":{"type":"usage_limit_reached","message":"Quota exhausted","resets_in_seconds":23}}}`, 429, 23 * time.Second},
		} {
			t.Run(transport+"/"+tc.name, func(t *testing.T) {
				frames := []string{codexCreatedEvent, codexInProgressEvent, codexModerationMetadataEvent, tc.event}
				var server *httptest.Server
				if transport == "http" {
					server = codexSSEServer(frames...)
				} else {
					server = codexWebsocketServerHoldingConnection(t, frames...)
				}
				defer server.Close()
				// Nil config must still protect credential files on the ordinary transports.
				var cfg *config.Config
				if transport == "duplex" {
					cfg = &config.Config{Codex: config.CodexConfig{ResponseSteering: true}}
				}
				ctx, cancel := codexCredentialBootstrapContext(t, transport)
				defer cancel()
				var input chan cliproxyexecutor.WebsocketInput
				if transport == "duplex" {
					input = make(chan cliproxyexecutor.WebsocketInput, 1)
					input <- cliproxyexecutor.WebsocketInput{Payload: []byte(`{"type":"response.create","input":[]}`)}
					ctx = cliproxyexecutor.WithWebsocketInput(ctx, input)
				}
				req, opts := codexWebsocketRequest()
				result, err := codexCredentialBootstrapExecutor(t, transport, cfg).ExecuteStream(ctx, codexCredentialBootstrapAuth(server.URL), req, opts)
				if transport == "duplex" {
					if err != nil || result == nil {
						t.Fatalf("duplex start = %v, %v", result, err)
					}
					payload, streamErr := drainChunks(result)
					if payload != "" {
						t.Fatalf("rejected handshake escaped: %s", payload)
					}
					err = streamErr
					if len(input) != 1 {
						t.Fatal("rejected bootstrap consumed a follow-up request")
					}
				} else if result != nil {
					drainChunks(result)
					t.Fatal("rejected handshake returned a stream")
				}
				if err == nil || statusCodeFromTestError(t, err) != tc.status {
					t.Fatalf("bootstrap error = %v, want status %d", err, tc.status)
				}
				if tc.delay > 0 {
					assertIssue7600RetryAfter(t, err, tc.delay)
				}
			})
		}
	}
}

func TestCodexCredentialBootstrapConfigProvidersUnchanged(t *testing.T) {
	for _, transport := range []string{"http", "websocket"} {
		for _, source := range []string{"config", "memory"} {
			for _, enabled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/buffering=%t", transport, source, enabled), func(t *testing.T) {
					var server *httptest.Server
					if transport == "http" {
						server = codexSSEServer(codexCreatedEvent, codexOverloadEvent)
					} else {
						server = codexWebsocketServerHoldingConnection(t, codexCreatedEvent, codexOverloadEvent)
					}
					defer server.Close()
					auth := codexCredentialBootstrapAuth(server.URL)
					auth.Attributes[cliproxyauth.AttributeSourceBackend] = source
					ctx, cancel := codexCredentialBootstrapContext(t, transport)
					defer cancel()
					req, opts := codexWebsocketRequest()
					result, err := codexCredentialBootstrapExecutor(t, transport, codexBufferingConfig(enabled)).ExecuteStream(ctx, auth, req, opts)
					if enabled {
						if result != nil || err == nil || statusCodeFromTestError(t, err) != 503 {
							t.Fatalf("explicit buffering changed: result=%v err=%v", result, err)
						}
						return
					}
					if err != nil || result == nil {
						t.Fatalf("config-provider passthrough changed: result=%v err=%v", result, err)
					}
					payload, streamErr := drainChunks(result)
					if !strings.Contains(payload, "response.created") || streamErr == nil || statusCodeFromTestError(t, streamErr) != 502 {
						t.Fatalf("config-provider stream changed: payload=%s err=%v", payload, streamErr)
					}
				})
			}
		}
	}
}

func TestCodexCredentialBootstrapManagerFailoverAndCommit(t *testing.T) {
	for _, transport := range []string{"http", "websocket", "duplex"} {
		for _, tc := range []struct {
			name, before, failure string
			chat                  bool
		}{
			{"initial_rate_limit", "", codexCredentialRateLimitEvent, false},
			{"initial_overload", "", codexCredentialOverloadEvent, false},
			{"after_content", codexOutputDeltaEvent, codexCredentialRateLimitEvent, false},
			{"after_tool", codexReleasingFrameCases()[1].frame, codexCredentialOverloadEvent, false},
			{"untranslated_tool", codexReleasingFrameCases()[1].frame, codexCredentialOverloadEvent, true},
		} {
			if transport == "duplex" && tc.chat {
				continue
			}
			t.Run(transport+"/"+tc.name, func(t *testing.T) {
				var rejected, healthy atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					frames := []string{codexCreatedEvent, codexInProgressEvent}
					if r.Header.Get("Authorization") == "Bearer bad" {
						rejected.Add(1)
						if tc.before != "" {
							frames = append(frames, tc.before)
						}
						frames = append(frames, tc.failure)
					} else {
						healthy.Add(1)
						frames = []string{strings.ReplaceAll(codexCreatedEvent, "resp_1", "healthy-response"), codexOutputAddedEvent, codexOutputDeltaEvent, strings.ReplaceAll(codexCompletedEventBody, "resp_1", "healthy-response")}
					}
					if transport == "http" {
						w.Header().Set("Content-Type", "text/event-stream")
						for _, frame := range frames {
							_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
						}
						return
					}
					conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
					if err != nil {
						t.Error(err)
						return
					}
					defer func() { _ = conn.Close() }()
					if _, _, err = conn.ReadMessage(); err != nil {
						return
					}
					for _, frame := range frames {
						if err = conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
							return
						}
					}
					_, _, _ = conn.ReadMessage()
				}))
				defer server.Close()
				cfg := &config.Config{Codex: config.CodexConfig{ResponseSteering: transport == "duplex"}}
				ctx, cancel := codexCredentialBootstrapContext(t, transport)
				defer cancel()
				manager := cliproxyauth.NewManager(nil, &cliproxyauth.FillFirstSelector{}, nil)
				manager.SetConfig(cfg)
				manager.SetRetryConfig(0, 0, 0)
				manager.RegisterExecutor(codexCredentialBootstrapExecutor(t, transport, cfg))
				req, opts := codexWebsocketRequest()
				opts.Metadata = map[string]any{cliproxyexecutor.ExecutionSessionMetadataKey: t.Name()}
				if tc.chat {
					opts.ResponseFormat = sdktranslator.FromString("openai")
				}
				for i, key := range []string{"bad", "good"} {
					auth := codexCredentialBootstrapAuth(server.URL)
					auth.ID = t.Name() + "/" + key
					auth.FileName = key + ".json"
					auth.Metadata["access_token"] = key
					auth.Attributes["priority"] = fmt.Sprint(2 - i)
					registry.GetGlobalRegistry().RegisterClient(auth.ID, "codex", []*registry.ModelInfo{{ID: req.Model}})
					t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
					if _, err := manager.Register(cliproxyauth.WithSkipPersist(ctx), auth); err != nil {
						t.Fatal(err)
					}
				}
				result, err := manager.ExecuteStream(ctx, []string{"codex"}, req, opts)
				if err != nil || result == nil {
					t.Fatalf("ExecuteStream: result=%v err=%v", result, err)
				}
				var text strings.Builder
				var streamErr error
				wireFailure := false
				for chunk := range result.Chunks {
					text.Write(chunk.Payload)
					if chunk.Err != nil {
						streamErr = chunk.Err
					}
					if transport == "duplex" && strings.Contains(string(chunk.Payload), `"type":"error"`) {
						wireFailure = true
						cancel()
					}
					if transport == "duplex" && strings.Contains(string(chunk.Payload), `"type":"response.completed"`) {
						cancel()
					}
				}
				if rejected.Load() != 1 {
					t.Fatalf("rejected credential executed %d times", rejected.Load())
				}
				if tc.before == "" {
					if healthy.Load() != 1 || streamErr != nil || !strings.Contains(text.String(), "healthy-response") || strings.Contains(text.String(), "resp_1") {
						t.Fatalf("failover: healthy=%d err=%v payload=%s", healthy.Load(), streamErr, text.String())
					}
				} else if healthy.Load() != 0 || streamErr == nil && !wireFailure {
					t.Fatalf("semantic output replayed: healthy=%d err=%v payload=%s", healthy.Load(), streamErr, text.String())
				}
			})
		}
	}
}

func TestCodexCredentialBootstrapCancellation(t *testing.T) {
	for _, transport := range []string{"http", "websocket", "duplex"} {
		t.Run(transport, func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if transport == "http" {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "data: %s\n\n", codexCreatedEvent)
					w.(http.Flusher).Flush()
					close(started)
					<-r.Context().Done()
					return
				}
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = conn.Close() }()
				if _, _, err = conn.ReadMessage(); err != nil {
					return
				}
				_ = conn.WriteMessage(websocket.TextMessage, []byte(codexCreatedEvent))
				close(started)
				_, _, _ = conn.ReadMessage()
			}))
			defer server.Close()
			ctx, cancel := codexCredentialBootstrapContext(t, transport)
			defer cancel()
			go func() {
				select {
				case <-started:
					cancel()
				case <-ctx.Done():
				}
			}()
			cfg := &config.Config{Codex: config.CodexConfig{ResponseSteering: transport == "duplex"}}
			req, opts := codexWebsocketRequest()
			result, err := codexCredentialBootstrapExecutor(t, transport, cfg).ExecuteStream(ctx, codexCredentialBootstrapAuth(server.URL), req, opts)
			if result != nil {
				payload, streamErr := drainChunks(result)
				if payload != "" || streamErr != nil && !errors.Is(streamErr, context.Canceled) {
					t.Fatalf("cancellation leaked output or upstream failure: payload=%s err=%v", payload, streamErr)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation = %v, want context.Canceled", err)
			}
		})
	}
}

func TestCodexCredentialBootstrapBudgets(t *testing.T) {
	for _, transport := range []string{"http", "websocket", "duplex"} {
		for _, budget := range []string{"frames", "bytes"} {
			t.Run(transport+"/"+budget, func(t *testing.T) {
				frames := []string{codexCreatedEvent}
				if budget == "frames" {
					for range codexBootstrapMaxBufferedFrames + 1 {
						frames = append(frames, codexKeepaliveEvent)
					}
				} else {
					frames = append(frames, `{"type":"keepalive","padding":"`+strings.Repeat("x", codexBootstrapMaxBufferedBytes)+`"}`)
				}
				frames = append(frames, codexCredentialRateLimitEvent)
				var server *httptest.Server
				if transport == "http" {
					server = codexSSEServer(frames...)
				} else {
					server = codexWebsocketServerHoldingConnection(t, frames...)
				}
				defer server.Close()
				cfg := &config.Config{Codex: config.CodexConfig{ResponseSteering: transport == "duplex"}}
				ctx, cancel := codexCredentialBootstrapContext(t, transport)
				defer cancel()
				req, opts := codexWebsocketRequest()
				result, err := codexCredentialBootstrapExecutor(t, transport, cfg).ExecuteStream(ctx, codexCredentialBootstrapAuth(server.URL), req, opts)
				if err != nil || result == nil {
					t.Fatalf("budget must release the stream: result=%v err=%v", result, err)
				}
				created, failed := false, false
				for chunk := range result.Chunks {
					if chunk.Committed {
						t.Fatal("budget exhaustion falsely marked semantic output")
					}
					created = created || strings.Contains(string(chunk.Payload), "response.created")
					failed = failed || chunk.Err != nil
				}
				if !created || !failed {
					t.Fatalf("released stream lost handshake or failure: created=%t failed=%t", created, failed)
				}
			})
		}
	}
}

func TestCodexCredentialBootstrapUntranslatedToolCommits(t *testing.T) {
	for _, transport := range []string{"http", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			frames := []string{codexReleasingFrameCases()[1].frame, codexCredentialOverloadEvent}
			var server *httptest.Server
			if transport == "http" {
				server = codexSSEServer(frames...)
			} else {
				server = codexWebsocketServerHoldingConnection(t, frames...)
			}
			defer server.Close()
			ctx, cancel := codexCredentialBootstrapContext(t, transport)
			defer cancel()
			req, opts := codexWebsocketRequest()
			opts.ResponseFormat = sdktranslator.FromString("openai")
			result, err := codexCredentialBootstrapExecutor(t, transport, nil).ExecuteStream(ctx, codexCredentialBootstrapAuth(server.URL), req, opts)
			if err != nil || result == nil {
				t.Fatalf("semantic tool start returned a retryable failure: %v", err)
			}
			committed, failed := false, false
			for chunk := range result.Chunks {
				if chunk.Err != nil {
					if !committed {
						t.Fatal("tool side effect was not committed before overload")
					}
					failed = true
				}
				if chunk.Committed {
					if len(chunk.Payload) != 0 {
						t.Fatalf("fixture should not render tool output, got %s", chunk.Payload)
					}
					committed = true
				}
			}
			if !committed || !failed {
				t.Fatalf("commit/error lost: committed=%t failed=%t", committed, failed)
			}
		})
	}
}
