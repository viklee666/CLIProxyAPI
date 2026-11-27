package auth

import (
	"context"
	"net/http"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func TestManagerExecuteDisabledRetriesOAuthRecovery(t *testing.T) {
	for _, tc := range []struct {
		name           string
		allowRecovery  bool
		refreshFails   bool
		refreshedFails bool
		wantError      bool
		wantCalls      int
		wantRefreshes  int
	}{
		{name: "default remains single attempt", wantError: true, wantCalls: 1},
		{name: "explicit same identity recovery", allowRecovery: true, wantCalls: 2, wantRefreshes: 1},
		{name: "refresh failure never changes identity", allowRecovery: true, refreshFails: true, wantError: true, wantCalls: 1, wantRefreshes: 1},
		{name: "second 401 never changes identity", allowRecovery: true, refreshedFails: true, wantError: true, wantCalls: 2, wantRefreshes: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager, executor, primary, backup, model := newUnauthorizedRefreshFixture(t, tc.refreshFails)
			manager.SetRetryConfig(8, 0, 0)
			if tc.refreshedFails {
				executor.tokenInvalid["fresh-access-token"] = struct{}{}
			}
			ctx := cliproxyexecutor.WithRequestRetryState(t.Context())
			cliproxyexecutor.DisableRequestRetries(ctx)
			if tc.allowRecovery {
				ctx = cliproxyexecutor.WithOAuthRecoveryAllowed(ctx)
			}
			// Deliberately omit a pin here: disabled generation retries must prevent
			// cross-account fallback independently of the management handler's pin.
			response, err := manager.Execute(ctx, []string{primary.Provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
			if (err != nil) != tc.wantError {
				t.Fatalf("Execute error=%v wantError=%v", err, tc.wantError)
			}
			calls := executor.ExecuteCalls()
			if len(calls) != tc.wantCalls || executor.RefreshCalls() != tc.wantRefreshes {
				t.Fatalf("calls=%v refreshes=%d, want calls=%d refreshes=%d", calls, executor.RefreshCalls(), tc.wantCalls, tc.wantRefreshes)
			}
			for _, id := range calls {
				if id != primary.ID || id == backup.ID {
					t.Fatalf("OAuth recovery switched identity: %v", calls)
				}
			}
			if !tc.wantError && string(response.Payload) != primary.ID+":fresh-access-token" {
				t.Fatalf("wrong recovered response: %s", response.Payload)
			}
			if !cliproxyexecutor.RequestRetriesDisabled(ctx) {
				t.Fatal("OAuth recovery re-enabled ordinary generation retries")
			}
		})
	}
}

type oauthRecoveryPrepareExecutor struct {
	*unauthorizedRefreshExecutor
	prepareCalls []string
}

func (e *oauthRecoveryPrepareExecutor) ShouldPrepareRequestAuth(*Auth) bool { return true }

func (e *oauthRecoveryPrepareExecutor) PrepareRequestAuth(_ context.Context, auth *Auth) (*Auth, error) {
	e.prepareCalls = append(e.prepareCalls, auth.ID)
	if authAccessToken(auth) == "stale-access-token" {
		return auth, &Error{HTTPStatus: http.StatusUnauthorized, Message: "stale preparation token"}
	}
	return auth, nil
}

func TestManagerExecuteDisabledRetriesOAuthPreparationRecovery(t *testing.T) {
	for _, tc := range []struct {
		name          string
		allowRecovery bool
		generation401 bool
		wantError     bool
		wantPrepares  int
		wantExecutes  int
		wantRefreshes int
	}{
		{name: "default remains single attempt", wantError: true, wantPrepares: 1},
		{name: "preparation recovers same identity", allowRecovery: true, wantPrepares: 2, wantExecutes: 1, wantRefreshes: 1},
		{name: "preparation consumes the single refresh", allowRecovery: true, generation401: true, wantError: true, wantPrepares: 2, wantExecutes: 1, wantRefreshes: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager, base, primary, _, model := newUnauthorizedRefreshFixture(t, false)
			executor := &oauthRecoveryPrepareExecutor{unauthorizedRefreshExecutor: base}
			manager.RegisterExecutor(executor)
			if tc.generation401 {
				base.tokenInvalid["fresh-access-token"] = struct{}{}
			}
			ctx := cliproxyexecutor.WithRequestRetryState(t.Context())
			cliproxyexecutor.DisableRequestRetries(ctx)
			if tc.allowRecovery {
				ctx = cliproxyexecutor.WithOAuthRecoveryAllowed(ctx)
			}
			_, err := manager.Execute(ctx, []string{primary.Provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{
				Metadata: map[string]any{cliproxyexecutor.PinnedAuthMetadataKey: primary.ID},
			})
			if (err != nil) != tc.wantError || len(executor.prepareCalls) != tc.wantPrepares || len(base.ExecuteCalls()) != tc.wantExecutes || base.RefreshCalls() != tc.wantRefreshes {
				t.Fatalf("err=%v prepares=%v executes=%v refreshes=%d", err, executor.prepareCalls, base.ExecuteCalls(), base.RefreshCalls())
			}
			for _, id := range executor.prepareCalls {
				if id != primary.ID {
					t.Fatalf("preparation recovered a different identity: %v", executor.prepareCalls)
				}
			}
		})
	}
}

func TestManagerLocalManagementOAuthRecoveryBypassesHome(t *testing.T) {
	manager, executor, primary, _, model := newUnauthorizedRefreshFixture(t, false)
	manager.SetConfig(&internalconfig.Config{Home: internalconfig.HomeConfig{Enabled: true}})
	ctx := cliproxyexecutor.WithRequestRetryState(context.Background())
	cliproxyexecutor.DisableRequestRetries(ctx)
	ctx = cliproxyexecutor.WithOAuthRecoveryAllowed(ctx)
	response, err := manager.Execute(ctx, []string{primary.Provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{
		Metadata: map[string]any{
			LocalManagementExecutionMetadataKey:    true,
			cliproxyexecutor.PinnedAuthMetadataKey: primary.ID,
		},
	})
	if err != nil || string(response.Payload) != primary.ID+":fresh-access-token" {
		t.Fatalf("local management execution = %s, %v", response.Payload, err)
	}
	if len(executor.ExecuteCalls()) != 2 || executor.RefreshCalls() != 1 {
		t.Fatalf("calls=%v refreshes=%d", executor.ExecuteCalls(), executor.RefreshCalls())
	}
}
