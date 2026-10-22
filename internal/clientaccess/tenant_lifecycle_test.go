package clientaccess

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/tenant"
)

type tenantStatusFunc func(context.Context, int64) (bool, error)

func (f tenantStatusFunc) TenantEnabled(ctx context.Context, id int64) (bool, error) {
	return f(ctx, id)
}

func newLifecycleTenant(t *testing.T) (*tenant.Service, tenant.Tenant) {
	t.Helper()
	service, errNew := tenant.New(filepath.Join(t.TempDir(), "tenant.sqlite"))
	if errNew != nil {
		t.Fatal(errNew)
	}
	t.Cleanup(func() { _ = service.Close() })
	owner, _, errCreate := service.Create(context.Background(), tenant.CreateInput{DisplayName: "owner"})
	if errCreate != nil {
		t.Fatal(errCreate)
	}
	return service, owner
}

func TestTenantKeysRequireCurrentEnabledTenant(t *testing.T) {
	service := newTestService(t)
	tenants, owner := newLifecycleTenant(t)
	ctx := context.Background()
	if _, errCreate := service.CreateTenantKey(ctx, owner.ID, KeyCreate{Name: "missing-resolver"}); errCreate == nil {
		t.Fatal("tenant key creation succeeded without a status resolver")
	}
	service.SetTenantStatusResolver(tenants)
	key, errCreate := service.CreateTenantKey(ctx, owner.ID, KeyCreate{Name: "tenant-key"})
	if errCreate != nil {
		t.Fatal(errCreate)
	}
	assertAuthentication := func(wantAllowed bool) {
		t.Helper()
		result, authErr := authenticateSecret(service, key.Secret)
		if result != nil && result.Release != nil {
			result.Release()
		}
		if (authErr == nil) != wantAllowed {
			t.Fatalf("Authenticate allowed = %v, want %v, error = %v", authErr == nil, wantAllowed, authErr)
		}
	}
	assertAuthentication(true)
	enabled := false
	if _, errUpdate := tenants.Update(ctx, owner.ID, tenant.UpdateInput{Enabled: &enabled}); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	assertAuthentication(false)
	enabled = true
	if _, errUpdate := tenants.Update(ctx, owner.ID, tenant.UpdateInput{Enabled: &enabled}); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	assertAuthentication(true)
	service.SetTenantStatusResolver(nil)
	assertAuthentication(false)
	service.SetTenantStatusResolver(tenantStatusFunc(func(context.Context, int64) (bool, error) {
		return true, errors.New("status store unavailable")
	}))
	assertAuthentication(false)
	service.SetTenantStatusResolver(tenants)
	if errDelete := tenants.Delete(ctx, owner.ID); errDelete != nil {
		t.Fatal(errDelete)
	}
	// The client-access row and snapshot deliberately remain present.
	assertAuthentication(false)
	if _, errCreate := service.CreateTenantGroup(ctx, owner.ID, GroupCreate{Name: "deleted-owner"}); errCreate == nil {
		t.Fatal("a deleted tenant created a group")
	}
	global, errGlobal := service.CreateKey(ctx, KeyCreate{Name: "administrator"})
	if errGlobal != nil {
		t.Fatal(errGlobal)
	}
	result, authErr := authenticateSecret(service, global.Secret)
	if authErr != nil {
		t.Fatalf("tenant status affected administrator key: %v", authErr)
	}
	result.Release()
}

func TestTenantCreateRechecksStatusAfterCrossStoreWrite(t *testing.T) {
	for _, resource := range []string{"key", "group"} {
		t.Run(resource, func(t *testing.T) {
			service := newTestService(t)
			tenants, owner := newLifecycleTenant(t)
			ctx := context.Background()
			written := make(chan struct{})
			resume := make(chan struct{})
			var resumeOnce sync.Once
			defer resumeOnce.Do(func() { close(resume) })
			calls := 0
			service.SetTenantStatusResolver(tenantStatusFunc(func(ctx context.Context, id int64) (bool, error) {
				calls++
				if calls == 2 {
					close(written)
					<-resume
				}
				return tenants.TenantEnabled(ctx, id)
			}))
			completed := make(chan error, 1)
			go func() {
				if resource == "key" {
					_, errCreate := service.CreateTenantKey(ctx, owner.ID, KeyCreate{Name: "in-flight"})
					completed <- errCreate
				} else {
					_, errCreate := service.CreateTenantGroup(ctx, owner.ID, GroupCreate{Name: "in-flight"})
					completed <- errCreate
				}
			}()
			<-written
			enabled := false
			if _, errUpdate := tenants.Update(ctx, owner.ID, tenant.UpdateInput{Enabled: &enabled}); errUpdate != nil {
				t.Fatal(errUpdate)
			}
			if errDelete := tenants.Delete(ctx, owner.ID); errDelete != nil {
				t.Fatal(errDelete)
			}
			resumeOnce.Do(func() { close(resume) })
			if errCreate := <-completed; errCreate == nil {
				t.Fatal("in-flight creation succeeded after tenant deletion")
			}
			keys, errKeys := service.ListTenantKeys(ctx, owner.ID, ListOptions{})
			groups, errGroups := service.ListTenantGroups(ctx, owner.ID, ListOptions{})
			if errKeys != nil || errGroups != nil || keys.Total != 0 || groups.Total != 0 {
				t.Fatalf("orphan resources remain: keys=%d groups=%d errors=%v/%v", keys.Total, groups.Total, errKeys, errGroups)
			}
		})
	}
}
