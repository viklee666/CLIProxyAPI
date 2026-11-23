package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MoeForge/moego/json"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/codex"
)

func TestPersistCodexAuthFileNormalizesNewCredentialMetadata(t *testing.T) {
	for _, mode := range []string{"metadata", "storage", "refresh-patch"} {
		t.Run(mode, func(t *testing.T) {
			auth := &Auth{
				Provider: "codex",
				Metadata: map[string]any{
					"type": "codex", "access_token": "new-token",
					"disable-cooling": true, "request-retry": 2, "request_retry": 0,
					"concurrency-limit": 3, "rpm-limit": 5,
				},
			}
			switch mode {
			case "storage":
				auth.Storage = &codex.CodexTokenStorage{AccessToken: "new-token"}
			case "refresh-patch":
				auth.CodexRefreshPatch = codex.NewCredentialRefreshPatch(map[string]any{"access_token": "old-token"}, auth.Metadata, true, false)
			}
			path := filepath.Join(t.TempDir(), "codex.json")
			if handled, errPersist := PersistCodexAuthFile(t.Context(), path, auth); !handled || errPersist != nil {
				t.Fatalf("PersistCodexAuthFile() = %t, %v", handled, errPersist)
			}
			raw, errRead := os.ReadFile(path)
			if errRead != nil {
				t.Fatal(errRead)
			}
			var saved map[string]any
			if errUnmarshal := json.Unmarshal(raw, &saved); errUnmarshal != nil {
				t.Fatal(errUnmarshal)
			}
			for _, key := range []string{"disable-cooling", "request-retry", "concurrency-limit", "rpm-limit"} {
				if _, exists := saved[key]; exists {
					t.Errorf("legacy key %q persisted", key)
				}
			}
			if saved["disable_cooling"] != true || saved["request_retry"] != float64(0) || saved["concurrency_limit"] != float64(3) || saved["rpm_limit"] != float64(5) {
				t.Fatalf("canonical metadata lost or overridden: %#v", saved)
			}
		})
	}
}
