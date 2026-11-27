package auth

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type credentialSweepExecutor struct {
	schedulerTestExecutor
	attempts  []string
	fail      func(context.Context, *Auth) error
	bootstrap bool
	committed bool
}

func (e *credentialSweepExecutor) run(ctx context.Context, auth *Auth) error {
	e.attempts = append(e.attempts, auth.ID)
	cliproxyexecutor.MarkUpstreamAttempt(ctx)
	return e.fail(ctx, auth)
}

func (e *credentialSweepExecutor) Execute(ctx context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	if err := e.run(ctx, auth); err != nil {
		return cliproxyexecutor.Response{}, err
	}
	return cliproxyexecutor.Response{Payload: []byte(`{"type":"response.completed","output_text":"served"}`)}, nil
}

func (e *credentialSweepExecutor) CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.Execute(ctx, auth, req, opts)
}

func (e *credentialSweepExecutor) ExecuteStream(ctx context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	err := e.run(ctx, auth)
	if err == nil {
		return successStreamResult(), nil
	}
	if !e.bootstrap && !e.committed {
		return nil, err
	}
	ch := make(chan cliproxyexecutor.StreamChunk, 2)
	if e.committed {
		ch <- cliproxyexecutor.StreamChunk{Committed: true}
	}
	ch <- cliproxyexecutor.StreamChunk{Err: err}
	close(ch)
	return &cliproxyexecutor.StreamResult{Chunks: ch}, nil
}

func newCredentialSweepManager(t *testing.T, selector Selector, count int) (*Manager, *credentialSweepExecutor, []*Auth) {
	t.Helper()
	manager := NewManager(nil, selector, nil)
	// A single-credential configured cap must not truncate the stored-credential sweep.
	manager.SetRetryConfig(0, 0, 1)
	exec := &credentialSweepExecutor{schedulerTestExecutor: schedulerTestExecutor{provider: "codex"}}
	manager.RegisterExecutor(exec)
	auths := make([]*Auth, 0, count)
	for i := range count {
		id := fmt.Sprintf("%s-%d", t.Name(), i)
		auth := &Auth{ID: id, Index: id, FileName: id + ".json", Provider: "codex", Status: StatusActive,
			Attributes: map[string]string{"priority": fmt.Sprint(100 - i), "weight": "1"}}
		registry.GetGlobalRegistry().RegisterClient(id, "codex", []*registry.ModelInfo{{ID: "credential-sweep-model"}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
		registered, err := manager.Register(WithSkipPersist(context.Background()), auth)
		if err != nil {
			t.Fatal(err)
		}
		auths = append(auths, registered)
	}
	return manager, exec, auths
}

func runCredentialSweep(t *testing.T, mode string, manager *Manager, ctx context.Context, opts cliproxyexecutor.Options) error {
	t.Helper()
	req := cliproxyexecutor.Request{Model: "credential-sweep-model"}
	if mode == "execute" || mode == "count" {
		var response cliproxyexecutor.Response
		var err error
		if mode == "count" {
			response, err = manager.ExecuteCount(ctx, []string{"codex"}, req, opts)
		} else {
			response, err = manager.Execute(ctx, []string{"codex"}, req, opts)
		}
		if err == nil && string(response.Payload) != `{"type":"response.completed","output_text":"served"}` {
			t.Fatalf("unexpected successful response: %s", response.Payload)
		}
		return err
	}
	result, err := manager.ExecuteStream(ctx, []string{"codex"}, req, opts)
	if err != nil {
		return err
	}
	completed := false
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			return chunk.Err
		}
		if string(chunk.Payload) == `data: {"type":"response.completed"}` {
			completed = true
		}
	}
	if !completed {
		t.Fatal("successful stream did not complete")
	}
	return nil
}

