package cliproxy

import (
	"context"
	"testing"
)

// registerBuiltServiceCleanup releases persistent fork stores before TempDir cleanup.
func registerBuiltServiceCleanup(t testing.TB, service *Service) {
	t.Helper()
	if service == nil {
		return
	}
	t.Cleanup(func() {
		if errShutdown := service.Shutdown(context.Background()); errShutdown != nil {
			t.Errorf("shutdown test service: %v", errShutdown)
		}
	})
}
