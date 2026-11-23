package cliproxy

import (
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func TestTenantModelRegistrationRejectsSupersededCatalog(t *testing.T) {
	for _, raw := range []string{`[{"name":"stale-model"}]`, `invalid-json`, `[]`} {
		t.Run(raw, func(t *testing.T) {
			cfg := &config.Config{}
			service := &Service{cfg: cfg}
			auth := &coreauth.Auth{
				ID: t.Name(), Provider: "openai-compatibility",
				Attributes: map[string]string{
					coreauth.AttributeTenantID:      "1",
					coreauth.AttributeTenantChannel: "openai-compat",
					coreauth.AttributeTenantModels:  raw,
				},
			}
			reg := GlobalModelRegistry()
			reg.RegisterClient(auth.ID, auth.Provider, []*ModelInfo{{ID: "old-model"}})
			t.Cleanup(func() { reg.UnregisterClient(auth.ID) })
			fence := newModelRegistrationFence(auth, cfg)
			reg.RegisterClient(auth.ID, auth.Provider, []*ModelInfo{{ID: "current-model"}})
			epoch := reg.ClientRegistrationEpoch(auth.ID)

			handled, published := service.registerTenantModelsForAuth(cfg, auth, fence, auth.Provider, "", nil)
			if !handled || published {
				t.Fatalf("tenant registration = (%v, %v), want handled and rejected", handled, published)
			}
			if reg.ClientRegistrationEpoch(auth.ID) != epoch || !reg.ClientSupportsModel(auth.ID, "current-model") {
				t.Fatal("superseded tenant registration changed the current catalog")
			}
		})
	}
}