func credentialSweepError(status int) error {
	delay := 375 * time.Second
	if status == 429 {
		return customStatusError{code: status, msg: `{"code":"INVALID_AUTH_RATE_LIMITED","message":"Too many invalid authentication attempts; retry later"}`, retryAfter: &delay}
	}
	return customStatusError{code: status, msg: `{"error":{"message":"Our servers are currently overloaded. Please try again later.","type":"service_unavailable_error","param":null,"code":"server_is_overloaded","headers":{"retry-after":"375","retry-after-ms":"375000"}},"sequence_number":4}`, retryAfter: &delay}
}

func TestCredentialFailoverSweepsByPriority(t *testing.T) {
	selectors := map[string]func() Selector{
		"adaptive":    func() Selector { return NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{}) },
		"round-robin": func() Selector { return &RoundRobinSelector{} },
		"fill-first":  func() Selector { return &FillFirstSelector{} },
		"weighted":    func() Selector { return &WeightedRoundRobinSelector{} },
		"affinity-never": func() Selector {
			return NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{Fallback: NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{}), RebindOn: SessionAffinityRebindOnNever})
		},
	}
	for name, selector := range selectors {
		for _, mode := range []string{"execute", "count", "stream", "bootstrap"} {
			for _, status := range []int{429, 502} {
				t.Run(fmt.Sprintf("%s/%s/%d", name, mode, status), func(t *testing.T) {
					manager, exec, auths := newCredentialSweepManager(t, selector(), 3)
					exec.bootstrap = mode == "bootstrap"
					exec.fail = func(_ context.Context, a *Auth) error {
						if a.ID != auths[2].ID {
							return credentialSweepError(status)
						}
						return nil
					}
					opts := cliproxyexecutor.Options{Metadata: map[string]any{cliproxyexecutor.CanonicalSessionIDMetadataKey: "sweep-session"}}
					if err := runCredentialSweep(t, mode, manager, context.Background(), opts); err != nil {
						t.Fatal(err)
					}
					want := []string{auths[0].ID, auths[1].ID, auths[2].ID}
					if !reflect.DeepEqual(exec.attempts, want) {
						t.Fatalf("attempts=%v want=%v", exec.attempts, want)
					}
				})
			}
		}
	}
}

func TestCredentialFailoverAuthorizationAndExhaustion(t *testing.T) {
	for _, mode := range []string{"execute", "count", "bootstrap"} {
		for _, admin := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/admin=%t", mode, admin), func(t *testing.T) {
				manager, exec, auths := newCredentialSweepManager(t, NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{}), 6)
				manager.SetRetryConfig(5, 0, 1)
				groups := make(map[string]map[int64]int)
				for i, auth := range auths {
					group := int64(1)
					if i == 1 {
						group = 2
					}
					groups[auth.Index] = map[int64]int{group: 100 - i}
					// Disable cooldown to prove the request-local attempted set bounds the sweep.
					auth.Metadata = map[string]any{"disable_cooling": true}
					if i == 3 {
						auth.Disabled = true
					}
					if i == 4 {
						auth.Attributes[AttributeSource] = "config:codex-api-key"
						auth.FileName = ""
					}
					if _, err := manager.Update(WithSkipPersist(context.Background()), auth); err != nil {
						t.Fatal(err)
					}
				}
				// The last credential supports another model and must never execute.
				registry.GetGlobalRegistry().RegisterClient(auths[5].ID, "codex", []*registry.ModelInfo{{ID: "other-model"}})
				manager.SetCredentialGroupResolver(testCredentialGroupResolver{memberships: groups})
				exec.bootstrap = mode == "bootstrap"
				exec.fail = func(context.Context, *Auth) error { return credentialSweepError(502) }
				opts := clientGroupOptions("1", false)
				opts.Metadata[cliproxyexecutor.ClientAllowAllGroupsMetadataKey] = admin
				err := runCredentialSweep(t, mode, manager, context.Background(), opts)
				if statusCodeFromError(err) != 502 {
					t.Fatalf("terminal error=%v, want original 502", err)
				}
				if got := retryAfterFromError(err); got == nil || *got <= 374*time.Second || *got > 375*time.Second {
					t.Fatalf("lost upstream retry hint: %v", got)
				}
				want := []string{auths[0].ID, auths[2].ID}
				if admin {
					want = []string{auths[0].ID, auths[1].ID, auths[2].ID}
				}
				if !reflect.DeepEqual(exec.attempts, want) {
					t.Fatalf("attempts=%v want=%v", exec.attempts, want)
				}
			})
		}
	}
}

