package clientaccess

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	sdkaccess "github.com/router-for-me/CLIProxyAPI/v7/sdk/access"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	service, errNew := New(filepath.Join(t.TempDir(), "client-access.sqlite"))
	if errNew != nil {
		t.Fatalf("New() error = %v", errNew)
	}
	t.Cleanup(func() {
		if errClose := service.Close(); errClose != nil {
			t.Fatalf("Close() error = %v", errClose)
		}
	})
	return service
}

func authenticateSecret(service *Service, secret string) (*sdkaccess.Result, *sdkaccess.AuthError) {
	request, _ := http.NewRequest(http.MethodPost, "http://localhost/v1/responses", nil)
	request.Header.Set("Authorization", "Bearer "+secret)
	return service.Authenticate(context.Background(), request)
}

func TestServiceGroupAndKeyCRUD(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	group, errCreateGroup := service.CreateGroup(ctx, GroupCreate{Name: "Codex Team", Description: "primary"})
	if errCreateGroup != nil {
		t.Fatalf("CreateGroup() error = %v", errCreateGroup)
	}
	if !group.Enabled || group.Name != "Codex Team" {
		t.Fatalf("CreateGroup() = %+v", group)
	}

	allowAll := false
	created, errCreateKey := service.CreateKey(ctx, KeyCreate{
		Name:             "desktop",
		CustomSecret:     "sk-cpa-test-secret-0001",
		AllowAllGroups:   &allowAll,
		GroupIDs:         []int64{group.ID},
		RPMLimit:         10,
		ConcurrencyLimit: 2,
	})
	if errCreateKey != nil {
		t.Fatalf("CreateKey() error = %v", errCreateKey)
	}
	if created.Secret != "sk-cpa-test-secret-0001" || created.KeyMask == created.Secret {
		t.Fatalf("CreateKey() leaked or lost secret: %+v", created)
	}
	if len(created.GroupIDs) != 1 || created.GroupIDs[0] != group.ID {
		t.Fatalf("CreateKey() groups = %v", created.GroupIDs)
	}

	page, errList := service.ListKeys(ctx, ListOptions{Page: 1, PageSize: 10, Search: "desk"})
	if errList != nil {
		t.Fatalf("ListKeys() error = %v", errList)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("ListKeys() = %+v", page)
	}

	disabled := false
	updated, errUpdate := service.UpdateKey(ctx, created.ID, KeyUpdate{Enabled: &disabled, ResetRequestUsage: true})
	if errUpdate != nil {
		t.Fatalf("UpdateKey() error = %v", errUpdate)
	}
	if updated.Enabled {
		t.Fatalf("UpdateKey() enabled = true")
	}

	if errDeleteKey := service.DeleteKey(ctx, created.ID); errDeleteKey != nil {
		t.Fatalf("DeleteKey() error = %v", errDeleteKey)
	}
	if errDeleteGroup := service.DeleteGroup(ctx, group.ID); errDeleteGroup != nil {
		t.Fatalf("DeleteGroup() error = %v", errDeleteGroup)
	}
}

func TestServiceAuthenticateConcurrencyAndMetadata(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	group, errGroup := service.CreateGroup(ctx, GroupCreate{Name: "premium"})
	if errGroup != nil {
		t.Fatalf("CreateGroup() error = %v", errGroup)
	}
	allowAll := false
	created, errKey := service.CreateKey(ctx, KeyCreate{
		Name:             "limited",
		CustomSecret:     "sk-cpa-test-secret-0002",
		AllowAllGroups:   &allowAll,
		GroupIDs:         []int64{group.ID},
		ConcurrencyLimit: 1,
	})
	if errKey != nil {
		t.Fatalf("CreateKey() error = %v", errKey)
	}

	first, firstErr := authenticateSecret(service, created.Secret)
	if firstErr != nil {
		t.Fatalf("Authenticate() error = %v", firstErr)
	}
	if first.Metadata[MetadataKeyGroupIDs] != "1" || first.Metadata[MetadataKeyAllowAllGroups] != "false" {
		t.Fatalf("Authenticate() metadata = %#v", first.Metadata)
	}
	if first.Release == nil {
		t.Fatal("Authenticate() release is nil")
	}

	_, secondErr := authenticateSecret(service, created.Secret)
	if secondErr == nil || secondErr.Code != sdkaccess.AuthErrorCodeRateLimited {
		t.Fatalf("second Authenticate() error = %#v", secondErr)
	}
	first.Release()

	third, thirdErr := authenticateSecret(service, created.Secret)
	if thirdErr != nil {
		t.Fatalf("third Authenticate() error = %v", thirdErr)
	}
	third.Release()
}

