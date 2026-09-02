package executor

import (
	"context"
	"fmt"
	"strings"
	"time"

	claudeauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/claude"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

const (
	claudeAccountProfileCheckedAtKey = "claude_account_profile_checked_at"
	claudeAccountProfileTimeout      = 10 * time.Second
)

type claudeOAuthProfileFetcher func(context.Context, *cliproxyauth.Auth, string) (*claudeauth.OAuthProfile, error)

func (e *ClaudeExecutor) ShouldPrepareRequestAuth(auth *cliproxyauth.Auth) bool {
	apiKey, _ := claudeCreds(auth)
	if !isClaudeOAuthToken(apiKey) || auth == nil {
		return false
	}
	if !claudeauth.HasCanonicalDeviceIDPool(helps.ClaudeDeviceIDPool(auth)) {
		return true
	}
	return helps.ClaudeCredentialAccountUUID(auth) == ""
}

func isClaudeSetupToken(auth *cliproxyauth.Auth, apiKey string) bool {
	if !isClaudeOAuthToken(apiKey) || auth == nil {
		return false
	}
	if auth.ReadMetadataBool("skip_account_profile") ||
		auth.ReadMetadataBool("is_setup_token") ||
		auth.ReadMetadataBool("setup_token") {
		return true
	}
	if kind := strings.ToLower(strings.TrimSpace(auth.ReadAttribute("auth_kind"))); kind == "setup_token" || kind == "setup-token" {
		return true
	}
	scopes := strings.ToLower(auth.ReadMetadataString("scopes"))
	if scopes == "" {
		scopes = strings.ToLower(auth.ReadMetadataString("scope"))
	}
	if scopes != "" && !strings.Contains(scopes, "user:profile") && !strings.Contains(scopes, "user:office") {
		return true
	}
	return false
}

func isClaudeOAuthScope403(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "status 403") ||
		strings.Contains(msg, "403 forbidden") ||
		strings.Contains(msg, "403") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "permission_error") ||
		strings.Contains(msg, "scope requirement") ||
		strings.Contains(msg, "insufficient_scope") ||
		strings.Contains(msg, "user:profile") ||
		strings.Contains(msg, "user:office")
}

func (e *ClaudeExecutor) PrepareRequestAuth(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if auth == nil || !e.ShouldPrepareRequestAuth(auth) {
		return auth, nil
	}
	apiKey, _ := claudeCreds(auth)
	if _, errDeviceIDs := helps.EnsureClaudeCredentialDevicePoolRequired(ctx, auth); errDeviceIDs != nil {
		return nil, errDeviceIDs
	}
	if helps.ClaudeCredentialAccountUUID(auth) != "" {
		return auth, nil
	}

	if isClaudeSetupToken(auth, apiKey) {
		seed := helps.ClaudeCLIAuthIdentitySeed(auth)
		if seed == "" {
			seed = "claude-setup-token|" + apiKey
		}
		storeClaudeAccountUUIDFallback(auth, helps.StableClaudeCLIAccountUUID(seed))
		return auth, nil
	}

	profile, errProfile := e.fetchClaudeOAuthProfile(ctx, auth, apiKey)
	if errProfile != nil {
		if errContext := ctx.Err(); errContext != nil {
			return nil, errContext
		}
		if isClaudeOAuthScope403(errProfile) {
			log.Debugf("Claude OAuth account profile lookup returned 403 for auth %s: %v (falling back to stable credential identity)", auth.ID, errProfile)
		} else {
			log.Debugf("Claude OAuth account profile lookup failed for auth %s: %v (falling back to stable credential identity)", auth.ID, errProfile)
		}
		storeClaudeOAuthAccountUUIDFallback(auth, apiKey)
		return auth, nil
	}
	if profile == nil || strings.TrimSpace(profile.Account.UUID) == "" {
		log.Debugf("Claude OAuth account profile lookup returned empty account UUID for auth %s (falling back to stable credential identity)", auth.ID)
		storeClaudeOAuthAccountUUIDFallback(auth, apiKey)
		return auth, nil
	}
	checkedAt := time.Now().UTC().Format(time.RFC3339)
	auth.MutateMetadata(func(meta map[string]any) {
		claudeauth.StoreMetadataStringIn(meta, "account_uuid", profile.Account.UUID)
		claudeauth.StoreMetadataStringIn(meta, "email", profile.Account.Email)
		claudeauth.StoreMetadataStringIn(meta, "organization_uuid", profile.Organization.UUID)
		claudeauth.StoreMetadataStringIn(meta, "organization_name", profile.Organization.Name)
		claudeauth.StoreMetadataStringIn(meta, claudeAccountProfileCheckedAtKey, checkedAt)
	})
	return auth, nil
}