func TestCredentialFailoverTerminalBoundaries(t *testing.T) {
	for _, mode := range []string{"execute", "count", "bootstrap"} {
		for _, boundary := range []string{"pin", "disabled-retries", "request-invalid", "stop-rule", "cancel", "config-provider", "committed"} {
			if boundary == "committed" && mode != "bootstrap" {
				continue
			}
			t.Run(mode+"/"+boundary, func(t *testing.T) {
				manager, exec, auths := newCredentialSweepManager(t, NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{}), 2)
				exec.bootstrap = mode == "bootstrap"
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				opts := cliproxyexecutor.Options{Metadata: make(map[string]any)}
				failure := credentialSweepError(502)
				switch boundary {
				case "pin":
					opts.Metadata[cliproxyexecutor.PinnedAuthMetadataKey] = auths[0].ID
				case "disabled-retries":
					ctx = cliproxyexecutor.WithRequestRetryState(ctx)
					cliproxyexecutor.DisableRequestRetries(ctx)
				case "request-invalid":
					failure = customStatusError{code: http.StatusBadRequest, msg: `{"error":{"type":"invalid_request_error","message":"invalid input"}}`}
				case "config-provider":
					auths[0].FileName = ""
					auths[0].Attributes[AttributeSource] = "config:codex-api-key"
				case "stop-rule":
					auths[0].Metadata = map[string]any{"request_scoped_errors": []internalconfig.RequestScopedErrorRule{{Status: 502, Match: []string{"server_is_overloaded"}, Action: "stop"}}}
				case "committed":
					exec.committed = true
				}
				if _, err := manager.Update(WithSkipPersist(ctx), auths[0]); err != nil {
					t.Fatal(err)
				}
				exec.fail = func(_ context.Context, _ *Auth) error {
					if boundary == "cancel" {
						cancel()
					}
					return failure
				}
				err := runCredentialSweep(t, mode, manager, ctx, opts)
				if err == nil {
					t.Fatal("expected terminal failure")
				}
				if boundary == "committed" && !isRequestStopError(err) {
					t.Fatalf("semantic output allowed outer replay: %v", err)
				}
				if !reflect.DeepEqual(exec.attempts, []string{auths[0].ID}) {
					t.Fatalf("terminal boundary attempted other identities: %v", exec.attempts)
				}
			})
		}
	}
}

func TestCredentialFailoverSkipsPreviouslyCoolingFile(t *testing.T) {
	for _, selector := range []Selector{&RoundRobinSelector{}, NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{})} {
		t.Run(fmt.Sprintf("%T", selector), func(t *testing.T) {
			manager, exec, auths := newCredentialSweepManager(t, selector, 2)
			auths[0].Unavailable = true
			auths[0].ModelStates = map[string]*ModelState{"credential-sweep-model": {Unavailable: true, NextRetryAfter: time.Now().Add(time.Hour)}}
			if _, err := manager.Update(WithSkipPersist(context.Background()), auths[0]); err != nil {
				t.Fatal(err)
			}
			exec.fail = func(context.Context, *Auth) error { return nil }
			if err := runCredentialSweep(t, "execute", manager, context.Background(), cliproxyexecutor.Options{}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(exec.attempts, []string{auths[1].ID}) {
				t.Fatalf("attempts=%v", exec.attempts)
			}
		})
	}
}

