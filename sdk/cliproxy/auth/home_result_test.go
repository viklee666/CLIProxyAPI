package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

type homeUnauthorizedUsageCapture struct {
	authID  string
	records chan coreusage.Record
}

func (p *homeUnauthorizedUsageCapture) HandleUsage(_ context.Context, record coreusage.Record) {
	if p == nil || record.ExecutorType != homeResultExecutorType || record.AuthID != p.authID {
		return
	}
	select {
	case p.records <- record:
	default:
	}
}

type noopHomeUnauthorizedUsagePlugin struct{}

func (noopHomeUnauthorizedUsagePlugin) HandleUsage(context.Context, coreusage.Record) {}

func registerHomeUnauthorizedUsageCapture(t *testing.T, authID string) *homeUnauthorizedUsageCapture {
	t.Helper()
	capture := &homeUnauthorizedUsageCapture{authID: authID, records: make(chan coreusage.Record, 1)}
	coreusage.RegisterNamedPlugin(t.Name(), capture)
	t.Cleanup(func() {
		coreusage.RegisterNamedPlugin(t.Name(), noopHomeUnauthorizedUsagePlugin{})
	})
	return capture
}

func TestReportHomeUnauthorizedCopiesClientReservationID(t *testing.T) {
	const (
		authID        = "home-auth-1"
		reservationID = "car_home_401"
	)
	capture := registerHomeUnauthorizedUsageCapture(t, authID)

	auth := &Auth{
		ID:       authID,
		Index:    "idx-home-1",
		Provider: "codex",
		Metadata: map[string]any{"access_token": "home-token"},
	}
	ctx := coreusage.WithClientReservationID(context.Background(), reservationID)
	(&Manager{}).ReportHomeUnauthorized(ctx, auth, "codex", "gpt-5.4", []byte("invalid token"))

	select {
	case record := <-capture.records:
		if record.ClientReservationID != reservationID {
			t.Fatalf("ClientReservationID = %q, want %q", record.ClientReservationID, reservationID)
		}
		if record.AccessTokenSHA256 != AccessTokenSHA256(auth) {
			t.Fatalf("AccessTokenSHA256 = %q, want %q", record.AccessTokenSHA256, AccessTokenSHA256(auth))
		}
		if !record.Failed {
			t.Fatal("Failed = false, want true")
		}
		if record.Fail.StatusCode != http.StatusUnauthorized {
			t.Fatalf("Fail.StatusCode = %d, want %d", record.Fail.StatusCode, http.StatusUnauthorized)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Home unauthorized usage record")
	}
}
