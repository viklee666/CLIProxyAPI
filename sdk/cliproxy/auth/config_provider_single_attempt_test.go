package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/MoeForge/moego/json"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executionregistry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestConfigProviderSingleAttemptAcrossExecutionPaths(t *testing.T) {
	const model = "config-single-attempt-model"
	for _, homeMode := range []bool{false, true} {
		for _, path := range []string{"execute", "count", "stream", "stream-bootstrap"} {
			t.Run(fmt.Sprintf("home=%t/%s", homeMode, path), func(t *testing.T) {
				failure := &Error{HTTPStatus: http.StatusServiceUnavailable, Message: "original upstream failure"}
				executor := &retryContractHomeExecutor{
					failAll: true, failure: failure, streamBootstrap: path == "stream-bootstrap",
					streamHeaders: http.Header{"X-Upstream-Attempt": {"1"}},
				}
				manager := NewManager(nil, &RoundRobinSelector{}, nil)
				manager.RegisterExecutor(executor)
				manager.SetConfig(&internalconfig.Config{Home: internalconfig.HomeConfig{Enabled: homeMode}})
				manager.SetRetryConfig(8, 0, 0)
				for _, id := range []string{"home-retry-a", "home-retry-b", "home-retry-c"} {
					auth := &Auth{
						ID: id + "-" + t.Name(), Provider: executor.Identifier(), Status: StatusActive,
						Attributes: map[string]string{AttributeSource: "config:test[" + id + "]", AttributeAuthKind: AuthKindAPIKey},
						Metadata:   map[string]any{"disable_cooling": true, "request_retry": 12},
					}
					if homeMode {
						response, errMarshal := json.Marshal(homeAuthDispatchResponse{Auth: *auth})
						if errMarshal != nil {
							t.Fatal(errMarshal)
						}
						manager.PublishHomeDispatch(&accessIsolationHomeDispatcher{response: response}, executionregistry.New(), 1)
						break
					}
					if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
						t.Fatal(errRegister)
					}
					registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: model}})
					t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
				}
				for request := 1; request <= 10; request++ {
					req := cliproxyexecutor.Request{Model: model}
					var errExecute error
					switch path {
					case "execute":
						_, errExecute = manager.Execute(context.Background(), []string{executor.Identifier()}, req, cliproxyexecutor.Options{})
					case "count":
						_, errExecute = manager.ExecuteCount(context.Background(), []string{executor.Identifier()}, req, cliproxyexecutor.Options{})
					default:
						var result *cliproxyexecutor.StreamResult
						result, errExecute = manager.ExecuteStream(context.Background(), []string{executor.Identifier()}, req, cliproxyexecutor.Options{Stream: true})
						if errExecute == nil && result != nil {
							for chunk := range result.Chunks {
								if chunk.Err != nil {
									errExecute = chunk.Err
								}
							}
							if path == "stream-bootstrap" && result.Headers.Get("X-Upstream-Attempt") != "1" {
								t.Fatalf("lost headers from original stream: %v", result.Headers)
							}
						}
					}
					if !errors.Is(errExecute, failure) {
						t.Fatalf("error=%v, want original upstream failure", errExecute)
					}
					if calls := executor.Calls(); len(calls) != request {
						t.Fatalf("%d downstream requests produced %d executions: %v", request, len(calls), calls)
					}
				}
			})
		}
	}
}
