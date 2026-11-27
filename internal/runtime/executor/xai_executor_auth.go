package executor

import (
	"context"
	"net/http"
	"strings"
	"time"

	xaiauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/xai"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// Refresh refreshes xAI OAuth credentials using the stored refresh token.
func (e *XAIExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	log.Debugf("xai executor: refresh called")
	if refreshed, handled, err := helps.RefreshAuthViaHome(ctx, e.cfg, auth); handled {
		return refreshed, err
	}
	if auth == nil {
		return nil, statusErr{code: http.StatusInternalServerError, msg: "xai executor: auth is nil"}
	}
	refreshToken := auth.ReadMetadataString("refresh_token")
	if refreshToken == "" {
		return auth, nil
	}
	tokenEndpoint := auth.ReadMetadataString("token_endpoint")
	svc := xaiauth.NewXAIAuthWithProxyURL(e.cfg, auth.ProxyURL)
	td, err := svc.RefreshTokens(ctx, refreshToken, tokenEndpoint)
	if err != nil {
		return nil, err
	}
	lastRefresh := time.Now().UTC().Format(time.RFC3339)
	auth.MutateMetadata(func(meta map[string]any) {
		meta["type"] = "xai"
		meta["auth_kind"] = "oauth"
		meta["access_token"] = td.AccessToken
		if td.RefreshToken != "" {
			meta["refresh_token"] = td.RefreshToken
		}
		if td.IDToken != "" {
			meta["id_token"] = td.IDToken
		}
		if td.TokenType != "" {
			meta["token_type"] = td.TokenType
		}
		if td.ExpiresIn > 0 {
			meta["expires_in"] = td.ExpiresIn
		}
		if td.Expire != "" {
			meta["expired"] = td.Expire
		}
		if td.Email != "" {
			meta["email"] = td.Email
		}
		if td.Subject != "" {
			meta["sub"] = td.Subject
		}
		if tokenEndpoint != "" {
			meta["token_endpoint"] = tokenEndpoint
		}
		if xaiMetadataString(meta, "base_url") == "" {
			meta["base_url"] = xaiauth.DefaultAPIBaseURL
		}
		meta["last_refresh"] = lastRefresh
	})
	auth.MutateAttributes(func(attrs map[string]string) {
		attrs["auth_kind"] = "oauth"
		if strings.TrimSpace(attrs["base_url"]) == "" {
			attrs["base_url"] = xaiauth.DefaultAPIBaseURL
		}
	})
	return auth, nil
}
