package registry

import "testing"

func TestGetAvailableModelsForClientsScopesCatalogToRequestedClients(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("tenant-a", "openai", []*ModelInfo{
		{ID: "tenant-model", Object: "model", OwnedBy: "tenant-a", Type: "openai"},
		{ID: "shared-model", Object: "model", OwnedBy: "tenant-a", Type: "openai"},
	})
	r.RegisterClient("tenant-b", "openai", []*ModelInfo{
		{ID: "other-model", Object: "model", OwnedBy: "tenant-b", Type: "openai"},
		{ID: "shared-model", Object: "model", OwnedBy: "tenant-b", Type: "openai"},
	})

	models := r.GetAvailableModelsForClients("openai", []string{"tenant-a"})
	if len(models) != 2 {
		t.Fatalf("model count = %d, want 2: %#v", len(models), models)
	}
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		id, _ := model["id"].(string)
		seen[id] = struct{}{}
	}
	if _, ok := seen["tenant-model"]; !ok {
		t.Fatalf("tenant model missing: %#v", models)
	}
	if _, ok := seen["shared-model"]; !ok {
		t.Fatalf("shared tenant model missing: %#v", models)
	}
	if _, ok := seen["other-model"]; ok {
		t.Fatalf("other tenant model leaked: %#v", models)
	}
}

func TestGetAvailableModelInfosForClientsPreservesThinkingAndScopesClients(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("tenant-a", "openai", []*ModelInfo{
		{ID: "tenant-model", Object: "model", OwnedBy: "tenant-a", Thinking: &ThinkingSupport{Levels: []string{"low", "high"}}},
		{ID: "shared-model", Object: "model", OwnedBy: "tenant-a"},
	})
	r.RegisterClient("tenant-b", "openai", []*ModelInfo{
		{ID: "other-model", Object: "model", OwnedBy: "tenant-b", Thinking: &ThinkingSupport{Levels: []string{"high"}}},
	})

	infos := r.GetAvailableModelInfosForClients([]string{"tenant-a"})
	if len(infos) != 2 {
		t.Fatalf("model count = %d, want 2: %#v", len(infos), infos)
	}
	seen := make(map[string]*ModelInfo, len(infos))
	for _, info := range infos {
		seen[info.ID] = info
	}
	tenant := seen["tenant-model"]
	if tenant == nil || tenant.Thinking == nil || len(tenant.Thinking.Levels) != 2 || tenant.Thinking.Levels[1] != "high" {
		t.Fatalf("tenant thinking = %#v", tenant)
	}
	if _, ok := seen["other-model"]; ok {
		t.Fatalf("other tenant model leaked: %#v", infos)
	}
	tenant.Thinking.Levels[0] = "mutated"
	fresh := r.GetAvailableModelInfosForClients([]string{"tenant-a"})
	for _, info := range fresh {
		if info.ID == "tenant-model" && info.Thinking.Levels[0] != "low" {
			t.Fatalf("snapshot was not cloned: %#v", info.Thinking.Levels)
		}
	}
}