func TestServiceAuthenticateExpiryAndRPM(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	expiredAt := time.Now().Add(-time.Minute)
	expired, errExpired := service.CreateKey(ctx, KeyCreate{
		Name:         "expired",
		CustomSecret: "sk-cpa-test-secret-0003",
		ExpiresAt:    &expiredAt,
	})
	if errExpired != nil {
		t.Fatalf("CreateKey(expired) error = %v", errExpired)
	}
	if _, authErr := authenticateSecret(service, expired.Secret); authErr == nil || authErr.Code != sdkaccess.AuthErrorCodeAccessDenied {
		t.Fatalf("Authenticate(expired) error = %#v", authErr)
	}

	rpm, errRPM := service.CreateKey(ctx, KeyCreate{
		Name:         "rpm",
		CustomSecret: "sk-cpa-test-secret-0004",
		RPMLimit:     1,
	})
	if errRPM != nil {
		t.Fatalf("CreateKey(rpm) error = %v", errRPM)
	}
	result, authErr := authenticateSecret(service, rpm.Secret)
	if authErr != nil {
		t.Fatalf("Authenticate(rpm first) error = %v", authErr)
	}
	result.Release()
	if _, authErr = authenticateSecret(service, rpm.Secret); authErr == nil || authErr.Code != sdkaccess.AuthErrorCodeRateLimited {
		t.Fatalf("Authenticate(rpm second) error = %#v", authErr)
	}
}

func TestServiceCredentialBindingsReplaceClearDisableAndCascade(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	primary, errPrimary := service.CreateGroup(ctx, GroupCreate{Name: "primary"})
	if errPrimary != nil {
		t.Fatalf("CreateGroup(primary) error = %v", errPrimary)
	}
	secondary, errSecondary := service.CreateGroup(ctx, GroupCreate{Name: "secondary"})
	if errSecondary != nil {
		t.Fatalf("CreateGroup(secondary) error = %v", errSecondary)
	}

	errReplace := service.ReplaceCredentialBindings(ctx, CredentialBindingBatch{
		AuthIndices: []string{"auth-a", "auth-b", "auth-a"},
		Groups: []CredentialGroupInput{
			{GroupID: primary.ID, Priority: 20},
			{GroupID: secondary.ID, Priority: 10},
		},
	})
	if errReplace != nil {
		t.Fatalf("ReplaceCredentialBindings() error = %v", errReplace)
	}
	page, errList := service.ListCredentialBindings(ctx, ListOptions{Page: 1, PageSize: 20, AuthIndices: []string{"auth-b"}})
	if errList != nil {
		t.Fatalf("ListCredentialBindings() error = %v", errList)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("ListCredentialBindings(auth-b) = %+v", page)
	}
	allowed, priority, overridden := service.ResolveCredentialAccess("auth-a", []int64{primary.ID, secondary.ID}, false, false)
	if !allowed || !overridden || priority != 20 {
		t.Fatalf("ResolveCredentialAccess() = (%v, %d, %v)", allowed, priority, overridden)
	}

	disabled := false
	if _, errDisable := service.UpdateGroup(ctx, primary.ID, GroupUpdate{Enabled: &disabled}); errDisable != nil {
		t.Fatalf("UpdateGroup(disable) error = %v", errDisable)
	}
	allowed, priority, overridden = service.ResolveCredentialAccess("auth-a", []int64{primary.ID}, false, false)
	if allowed || priority != 0 || overridden {
		t.Fatalf("disabled group still resolved: (%v, %d, %v)", allowed, priority, overridden)
	}

	if errClear := service.ReplaceCredentialBindings(ctx, CredentialBindingBatch{AuthIndices: []string{"auth-a"}}); errClear != nil {
		t.Fatalf("ReplaceCredentialBindings(clear) error = %v", errClear)
	}
	page, errList = service.ListCredentialBindings(ctx, ListOptions{Page: 1, PageSize: 20, AuthIndices: []string{"auth-a"}})
	if errList != nil {
		t.Fatalf("ListCredentialBindings(cleared) error = %v", errList)
	}
	if page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("cleared bindings = %+v", page)
	}

	if errDelete := service.DeleteGroup(ctx, secondary.ID); errDelete != nil {
		t.Fatalf("DeleteGroup() error = %v", errDelete)
	}
	page, errList = service.ListCredentialBindings(ctx, ListOptions{Page: 1, PageSize: 20})
	if errList != nil {
		t.Fatalf("ListCredentialBindings(after cascade) error = %v", errList)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].GroupID != primary.ID {
		t.Fatalf("cascade bindings = %+v", page)
	}
	if errDelete := service.DeleteGroup(ctx, primary.ID); errDelete != nil {
		t.Fatalf("DeleteGroup(primary) error = %v", errDelete)
	}
	page, errList = service.ListCredentialBindings(ctx, ListOptions{Page: 1, PageSize: 20})
	if errList != nil {
		t.Fatalf("ListCredentialBindings(after full cascade) error = %v", errList)
	}
	if page.Total != 0 {
		t.Fatalf("full cascade bindings total = %d", page.Total)
	}
}
