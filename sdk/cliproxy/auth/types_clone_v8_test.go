package auth

import (
	"reflect"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/codex"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/copilot"
)

func TestAuthClonePreservesV8LifecycleAndIndependentMetadata(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	auth := &Auth{
		ID: "clone-lifecycle", RegistrationEpoch: 7, CredentialVersion: 11, Generation: 13,
		CopilotCatalog: &copilot.Catalog{}, CodexRefreshPatch: &codex.CredentialRefreshPatch{},
		RefreshFailures: 3, RequiresSignIn: true,
		RejectedAccessToken: "rejected-access", RejectedSIWCAccessToken: "rejected-siwc-access",
		RejectedRefreshToken: "rejected-refresh", RejectedSIWCRefreshToken: "rejected-siwc-refresh",
		RenewalWarnedRefreshToken: "warned", RefreshError: &Error{Code: "refresh_failed"},
		AdoptedAt: now, AdoptedGrantHash: "grant", RenewalEventAt: now,
		InferenceVerified: true, LastInferenceVerifiedAt: now,
		RenewalVerified: true, LastRenewalVerifiedAt: now,
		antigravityEnumRegistration: "registration",
		cooldownRecordedSeq:         19, forceCooldownUntil: now, quotaForceCooldownUntil: now,
		credQuotaForceUntil: now, credentialRetryAfter: now, nonAuthCredentialRetryAfter: now,
		nonAuthCredentialLastError: &Error{Code: "upstream_failed"},
		nonQuotaCooldownUntil:      now, forceCooldownSeeded: true,
		fileSnapshotPath: "credential.json", fileSnapshotDigest: "digest",
		Metadata:    map[string]any{"siwc": map[string]any{"access_token": "original"}},
		Attributes:  map[string]string{AttributeTenantID: "17"},
		ModelStates: map[string]*ModelState{"model": {LastError: &Error{Code: "model_failed"}}},
	}
	cloned := auth.Clone()
	if !reflect.DeepEqual(auth, cloned) {
		t.Fatal("Clone lost credential lifecycle, cooldown, or source snapshot state")
	}
	cloned.MutateMetadata(func(meta map[string]any) {
		meta["siwc"].(map[string]any)["access_token"] = "changed"
	})
	cloned.MutateAttributes(func(attrs map[string]string) { attrs[AttributeTenantID] = "18" })
	cloned.RefreshError.Code = "changed"
	cloned.nonAuthCredentialLastError.Code = "changed"
	cloned.ModelStates["model"].LastError.Code = "changed"
	if auth.Metadata["siwc"].(map[string]any)["access_token"] != "original" || auth.ReadAttribute(AttributeTenantID) != "17" {
		t.Fatal("Clone shares credential metadata or ownership attributes")
	}
	if auth.RefreshError.Code != "refresh_failed" || auth.nonAuthCredentialLastError.Code != "upstream_failed" || auth.ModelStates["model"].LastError.Code != "model_failed" {
		t.Fatal("Clone shares mutable lifecycle errors")
	}
}
