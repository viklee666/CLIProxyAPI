package tenant

import (
	"context"
	"testing"
)

func TestLegacyMalformedProviderCannotBlockAnotherTenant(t *testing.T) {
	for _, invalidField := range []string{"models", "extra", "secret"} {
		t.Run(invalidField, func(t *testing.T) {
			service, first := newSafetyService(t)
			ctx := context.Background()
			second, _, errTenant := service.Create(ctx, CreateInput{DisplayName: "other-tenant"})
			if errTenant != nil {
				t.Fatal(errTenant)
			}
			broken, errBroken := service.CreateProvider(ctx, first.ID, ProviderCreateInput{Channel: ChannelClaude, Name: "legacy", APIKey: "synthetic-key"})
			if errBroken != nil {
				t.Fatal(errBroken)
			}
			if _, errHealthy := service.CreateProvider(ctx, second.ID, ProviderCreateInput{Channel: ChannelClaude, Name: "healthy", APIKey: "synthetic-key"}); errHealthy != nil {
				t.Fatal(errHealthy)
			}
			if _, errSync := service.SynthesizeAuths(ctx); errSync != nil {
				t.Fatal(errSync)
			}
			stored, errGet := service.store.getProvider(ctx, first.ID, broken.ID)
			if errGet != nil {
				t.Fatal(errGet)
			}
			oldIndex := stored.AuthIndex
			// Simulate a row written by a vulnerable older version, bypassing the
			// new service input validation without accessing any real database.
			switch invalidField {
			case "models":
				stored.modelsJSON = `{}`
			case "extra":
				stored.extraJSON = `{"websockets":"invalid"}`
			case "secret":
				stored.apiKeyEnc = "tenant:v1:AA:AA"
			}
			if _, errStore := service.store.updateProvider(ctx, stored); errStore != nil {
				t.Fatal(errStore)
			}
			auths, errSync := service.SynthesizeAuths(ctx)
			if errSync != nil {
				t.Fatalf("a malformed legacy provider blocked other tenants: %v", errSync)
			}
			if len(auths) != 1 || service.OwnerOf(auths[0].Index) != second.ID {
				t.Fatalf("expected only the other tenant's healthy provider; got %d", len(auths))
			}
			if service.OwnerOf(oldIndex) != 0 {
				t.Fatal("malformed provider retained runtime authorization")
			}
		})
	}
}