func storeClaudeOAuthAccountUUIDFallback(auth *cliproxyauth.Auth, apiKey string) {
	seed := helps.ClaudeCLIAuthIdentitySeed(auth)
	if seed == "" {
		seed = "claude-oauth-fallback|" + apiKey
	}
	storeClaudeAccountUUIDFallback(auth, helps.StableClaudeCLIAccountUUID(seed))
}

func storeClaudeAccountUUIDFallback(auth *cliproxyauth.Auth, accountUUID string) {
	if auth == nil {
		return
	}
	checkedAt := time.Now().UTC().Format(time.RFC3339)
	auth.MutateMetadata(func(meta map[string]any) {
		claudeauth.StoreMetadataStringIn(meta, "account_uuid", accountUUID)
		claudeauth.StoreMetadataStringIn(meta, claudeAccountProfileCheckedAtKey, checkedAt)
	})
}

func (e *ClaudeExecutor) fetchClaudeOAuthProfile(ctx context.Context, auth *cliproxyauth.Auth, apiKey string) (*claudeauth.OAuthProfile, error) {
	if e == nil {
		return nil, fmt.Errorf("fetch Claude OAuth profile: executor is nil")
	}
	if e.oauthProfileFetcher != nil {
		return e.oauthProfileFetcher(ctx, auth, apiKey)
	}
	if auth == nil {
		return nil, fmt.Errorf("fetch Claude OAuth profile: auth is nil")
	}
	profileCtx, cancelProfile := context.WithTimeout(ctx, claudeAccountProfileTimeout)
	defer cancelProfile()
	service := claudeauth.NewClaudeAuthWithProxyURL(e.cfg, auth.ProxyURL)
	return service.FetchOAuthProfile(profileCtx, apiKey)
}

func (e *ClaudeExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	log.Debugf("claude executor: refresh called")
	if refreshed, handled, err := helps.RefreshAuthViaHome(ctx, e.cfg, auth); handled {
		return refreshed, err
	}
	if auth == nil {
		return nil, fmt.Errorf("claude executor: auth is nil")
	}
	refreshToken := auth.ReadMetadataString("refresh_token")
	if refreshToken == "" {
		refreshToken = auth.ReadMetadataString("refreshToken")
	}
	if refreshToken == "" {
		return auth, nil
	}
	svc := claudeauth.NewClaudeAuthWithProxyURL(e.cfg, auth.ProxyURL)
	td, err := svc.RefreshTokensWithRetry(ctx, refreshToken, 3)
	if err != nil {
		return nil, err
	}
	lastRefresh := time.Now().Format(time.RFC3339)
	auth.MutateMetadata(func(meta map[string]any) {
		claudeauth.StoreMetadataValueIn(meta, "access_token", td.AccessToken)
		claudeauth.StoreMetadataStringIn(meta, "refresh_token", td.RefreshToken)
		// Profile fields are optional when token rotation succeeds but the follow-up
		// profile lookup fails. Never erase the previously resolved credential identity.
		claudeauth.StoreMetadataStringIn(meta, "email", td.Email)
		claudeauth.StoreMetadataStringIn(meta, "account_uuid", td.AccountUUID)
		claudeauth.StoreMetadataStringIn(meta, "organization_uuid", td.OrganizationUUID)
		claudeauth.StoreMetadataStringIn(meta, "organization_name", td.OrganizationName)
		claudeauth.StoreMetadataValueIn(meta, "expired", td.Expire)
		claudeauth.StoreMetadataValueIn(meta, "type", "claude")
		claudeauth.StoreMetadataValueIn(meta, "last_refresh", lastRefresh)
	})
	return auth, nil
}
