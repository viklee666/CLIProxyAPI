package clientaccess

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sdkaccess "github.com/router-for-me/CLIProxyAPI/v7/sdk/access"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

type testCredentialOwnership map[string]int64

func (owners testCredentialOwnership) OwnerOf(authIndex string) int64 {
	return owners[authIndex]
}

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

func TestServiceMigratesLegacyTenantIDColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-access.sqlite")
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, errOpen := sql.Open("sqlite", dsn)
	if errOpen != nil {
		t.Fatalf("open legacy database: %v", errOpen)
	}
	for _, statement := range []string{
		`CREATE TABLE client_access_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL COLLATE NOCASE UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL
		)`,
		`CREATE TABLE client_access_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			key_secret TEXT NOT NULL DEFAULT '',
			key_prefix TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			enabled INTEGER NOT NULL DEFAULT 1,
			allow_all_groups INTEGER NOT NULL DEFAULT 1,
			allow_ungrouped INTEGER NOT NULL DEFAULT 0,
			expires_at_ms INTEGER,
			rpm_limit INTEGER NOT NULL DEFAULT 0,
			concurrency_limit INTEGER NOT NULL DEFAULT 0,
			request_limit_total INTEGER NOT NULL DEFAULT 0,
			request_used_total INTEGER NOT NULL DEFAULT 0,
			request_limit_5h INTEGER NOT NULL DEFAULT 0,
			request_used_5h INTEGER NOT NULL DEFAULT 0,
			request_window_5h_ms INTEGER,
			request_limit_1d INTEGER NOT NULL DEFAULT 0,
			request_used_1d INTEGER NOT NULL DEFAULT 0,
			request_window_1d_ms INTEGER,
			request_limit_7d INTEGER NOT NULL DEFAULT 0,
			request_used_7d INTEGER NOT NULL DEFAULT 0,
			request_window_7d_ms INTEGER,
			token_limit_total INTEGER NOT NULL DEFAULT 0,
			token_used_total INTEGER NOT NULL DEFAULT 0,
			token_limit_5h INTEGER NOT NULL DEFAULT 0,
			token_used_5h INTEGER NOT NULL DEFAULT 0,
			token_window_5h_ms INTEGER,
			token_limit_1d INTEGER NOT NULL DEFAULT 0,
			token_used_1d INTEGER NOT NULL DEFAULT 0,
			token_window_1d_ms INTEGER,
			token_limit_7d INTEGER NOT NULL DEFAULT 0,
			token_used_7d INTEGER NOT NULL DEFAULT 0,
			token_window_7d_ms INTEGER,
			last_used_at_ms INTEGER,
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL
		)`,
		`INSERT INTO client_access_groups(name, description, enabled, created_at_ms, updated_at_ms) VALUES('legacy', '', 1, 1, 1)`,
	} {
		if _, errExec := db.Exec(statement); errExec != nil {
			_ = db.Close()
			t.Fatalf("seed legacy database: %v", errExec)
		}
	}
	if errClose := db.Close(); errClose != nil {
		t.Fatalf("close legacy database: %v", errClose)
	}

	service, errNew := New(path)
	if errNew != nil {
		t.Fatalf("New(legacy database) error = %v", errNew)
	}
	defer func() { _ = service.Close() }()

	groups, errList := service.ListGroups(context.Background(), ListOptions{Page: 1, PageSize: 10})
	if errList != nil {
		t.Fatalf("ListGroups() error = %v", errList)
	}
	if groups.Total != 1 || len(groups.Items) != 1 || groups.Items[0].Name != "legacy" {
		t.Fatalf("ListGroups() = %+v", groups)
	}
	if groups.Items[0].TenantID != 0 {
		t.Fatalf("legacy group tenant_id = %d, want 0", groups.Items[0].TenantID)
	}
}

func TestServiceMigratesLegacyKeySecretColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-access.sqlite")
	service, errNew := New(path)
	if errNew != nil {
		t.Fatalf("New() error = %v", errNew)
	}
	legacy, errCreateLegacy := service.CreateKey(context.Background(), KeyCreate{
		Name:         "legacy",
		CustomSecret: "sk-cpa-legacy-secret",
	})
	if errCreateLegacy != nil {
		t.Fatalf("CreateKey(legacy) error = %v", errCreateLegacy)
	}
	if _, errDrop := service.store.db.Exec(`ALTER TABLE client_access_keys DROP COLUMN key_secret`); errDrop != nil {
		t.Fatalf("drop key_secret column: %v", errDrop)
	}
	if errClose := service.Close(); errClose != nil {
		t.Fatalf("Close() error = %v", errClose)
	}

	reopened, errReopen := New(path)
	if errReopen != nil {
		t.Fatalf("New(legacy database) error = %v", errReopen)
	}
	defer func() { _ = reopened.Close() }()
	legacyAfterMigration, errGetLegacy := reopened.GetKey(context.Background(), legacy.ID)
	if errGetLegacy != nil {
		t.Fatalf("GetKey(legacy) error = %v", errGetLegacy)
	}
	if legacyAfterMigration.Secret != "" {
		t.Fatalf("legacy secret should be unavailable after migration, got %q", legacyAfterMigration.Secret)
	}
	legacyAuth, legacyAuthErr := authenticateSecret(reopened, "sk-cpa-legacy-secret")
	if legacyAuthErr != nil {
		t.Fatalf("Authenticate(legacy) error = %v", legacyAuthErr)
	}
	legacyAuth.Release()
	created, errCreate := reopened.CreateKey(context.Background(), KeyCreate{
		Name:         "migrated",
		CustomSecret: "sk-cpa-migrated-secret",
	})
	if errCreate != nil {
		t.Fatalf("CreateKey() error = %v", errCreate)
	}
	if created.Secret != "sk-cpa-migrated-secret" {
		t.Fatalf("CreateKey() secret = %q", created.Secret)
	}
}

func TestServicePersistentRequestQuotaAndWindowReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-access.sqlite")
	service, errNew := New(path)
	if errNew != nil {
		t.Fatalf("New() error = %v", errNew)
	}
	created, errCreate := service.CreateKey(context.Background(), KeyCreate{
		Name:              "request-limited",
		CustomSecret:      "sk-cpa-request-quota-test",
		RequestLimitTotal: 2,
		RequestLimit5h:    1,
	})
	if errCreate != nil {
		t.Fatalf("CreateKey() error = %v", errCreate)
	}
	first, authErr := authenticateSecret(service, created.Secret)
	if authErr != nil {
		t.Fatalf("first Authenticate() error = %v", authErr)
	}
	first.Release()
	if _, authErr = authenticateSecret(service, created.Secret); authErr == nil || authErr.Code != sdkaccess.AuthErrorCodeRateLimited {
		t.Fatalf("second Authenticate() error = %#v", authErr)
	}
	if authErr.Headers.Get("Retry-After") == "" || authErr.Headers.Get("X-RateLimit-Reset") == "" {
		t.Fatalf("quota reset headers = %#v", authErr.Headers)
	}
	past := time.Now().Add(-6 * time.Hour).UnixMilli()
	if _, errExec := service.store.db.Exec(`UPDATE client_access_keys SET request_window_5h_ms = ?, request_used_5h = 1 WHERE id = ?`, past, created.ID); errExec != nil {
		t.Fatalf("expire request window: %v", errExec)
	}
	third, authErr := authenticateSecret(service, created.Secret)
	if authErr != nil {
		t.Fatalf("Authenticate(after reset) error = %v", authErr)
	}
	third.Release()
	if errClose := service.Close(); errClose != nil {
		t.Fatalf("Close() error = %v", errClose)
	}

	reopened, errReopen := New(path)
	if errReopen != nil {
		t.Fatalf("New(reopen) error = %v", errReopen)
	}
	defer func() { _ = reopened.Close() }()
	if _, authErr = authenticateSecret(reopened, created.Secret); authErr == nil || authErr.Code != sdkaccess.AuthErrorCodeRateLimited {
		t.Fatalf("Authenticate(reopened total limit) error = %#v", authErr)
	}
	key, errGet := reopened.GetKey(context.Background(), created.ID)
	if errGet != nil {
		t.Fatalf("GetKey() error = %v", errGet)
	}
	if key.RequestUsedTotal != 2 || key.RequestUsed5h != 1 || key.RequestWindow5hAt == nil {
		t.Fatalf("persisted request usage = %+v", key)
	}
}

func TestServiceTokenReservationSettlementAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-access.sqlite")
	service, errNew := New(path, WithTokenReservation(80))
	if errNew != nil {
		t.Fatalf("New() error = %v", errNew)
	}
	created, errCreate := service.CreateKey(context.Background(), KeyCreate{
		Name:            "token-limited",
		CustomSecret:    "sk-cpa-token-quota-test",
		TokenLimitTotal: 100,
	})
	if errCreate != nil {
		t.Fatalf("CreateKey() error = %v", errCreate)
	}
	first, firstErr := authenticateSecret(service, created.Secret)
	if firstErr != nil {
		t.Fatalf("first Authenticate() error = %v", firstErr)
	}
	second, secondErr := authenticateSecret(service, created.Secret)
	if secondErr != nil {
		t.Fatalf("second Authenticate() error = %v", secondErr)
	}
	if _, thirdErr := authenticateSecret(service, created.Secret); thirdErr == nil || thirdErr.Code != sdkaccess.AuthErrorCodeRateLimited {
		t.Fatalf("third Authenticate() error = %#v", thirdErr)
	}
	first.Release()
	service.HandleUsage(context.Background(), coreusage.Record{
		APIKey:              created.Secret,
		ClientReservationID: first.Metadata[MetadataKeyReservationID],
		Detail:              coreusage.Detail{TotalTokens: 30},
	})
	key, errGet := service.GetKey(context.Background(), created.ID)
	if errGet != nil {
		t.Fatalf("GetKey() error = %v", errGet)
	}
	if key.TokenUsedTotal != 30 || key.TokenReserved != 20 {
		t.Fatalf("settled token usage = %+v", key)
	}
	service.HandleUsage(context.Background(), coreusage.Record{
		APIKey:              created.Secret,
		ClientReservationID: first.Metadata[MetadataKeyReservationID],
		Detail:              coreusage.Detail{InputTokens: 5, OutputTokens: 5},
	})
	key, errGet = service.GetKey(context.Background(), created.ID)
	if errGet != nil {
		t.Fatalf("GetKey(after additional usage) error = %v", errGet)
	}
	if key.TokenUsedTotal != 40 {
		t.Fatalf("additional token usage = %d", key.TokenUsedTotal)
	}
	second.Release()
	service.HandleUsage(context.Background(), coreusage.Record{
		APIKey:              created.Secret,
		ClientReservationID: second.Metadata[MetadataKeyReservationID],
		Detail:              coreusage.Detail{TotalTokens: 20},
	})
	if errClose := service.Close(); errClose != nil {
		t.Fatalf("Close() error = %v", errClose)
	}

	reopened, errReopen := New(path, WithTokenReservation(80))
	if errReopen != nil {
		t.Fatalf("New(reopen) error = %v", errReopen)
	}
	defer func() { _ = reopened.Close() }()
	key, errGet = reopened.GetKey(context.Background(), created.ID)
	if errGet != nil {
		t.Fatalf("GetKey(reopened) error = %v", errGet)
	}
	if key.TokenUsedTotal != 60 || key.TokenReserved != 0 {
		t.Fatalf("persisted token usage = %+v", key)
	}
}

