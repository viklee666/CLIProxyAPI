package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestAntigravityRefreshSignatureGrantMetadata(t *testing.T) {
	for _, tc := range []struct {
		name         string
		retained     string
		bound        bool
		staleBinding bool
	}{
		{name: "first refresh"},
		{name: "bound grant rotation", retained: "original-grant", bound: true},
		{name: "replaced credential", retained: "old-account-grant", staleBinding: true},
		{name: "unbound retained grant", retained: "untrusted-grant"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth := &cliproxyauth.Auth{
				ID:       t.Name(),
				Provider: "antigravity",
				Metadata: map[string]any{
					"access_token":  "old-access",
					"refresh_token": "old-refresh-" + tc.name,
					"project_id":    "shared-project",
				},
			}
			if tc.retained != "" {
				auth.StoreMetadataString(antigravitySignatureGrantScopeMetadataKey, tc.retained)
			}
			if tc.bound {
				auth.StoreMetadataString(antigravitySignatureGrantBindingMetadataKey, antigravitySignatureGrantBinding(auth))
			} else if tc.staleBinding {
				auth.StoreMetadataString(antigravitySignatureGrantBindingMetadataKey, "old-account-binding")
			}

			transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "https://oauth2.googleapis.com/token" {
					return nil, fmt.Errorf("unexpected refresh URL: %s", req.URL)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": {"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`)),
					Request:    req,
				}, nil
			})
			ctx := context.WithValue(t.Context(), "cliproxy.roundtripper", transport)
			exec := NewAntigravityExecutor(&config.Config{})

			var readers sync.WaitGroup
			start := make(chan struct{})
			readers.Go(func() {
				<-start
				for range 1000 {
					_ = auth.Clone()
					_ = antigravitySignatureCacheScope(auth)
					_ = antigravitySignatureGrantIdentity(auth)
					_ = antigravitySignatureGrantBinding(auth)
					_ = antigravityTransportScope(auth)
				}
			})
			close(start)
			updated, errRefresh := exec.refreshTokenWithOptions(ctx, auth, ctx, false)
			readers.Wait()
			if errRefresh != nil {
				t.Fatal(errRefresh)
			}
			if updated.ReadMetadataString("access_token") != "new-access" || updated.ReadMetadataString("refresh_token") != "new-refresh" {
				t.Fatal("refresh did not rotate both tokens")
			}
			wantBinding := antigravityCredentialScope("grant:", "new-refresh\x00new-access")
			if got := updated.ReadMetadataString(antigravitySignatureGrantBindingMetadataKey); got != wantBinding {
				t.Fatalf("grant binding = %q, want refreshed token binding %q", got, wantBinding)
			}
			wantIdentity := wantBinding
			if tc.bound {
				wantIdentity = tc.retained
			}
			if got := antigravitySignatureGrantIdentity(updated); got != wantIdentity {
				t.Fatalf("grant identity = %q, want %q", got, wantIdentity)
			}
		})
	}
}
