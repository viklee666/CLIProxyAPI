package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestServicePasswordSessionAndReset(t *testing.T) {
	t.Parallel()
	service, errNew := New(filepath.Join(t.TempDir(), "tenant.sqlite"))
	if errNew != nil {
		t.Fatalf("New() error = %v", errNew)
	}
	t.Cleanup(func() {
		if errClose := service.Close(); errClose != nil {
			t.Fatalf("Close() error = %v", errClose)
		}
	})

	created, password, errCreate := service.Create(context.Background(), CreateInput{DisplayName: "Tenant A"})
	if errCreate != nil {
		t.Fatalf("Create() error = %v", errCreate)
	}
	if len(password) != GeneratedPasswordLength {
		t.Fatalf("generated password length = %d, want %d", len(password), GeneratedPasswordLength)
	}
	matched, errAuthenticate := service.AuthenticatePassword(context.Background(), password)
	if errAuthenticate != nil {
		t.Fatalf("AuthenticatePassword() error = %v", errAuthenticate)
	}
	if matched.ID != created.ID {
		t.Fatalf("authenticated tenant = %d, want %d", matched.ID, created.ID)
	}
	token, _, errIssue := service.IssueSession(context.Background(), created.ID, 0)
	if errIssue != nil {
		t.Fatalf("IssueSession() error = %v", errIssue)
	}
	if _, errSession := service.AuthenticateSession(context.Background(), token); errSession != nil {
		t.Fatalf("AuthenticateSession() error = %v", errSession)
	}
	if errChange := service.ChangePassword(context.Background(), created.ID, password, "changed-password"); errChange != nil {
		t.Fatalf("ChangePassword() error = %v", errChange)
	}
	if _, errOldSession := service.AuthenticateSession(context.Background(), token); !errors.Is(errOldSession, ErrInvalidSession) {
		t.Fatalf("AuthenticateSession() after password change error = %v, want invalid session", errOldSession)
	}
	if _, errOldPassword := service.AuthenticatePassword(context.Background(), password); !errors.Is(errOldPassword, ErrInvalidCredentials) {
		t.Fatalf("AuthenticatePassword() with old password error = %v, want invalid credentials", errOldPassword)
	}
	if _, errNewPassword := service.AuthenticatePassword(context.Background(), "changed-password"); errNewPassword != nil {
		t.Fatalf("AuthenticatePassword() with changed password error = %v", errNewPassword)
	}
}

func TestServiceCredentialOwnershipSnapshot(t *testing.T) {
	t.Parallel()
	service, errNew := New(filepath.Join(t.TempDir(), "tenant.sqlite"))
	if errNew != nil {
		t.Fatalf("New() error = %v", errNew)
	}
	t.Cleanup(func() { _ = service.Close() })
	created, _, errCreate := service.Create(context.Background(), CreateInput{DisplayName: "Tenant A", Password: "password"})
	if errCreate != nil {
		t.Fatalf("Create() error = %v", errCreate)
	}
	if errSet := service.SetCredentialOwnership(context.Background(), "auth-index", created.ID); errSet != nil {
		t.Fatalf("SetCredentialOwnership() error = %v", errSet)
	}
	if owner := service.OwnerOf("auth-index"); owner != created.ID {
		t.Fatalf("OwnerOf() = %d, want %d", owner, created.ID)
	}
	if !service.HasOwnedCredentials() {
		t.Fatal("HasOwnedCredentials() = false, want true")
	}
	if errRemove := service.RemoveCredentialOwnership(context.Background(), "auth-index"); errRemove != nil {
		t.Fatalf("RemoveCredentialOwnership() error = %v", errRemove)
	}
	if owner := service.OwnerOf("auth-index"); owner != 0 {
		t.Fatalf("OwnerOf() after remove = %d, want 0", owner)
	}
}

func TestServiceProviderEncryptionAndSynthesis(t *testing.T) {
	t.Parallel()
	service, errNew := New(filepath.Join(t.TempDir(), "tenant.sqlite"))
	if errNew != nil {
		t.Fatalf("New() error = %v", errNew)
	}
	t.Cleanup(func() { _ = service.Close() })
	created, _, errCreate := service.Create(context.Background(), CreateInput{DisplayName: "Tenant A", Password: "password"})
	if errCreate != nil {
		t.Fatalf("Create() error = %v", errCreate)
	}
	models := json.RawMessage(`[{"name":"claude-sonnet-4","alias":"tenant-sonnet"}]`)
	provider, errProvider := service.CreateProvider(context.Background(), created.ID, ProviderCreateInput{
		Channel: ChannelClaude,
		Name:    "Claude",
		APIKey:  "sk-ant-test-secret",
		Headers: map[string]string{"x-test": "value"},
		Models:  models,
	})
	if errProvider != nil {
		t.Fatalf("CreateProvider() error = %v", errProvider)
	}
	if provider.AuthIndex == "" {
		t.Fatal("CreateProvider() returned an empty pending auth index")
	}
	stored, errStored := service.store.getProvider(context.Background(), created.ID, provider.ID)
	if errStored != nil {
		t.Fatalf("getProvider() error = %v", errStored)
	}
	if stored.apiKeyEnc == "sk-ant-test-secret" {
		t.Fatal("provider API key was persisted in plaintext")
	}
	auths, errSynthesize := service.SynthesizeAuths(context.Background())
	if errSynthesize != nil {
		t.Fatalf("SynthesizeAuths() error = %v", errSynthesize)
	}
	if len(auths) != 1 {
		t.Fatalf("SynthesizeAuths() auth count = %d, want 1", len(auths))
	}
	auth := auths[0]
	if auth.Attributes["api_key"] != "sk-ant-test-secret" {
		t.Fatalf("synthesized api key = %q", auth.Attributes["api_key"])
	}
	if auth.Attributes["tenant_id"] != "1" || auth.Attributes["runtime_only"] != "true" {
		t.Fatalf("synthesized tenant attributes = %#v", auth.Attributes)
	}
	if auth.Prefix != "t1" {
		t.Fatalf("synthesized prefix = %q, want t1", auth.Prefix)
	}
	if service.OwnerOf(auth.Index) != created.ID {
		t.Fatalf("OwnerOf(%q) = %d, want %d", auth.Index, service.OwnerOf(auth.Index), created.ID)
	}
	updated, errUpdated := service.GetProvider(context.Background(), created.ID, provider.ID)
	if errUpdated != nil {
		t.Fatalf("GetProvider() error = %v", errUpdated)
	}
	if updated.AuthIndex != auth.Index {
		t.Fatalf("provider auth index = %q, want %q", updated.AuthIndex, auth.Index)
	}
	if errDelete := service.DeleteProvider(context.Background(), created.ID, provider.ID); errDelete != nil {
		t.Fatalf("DeleteProvider() error = %v", errDelete)
	}
	if service.OwnerOf(auth.Index) != 0 {
		t.Fatalf("OwnerOf(%q) after delete = %d, want 0", auth.Index, service.OwnerOf(auth.Index))
	}
}

func TestTenantProviderPrefixKeepsSingleModelSeparator(t *testing.T) {
	testCases := []struct {
		name           string
		tenantID       int64
		providerPrefix string
		want           string
	}{
		{name: "tenant namespace", tenantID: 1, want: "t1"},
		{name: "custom provider namespace", tenantID: 42, providerPrefix: "team-a", want: "t42/team-a"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := tenantProviderPrefix(testCase.tenantID, testCase.providerPrefix); got != testCase.want {
				t.Fatalf("tenantProviderPrefix() = %q, want %q", got, testCase.want)
			}
		})
	}
}
