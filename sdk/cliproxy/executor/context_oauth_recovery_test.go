package executor

import (
	"context"
	"testing"
)

func TestOAuthRecoveryDoesNotEnableOrdinaryRetries(t *testing.T) {
	ctx := WithRequestRetryState(context.Background())
	DisableRequestRetries(ctx)
	if OAuthRecoveryAllowed(ctx) || OAuthRecoveryAllowed(nil) {
		t.Fatal("OAuth recovery must be explicit")
	}
	allowed := WithOAuthRecoveryAllowed(ctx)
	if !OAuthRecoveryAllowed(allowed) || !RequestRetriesDisabled(allowed) || !RequestRetriesDisabled(ctx) || OAuthRecoveryAllowed(ctx) {
		t.Fatal("OAuth opt-in must preserve retry suppression without changing the parent context")
	}
	if !OAuthRecoveryAllowed(WithRequestRetryState(allowed)) {
		t.Fatal("execution-layer retry state lost the OAuth recovery opt-in")
	}
	if !OAuthRecoveryAllowed(WithOAuthRecoveryAllowed(nil)) {
		t.Fatal("nil-context opt-in was not initialized")
	}
}
