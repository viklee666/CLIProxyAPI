package tenant

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
)

func newSafetyService(t *testing.T) (*Service, Tenant) {
	t.Helper()
	service, errNew := New(filepath.Join(t.TempDir(), "tenant.sqlite"))
	if errNew != nil {
		t.Fatal(errNew)
	}
	t.Cleanup(func() { _ = service.Close() })
	owner, _, errCreate := service.Create(context.Background(), CreateInput{DisplayName: "owner"})
	if errCreate != nil {
		t.Fatal(errCreate)
	}
	return service, owner
}

func TestProviderRejectsInvalidConfigurationBeforePersist(t *testing.T) {
	service, owner := newSafetyService(t)
	ctx := context.Background()
	for _, channel := range []string{ChannelClaude, ChannelCodex, ChannelGemini, ChannelXAI, ChannelOpenAICompat, ChannelVertex} {
		t.Run(channel, func(t *testing.T) {
			input := ProviderCreateInput{Channel: channel, Name: channel, APIKey: "test-only-key", Models: json.RawMessage(`{}`)}
			if _, errCreate := service.CreateProvider(ctx, owner.ID, input); errCreate == nil {
				t.Fatal("invalid model shape was persisted")
			}
			input.Models = json.RawMessage(`[]`)
			input.Extra = json.RawMessage(`{"websockets":"invalid"}`)
			if _, errCreate := service.CreateProvider(ctx, owner.ID, input); errCreate == nil {
				t.Fatal("invalid extra was persisted")
			}
			input.Extra = json.RawMessage(`{}`)
			provider, errCreate := service.CreateProvider(ctx, owner.ID, input)
			if errCreate != nil {
				t.Fatal(errCreate)
			}
			invalidModels := json.RawMessage(`{"invalid":true}`)
			if _, errUpdate := service.UpdateProvider(ctx, owner.ID, provider.ID, ProviderUpdateInput{Models: &invalidModels}); errUpdate == nil {
				t.Fatal("invalid model update was persisted")
			}
			invalidExtra := json.RawMessage(`{"weight":"invalid"}`)
			if _, errUpdate := service.UpdateProvider(ctx, owner.ID, provider.ID, ProviderUpdateInput{Extra: &invalidExtra}); errUpdate == nil {
				t.Fatal("invalid extra update was persisted")
			}
			stored, errGet := service.GetProvider(ctx, owner.ID, provider.ID)
			if errGet != nil || string(stored.Models) != "[]" || string(stored.Extra) != "{}" {
				t.Fatalf("valid configuration changed after rejected updates: %v", errGet)
			}
		})
	}
	providers, errList := service.ListProviders(ctx, owner.ID)
	if errList != nil || len(providers) != 6 {
		t.Fatalf("provider count = %d, error = %v; invalid creates must not persist", len(providers), errList)
	}
	if _, errSynthesize := service.SynthesizeAuths(ctx); errSynthesize != nil {
		t.Fatalf("rejected input poisoned synthesis: %v", errSynthesize)
	}
}

func TestProviderSecretsNeverTrustCiphertextPrefix(t *testing.T) {
	service, owner := newSafetyService(t)
	ctx := context.Background()
	secret := "tenant:v1:AA:AA"
	provider, errCreate := service.CreateProvider(ctx, owner.ID, ProviderCreateInput{Channel: ChannelClaude, Name: "literal-prefix", APIKey: secret})
	if errCreate != nil {
		t.Fatal(errCreate)
	}
	stored, errGet := service.store.getProvider(ctx, owner.ID, provider.ID)
	if errGet != nil || stored.apiKeyEnc == secret {
		t.Fatalf("ciphertext-prefixed plaintext was trusted: %v", errGet)
	}
	config, errConfig := service.ProviderTestConfig(ctx, owner.ID, provider.ID)
	if errConfig != nil || config.APIKey != secret {
		t.Fatalf("literal API key did not round-trip: %v", errConfig)
	}
	if _, errDecrypt := service.protector.UnprotectString(secret); errDecrypt == nil {
		t.Fatal("invalid nonce length was accepted")
	}
	if _, errSynthesize := service.SynthesizeAuths(ctx); errSynthesize != nil {
		t.Fatal(errSynthesize)
	}
}

func TestProviderDisableRevokesOwnershipBeforeSynchronization(t *testing.T) {
	service, owner := newSafetyService(t)
	ctx := context.Background()
	provider, errCreate := service.CreateProvider(ctx, owner.ID, ProviderCreateInput{Channel: ChannelClaude, Name: "provider", APIKey: "test-only-key"})
	if errCreate != nil {
		t.Fatal(errCreate)
	}
	auths, errSynthesize := service.SynthesizeAuths(ctx)
	if errSynthesize != nil {
		t.Fatal(errSynthesize)
	}
	index := auths[0].Index
	disabled := true
	if _, errUpdate := service.UpdateProvider(ctx, owner.ID, provider.ID, ProviderUpdateInput{Disabled: &disabled}); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	// No synchronization callback has run: even its failure must leave access revoked.
	if service.OwnerOf(index) != 0 {
		t.Fatal("disabled provider retained ownership before synchronization")
	}
	if _, errSynthesize := service.SynthesizeAuths(ctx); errSynthesize != nil {
		t.Fatal(errSynthesize)
	}
	if service.OwnerOf(index) != 0 {
		t.Fatal("synthesis restored disabled provider ownership")
	}
	disabled = false
	if _, errUpdate := service.UpdateProvider(ctx, owner.ID, provider.ID, ProviderUpdateInput{Disabled: &disabled}); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	if _, errSynthesize := service.SynthesizeAuths(ctx); errSynthesize != nil {
		t.Fatal(errSynthesize)
	}
	if service.OwnerOf(index) != owner.ID {
		t.Fatal("re-enabling provider did not restore ownership")
	}
}

func TestTenantDisableAndSynthesisCannotRestoreRevokedOwnership(t *testing.T) {
	service, owner := newSafetyService(t)
	ctx := context.Background()
	if _, errCreate := service.CreateProvider(ctx, owner.ID, ProviderCreateInput{Channel: ChannelClaude, Name: "provider", APIKey: "test-only-key"}); errCreate != nil {
		t.Fatal(errCreate)
	}
	auths, errSynthesize := service.SynthesizeAuths(ctx)
	if errSynthesize != nil {
		t.Fatal(errSynthesize)
	}
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			if _, errSync := service.SynthesizeAuths(ctx); errSync != nil {
				t.Error(errSync)
			}
		}()
	}
	close(start)
	enabled := false
	if _, errUpdate := service.Update(ctx, owner.ID, UpdateInput{Enabled: &enabled}); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	workers.Wait()
	if service.OwnerOf(auths[0].Index) != 0 || service.HasOwnedCredentials() {
		t.Fatal("concurrent synthesis restored disabled tenant ownership")
	}
	if errSet := service.SetCredentialOwnership(ctx, "manual", owner.ID); errSet == nil {
		t.Fatal("ownership was assigned to a disabled tenant")
	}
}