func TestServiceRequestQuotaReservationIsAtomic(t *testing.T) {
	service := newTestService(t)
	created, errCreate := service.CreateKey(context.Background(), KeyCreate{
		Name:              "atomic",
		CustomSecret:      "sk-cpa-atomic-quota-test",
		RequestLimitTotal: 5,
	})
	if errCreate != nil {
		t.Fatalf("CreateKey() error = %v", errCreate)
	}
	const attempts = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, authErr := authenticateSecret(service, created.Secret)
			if authErr != nil {
				return
			}
			result.Release()
			mu.Lock()
			succeeded++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if succeeded != 5 {
		t.Fatalf("successful reservations = %d, want 5", succeeded)
	}
	key, errGet := service.GetKey(context.Background(), created.ID)
	if errGet != nil {
		t.Fatalf("GetKey() error = %v", errGet)
	}
	if key.RequestUsedTotal != 5 {
		t.Fatalf("RequestUsedTotal = %d", key.RequestUsedTotal)
	}
}

func TestServiceAllPersistentQuotaWindows(t *testing.T) {
	tests := []struct {
		name   string
		window string
		create func(secret string) KeyCreate
	}{
		{name: "request-1d", window: "1d", create: func(secret string) KeyCreate {
			return KeyCreate{Name: "request-1d", CustomSecret: secret, RequestLimit1d: 1}
		}},
		{name: "request-7d", window: "7d", create: func(secret string) KeyCreate {
			return KeyCreate{Name: "request-7d", CustomSecret: secret, RequestLimit7d: 1}
		}},
		{name: "token-5h", window: "5h", create: func(secret string) KeyCreate {
			return KeyCreate{Name: "token-5h", CustomSecret: secret, TokenLimit5h: 1}
		}},
		{name: "token-1d", window: "1d", create: func(secret string) KeyCreate {
			return KeyCreate{Name: "token-1d", CustomSecret: secret, TokenLimit1d: 1}
		}},
		{name: "token-7d", window: "7d", create: func(secret string) KeyCreate {
			return KeyCreate{Name: "token-7d", CustomSecret: secret, TokenLimit7d: 1}
		}},
	}
	for index, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			service := newTestService(t)
			secret := fmt.Sprintf("sk-cpa-window-quota-%02d", index)
			created, errCreate := service.CreateKey(context.Background(), testCase.create(secret))
			if errCreate != nil {
				t.Fatalf("CreateKey() error = %v", errCreate)
			}
			first, firstErr := authenticateSecret(service, created.Secret)
			if firstErr != nil {
				t.Fatalf("first Authenticate() error = %v", firstErr)
			}
			if strings.HasPrefix(testCase.name, "token-") {
				service.HandleUsage(context.Background(), coreusage.Record{
					APIKey:              created.Secret,
					ClientReservationID: first.Metadata[MetadataKeyReservationID],
					Detail:              coreusage.Detail{TotalTokens: 1},
				})
			}
			first.Release()
			_, secondErr := authenticateSecret(service, created.Secret)
			if secondErr == nil || secondErr.Code != sdkaccess.AuthErrorCodeRateLimited || !strings.Contains(secondErr.Message, testCase.window) {
				t.Fatalf("second Authenticate() error = %#v", secondErr)
			}
		})
	}
}