func TestCredentialFailoverKeepsAdmissionAndTenantBoundaries(t *testing.T) {
	for _, fast := range []bool{false, true} {
		for _, boundary := range []string{"RPM", "concurrency", "tenant", "zero-weight"} {
			t.Run(fmt.Sprintf("fast=%t/%s", fast, boundary), func(t *testing.T) {
				var selector Selector = NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{})
				if fast {
					selector = &RoundRobinSelector{}
				}
				if boundary == "zero-weight" {
					selector = &WeightedRoundRobinSelector{}
				}
				manager, exec, auths := newCredentialSweepManager(t, selector, 3)
				opts := cliproxyexecutor.Options{Metadata: make(map[string]any)}
				switch boundary {
				case "RPM":
					auths[1].Metadata = map[string]any{"rpm_limit": 1}
					manager.requestLimiter.states = map[string]*requestLimitState{"auth:" + auths[1].ID: {starts: []time.Time{time.Now()}, lastUsed: time.Now()}}
				case "concurrency":
					auths[1].Metadata = map[string]any{"concurrency_limit": 1}
					manager.requestLimiter.states = map[string]*requestLimitState{"auth:" + auths[1].ID: {active: 1}}
				case "tenant":
					manager.SetCredentialOwnershipResolver(testCredentialOwnershipResolver{owners: map[string]int64{auths[0].Index: 1, auths[1].Index: 2, auths[2].Index: 1}})
					opts.Metadata[cliproxyexecutor.ClientTenantIDMetadataKey] = "1"
					opts.Metadata[cliproxyexecutor.ClientAllowAllGroupsMetadataKey] = true
					for i, auth := range auths {
						auth.Attributes[AttributeTenantID] = "1"
						if i == 1 {
							auth.Attributes[AttributeTenantID] = "2"
						}
					}
				case "zero-weight":
					auths[1].Attributes[AttributeWeight] = "0"
				}
				for _, auth := range auths {
					if _, err := manager.Update(WithSkipPersist(context.Background()), auth); err != nil {
						t.Fatal(err)
					}
				}
				exec.fail = func(_ context.Context, auth *Auth) error {
					if auth.ID == auths[2].ID {
						return nil
					}
					return credentialSweepError(429)
				}
				err := runCredentialSweep(t, "execute", manager, context.Background(), opts)
				want := []string{auths[0].ID, auths[2].ID}
				if boundary == "RPM" {
					want = want[:1]
					if err == nil {
						t.Fatal("RPM alone opened a lower tier")
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(exec.attempts, want) {
					t.Fatalf("attempts=%v want=%v", exec.attempts, want)
				}
			})
		}
	}
}

func TestCredentialFailoverSameTierAndStorageBackends(t *testing.T) {
	for _, backend := range []string{AuthSourceFile, AuthSourceGit, AuthSourceObjectStore, AuthSourcePostgres} {
		t.Run(backend, func(t *testing.T) {
			manager, exec, auths := newCredentialSweepManager(t, NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{}), 3)
			for _, auth := range auths {
				auth.Attributes["priority"] = "-5"
				auth.Attributes[AttributeSourceBackend] = backend
				if _, err := manager.Update(WithSkipPersist(context.Background()), auth); err != nil {
					t.Fatal(err)
				}
			}
			exec.fail = func(context.Context, *Auth) error {
				if len(exec.attempts) == 3 {
					return nil
				}
				return credentialSweepError(502)
			}
			if err := runCredentialSweep(t, "execute", manager, context.Background(), cliproxyexecutor.Options{}); err != nil {
				t.Fatal(err)
			}
			seen := make(map[string]bool)
			for _, id := range exec.attempts {
				if seen[id] {
					t.Fatalf("replayed credential: %v", exec.attempts)
				}
				seen[id] = true
			}
			if len(seen) != 3 {
				t.Fatalf("attempts=%v, want all three same-tier credentials", exec.attempts)
			}
		})
	}
}
