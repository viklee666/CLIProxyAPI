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