func TestServiceResetTokenUsageClearsReservations(t *testing.T) {
	service := newTestService(t)
	created, errCreate := service.CreateKey(context.Background(), KeyCreate{
		Name:            "reset-token",
		CustomSecret:    "sk-cpa-reset-token-usage",
		TokenLimitTotal: 100,
	})
	if errCreate != nil {
		t.Fatalf("CreateKey() error = %v", errCreate)
	}
	result, authErr := authenticateSecret(service, created.Secret)
	if authErr != nil {
		t.Fatalf("Authenticate() error = %v", authErr)
	}
	key, errGet := service.GetKey(context.Background(), created.ID)
	if errGet != nil {
		t.Fatalf("GetKey() error = %v", errGet)
	}
	if key.TokenReserved == 0 {
		t.Fatalf("TokenReserved = %d", key.TokenReserved)
	}
	updated, errUpdate := service.UpdateKey(context.Background(), created.ID, KeyUpdate{ResetTokenUsage: true})
	if errUpdate != nil {
		t.Fatalf("UpdateKey(reset) error = %v", errUpdate)
	}
	if updated.TokenUsedTotal != 0 || updated.TokenReserved != 0 {
		t.Fatalf("reset key = %+v", updated)
	}
	result.Release()
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
	if page.Items[0].Secret != created.Secret {
		t.Fatalf("ListKeys() secret = %q, want %q", page.Items[0].Secret, created.Secret)
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

func TestServiceAuthenticateIncludesTenantID(t *testing.T) {
	service := newTestService(t)
	created, errKey := service.CreateKey(context.Background(), KeyCreate{
		TenantID:     42,
		Name:         "tenant key",
		CustomSecret: "sk-cpa-test-secret-tenant",
	})
	if errKey != nil {
		t.Fatalf("CreateKey() error = %v", errKey)
	}
	result, authErr := authenticateSecret(service, created.Secret)
	if authErr != nil {
		t.Fatalf("Authenticate() error = %v", authErr)
	}
	if got := result.Metadata[MetadataKeyTenantID]; got != "42" {
		t.Fatalf("tenant metadata = %q, want 42", got)
	}
	result.Release()
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

func TestServiceDeleteCredentialBindingsPreservesOtherCredentials(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	group, errGroup := service.CreateGroup(ctx, GroupCreate{Name: "primary"})
	if errGroup != nil {
		t.Fatalf("CreateGroup() error = %v", errGroup)
	}
	if errReplace := service.ReplaceCredentialBindings(ctx, CredentialBindingBatch{
		AuthIndices: []string{"auth-delete", "auth-keep"},
		Groups:      []CredentialGroupInput{{GroupID: group.ID, Priority: 20}},
	}); errReplace != nil {
		t.Fatalf("ReplaceCredentialBindings() error = %v", errReplace)
	}

	if errDelete := service.DeleteCredentialBindings(ctx, []string{"auth-delete", "auth-delete", ""}); errDelete != nil {
		t.Fatalf("DeleteCredentialBindings() error = %v", errDelete)
	}
	page, errList := service.ListCredentialBindings(ctx, ListOptions{Page: 1, PageSize: 20})
	if errList != nil {
		t.Fatalf("ListCredentialBindings() error = %v", errList)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].AuthIndex != "auth-keep" {
		t.Fatalf("remaining bindings = %+v", page)
	}
	groups, errGroups := service.ListGroups(ctx, ListOptions{Page: 1, PageSize: 20})
	if errGroups != nil {
		t.Fatalf("ListGroups() error = %v", errGroups)
	}
	if len(groups.Items) != 1 || groups.Items[0].CredentialCount != 1 {
		t.Fatalf("groups after delete = %+v", groups)
	}
	allowed, priority, overridden := service.ResolveCredentialAccess("auth-delete", []int64{group.ID}, false, false)
	if allowed || priority != 0 || overridden {
		t.Fatalf("deleted credential still resolved: (%v, %d, %v)", allowed, priority, overridden)
	}
	allowed, priority, overridden = service.ResolveCredentialAccess("auth-keep", []int64{group.ID}, false, false)
	if !allowed || priority != 20 || !overridden {
		t.Fatalf("preserved credential resolution = (%v, %d, %v)", allowed, priority, overridden)
	}
}

func TestServiceReplaceGroupCredentialBindingsPreservesOtherGroups(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	first, errFirst := service.CreateGroup(ctx, GroupCreate{Name: "first"})
	if errFirst != nil {
		t.Fatalf("CreateGroup(first) error = %v", errFirst)
	}
	second, errSecond := service.CreateGroup(ctx, GroupCreate{Name: "second"})
	if errSecond != nil {
		t.Fatalf("CreateGroup(second) error = %v", errSecond)
	}
	if errReplace := service.ReplaceCredentialBindings(ctx, CredentialBindingBatch{
		AuthIndices: []string{"shared", "first-only"},
		Groups:      []CredentialGroupInput{{GroupID: first.ID, Priority: 10}},
	}); errReplace != nil {
		t.Fatalf("ReplaceCredentialBindings(first) error = %v", errReplace)
	}
	if errReplace := service.ReplaceCredentialBindings(ctx, CredentialBindingBatch{
		AuthIndices: []string{"shared", "second-only"},
		Groups:      []CredentialGroupInput{{GroupID: second.ID, Priority: 20}},
	}); errReplace != nil {
		t.Fatalf("ReplaceCredentialBindings(second) error = %v", errReplace)
	}

	if _, errReplace := service.ReplaceGroupCredentialBindings(ctx, first.ID, GroupCredentialBindingBatch{
		AuthIndices: []string{"first-next", "shared"},
		Priority:    30,
	}); errReplace != nil {
		t.Fatalf("ReplaceGroupCredentialBindings() error = %v", errReplace)
	}
	page, errList := service.ListCredentialBindings(ctx, ListOptions{Page: 1, PageSize: 20, AuthIndices: []string{"shared", "second-only"}})
	if errList != nil {
		t.Fatalf("ListCredentialBindings() error = %v", errList)
	}
	if page.Total != 3 {
		t.Fatalf("bindings total = %d, want 3", page.Total)
	}
	allowed, priority, overridden := service.ResolveCredentialAccess("shared", []int64{first.ID, second.ID}, false, false)
	if !allowed || !overridden || priority != 30 {
		t.Fatalf("ResolveCredentialAccess(shared) = (%v, %d, %v)", allowed, priority, overridden)
	}
	if _, errClear := service.ReplaceGroupCredentialBindings(ctx, first.ID, GroupCredentialBindingBatch{}); errClear != nil {
		t.Fatalf("ReplaceGroupCredentialBindings(clear) error = %v", errClear)
	}
	allowed, priority, overridden = service.ResolveCredentialAccess("shared", []int64{first.ID}, false, false)
	if allowed || overridden || priority != 0 {
		t.Fatalf("ResolveCredentialAccess(cleared first group) = (%v, %d, %v)", allowed, priority, overridden)
	}
	allowed, priority, overridden = service.ResolveCredentialAccess("shared", []int64{second.ID}, false, false)
	if !allowed || !overridden || priority != 20 {
		t.Fatalf("ResolveCredentialAccess(preserved second group) = (%v, %d, %v)", allowed, priority, overridden)
	}
}

func TestTenantScopedResourcesRejectCrossTenantAccess(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	firstGroup, errFirstGroup := service.CreateTenantGroup(ctx, 101, GroupCreate{Name: "tenant-101"})
	if errFirstGroup != nil {
		t.Fatalf("CreateTenantGroup(first) error = %v", errFirstGroup)
	}
	secondGroup, errSecondGroup := service.CreateTenantGroup(ctx, 202, GroupCreate{Name: "tenant-202"})
	if errSecondGroup != nil {
		t.Fatalf("CreateTenantGroup(second) error = %v", errSecondGroup)
	}

	if _, errGet := service.GetTenantGroup(ctx, 101, secondGroup.ID); !errors.Is(errGet, sql.ErrNoRows) {
		t.Fatalf("GetTenantGroup(cross tenant) error = %v, want sql.ErrNoRows", errGet)
	}
	groups, errListGroups := service.ListTenantGroups(ctx, 101, ListOptions{Page: 1, PageSize: 20})
	if errListGroups != nil {
		t.Fatalf("ListTenantGroups() error = %v", errListGroups)
	}
	if groups.Total != 1 || len(groups.Items) != 1 || groups.Items[0].ID != firstGroup.ID || groups.Items[0].TenantID != 101 {
		t.Fatalf("ListTenantGroups() = %+v", groups)
	}
	if _, errUpdate := service.UpdateTenantGroup(ctx, 101, secondGroup.ID, GroupUpdate{}); !errors.Is(errUpdate, sql.ErrNoRows) {
		t.Fatalf("UpdateTenantGroup(cross tenant) error = %v, want sql.ErrNoRows", errUpdate)
	}
	if errDelete := service.DeleteTenantGroup(ctx, 101, secondGroup.ID); !errors.Is(errDelete, sql.ErrNoRows) {
		t.Fatalf("DeleteTenantGroup(cross tenant) error = %v, want sql.ErrNoRows", errDelete)
	}

	if _, errCreate := service.CreateTenantKey(ctx, 101, KeyCreate{
		Name:         "invalid-cross-group",
		CustomSecret: "sk-cpa-invalid-cross-group",
		GroupIDs:     []int64{secondGroup.ID},
	}); !errors.Is(errCreate, sql.ErrNoRows) {
		t.Fatalf("CreateTenantKey(cross tenant group) error = %v, want sql.ErrNoRows", errCreate)
	}
	created, errCreate := service.CreateTenantKey(ctx, 101, KeyCreate{
		Name:              "tenant-key",
		CustomSecret:      "sk-cpa-tenant-scoped-key",
		GroupIDs:          []int64{firstGroup.ID},
		RPMLimit:          77,
		RequestLimitTotal: 88,
		TokenLimitTotal:   99,
	})
	if errCreate != nil {
		t.Fatalf("CreateTenantKey() error = %v", errCreate)
	}
	if created.TenantID != 101 || created.RPMLimit != 0 || created.RequestLimitTotal != 0 || created.TokenLimitTotal != 0 || created.ExpiresAt != nil {
		t.Fatalf("CreateTenantKey() did not sanitize tenant quotas: %+v", created.Key)
	}
	secondKey, errSecondKey := service.CreateTenantKey(ctx, 202, KeyCreate{Name: "tenant-202-key", CustomSecret: "sk-cpa-tenant-202-key", GroupIDs: []int64{secondGroup.ID}})
	if errSecondKey != nil {
		t.Fatalf("CreateTenantKey(second) error = %v", errSecondKey)
	}
	if _, errGet := service.GetTenantKey(ctx, 101, secondKey.ID); !errors.Is(errGet, sql.ErrNoRows) {
		t.Fatalf("GetTenantKey(cross tenant) error = %v, want sql.ErrNoRows", errGet)
	}
	maliciousRPM := 123
	concurrency := 4
	updated, errUpdate := service.UpdateTenantKey(ctx, 101, created.ID, KeyUpdate{RPMLimit: &maliciousRPM, ConcurrencyLimit: &concurrency})
	if errUpdate != nil {
		t.Fatalf("UpdateTenantKey() error = %v", errUpdate)
	}
	if updated.RPMLimit != 0 || updated.ConcurrencyLimit != concurrency || updated.RequestLimitTotal != 0 || updated.TokenLimitTotal != 0 || updated.ExpiresAt != nil {
		t.Fatalf("UpdateTenantKey() did not sanitize tenant quotas: %+v", updated)
	}
	if _, errUpdate := service.UpdateTenantKey(ctx, 101, secondKey.ID, KeyUpdate{}); !errors.Is(errUpdate, sql.ErrNoRows) {
		t.Fatalf("UpdateTenantKey(cross tenant) error = %v, want sql.ErrNoRows", errUpdate)
	}
}

func TestTenantCredentialBindingsRequireOwnedAuthAndScopedGroup(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	firstGroup, errFirst := service.CreateTenantGroup(ctx, 11, GroupCreate{Name: "tenant-11"})
	if errFirst != nil {
		t.Fatalf("CreateTenantGroup(first) error = %v", errFirst)
	}
	secondGroup, errSecond := service.CreateTenantGroup(ctx, 22, GroupCreate{Name: "tenant-22"})
	if errSecond != nil {
		t.Fatalf("CreateTenantGroup(second) error = %v", errSecond)
	}
	owners := testCredentialOwnership{"owned-by-11": 11, "owned-by-22": 22}
	if _, errReplace := service.ReplaceTenantGroupCredentialBindings(ctx, 11, firstGroup.ID, GroupCredentialBindingBatch{AuthIndices: []string{"owned-by-22"}}, owners); !errors.Is(errReplace, sql.ErrNoRows) {
		t.Fatalf("ReplaceTenantGroupCredentialBindings(cross auth) error = %v, want sql.ErrNoRows", errReplace)
	}
	if _, errReplace := service.ReplaceTenantGroupCredentialBindings(ctx, 11, secondGroup.ID, GroupCredentialBindingBatch{AuthIndices: []string{"owned-by-11"}}, owners); !errors.Is(errReplace, sql.ErrNoRows) {
		t.Fatalf("ReplaceTenantGroupCredentialBindings(cross group) error = %v, want sql.ErrNoRows", errReplace)
	}
	if _, errReplace := service.ReplaceTenantGroupCredentialBindings(ctx, 11, firstGroup.ID, GroupCredentialBindingBatch{AuthIndices: []string{"owned-by-11"}, Priority: 9}, owners); errReplace != nil {
		t.Fatalf("ReplaceTenantGroupCredentialBindings() error = %v", errReplace)
	}
	bindings, errList := service.ListTenantCredentialBindings(ctx, 11, ListOptions{Page: 1, PageSize: 20})
	if errList != nil {
		t.Fatalf("ListTenantCredentialBindings() error = %v", errList)
	}
	if bindings.Total != 1 || len(bindings.Items) != 1 || bindings.Items[0].AuthIndex != "owned-by-11" || bindings.Items[0].GroupID != firstGroup.ID {
		t.Fatalf("ListTenantCredentialBindings() = %+v", bindings)
	}
	otherBindings, errOtherList := service.ListTenantCredentialBindings(ctx, 22, ListOptions{Page: 1, PageSize: 20})
	if errOtherList != nil {
		t.Fatalf("ListTenantCredentialBindings(other) error = %v", errOtherList)
	}
	if otherBindings.Total != 0 {
		t.Fatalf("tenant 22 unexpectedly saw tenant 11 bindings: %+v", otherBindings)
	}
}

func TestDeleteTenantResourcesLeavesAdministratorResources(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	tenantGroup, errTenantGroup := service.CreateTenantGroup(ctx, 7, GroupCreate{Name: "tenant-delete"})
	if errTenantGroup != nil {
		t.Fatalf("CreateTenantGroup() error = %v", errTenantGroup)
	}
	tenantKey, errTenantKey := service.CreateTenantKey(ctx, 7, KeyCreate{Name: "tenant-delete-key", CustomSecret: "sk-cpa-tenant-delete", GroupIDs: []int64{tenantGroup.ID}})
	if errTenantKey != nil {
		t.Fatalf("CreateTenantKey() error = %v", errTenantKey)
	}
	globalGroup, errGlobalGroup := service.CreateGroup(ctx, GroupCreate{Name: "administrator-group"})
	if errGlobalGroup != nil {
		t.Fatalf("CreateGroup() error = %v", errGlobalGroup)
	}
	globalKey, errGlobalKey := service.CreateKey(ctx, KeyCreate{Name: "administrator-key", CustomSecret: "sk-cpa-administrator", GroupIDs: []int64{globalGroup.ID}})
	if errGlobalKey != nil {
		t.Fatalf("CreateKey() error = %v", errGlobalKey)
	}

	if errDelete := service.DeleteTenantResources(ctx, 7); errDelete != nil {
		t.Fatalf("DeleteTenantResources() error = %v", errDelete)
	}
	if _, errGet := service.GetTenantGroup(ctx, 7, tenantGroup.ID); !errors.Is(errGet, sql.ErrNoRows) {
		t.Fatalf("tenant group survived cleanup: %v", errGet)
	}
	if _, errGet := service.GetTenantKey(ctx, 7, tenantKey.ID); !errors.Is(errGet, sql.ErrNoRows) {
		t.Fatalf("tenant key survived cleanup: %v", errGet)
	}
	if _, errGet := service.GetGroup(ctx, globalGroup.ID); errGet != nil {
		t.Fatalf("GetGroup(global) error = %v", errGet)
	}
	if _, errGet := service.GetKey(ctx, globalKey.ID); errGet != nil {
		t.Fatalf("GetKey(global) error = %v", errGet)
	}
}
