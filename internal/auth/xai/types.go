// Package xai provides OAuth2 authentication helpers for xAI Grok.
package xai

import (
	"strings"
	"time"
)

const (
	// DefaultAPIBaseURL is the default official xAI API base URL.
	// Used for OAuth credential defaults, websocket, compact, and media
	// (image/video). HTTP chat uses this when auth using_api is true or non-OAuth.
	DefaultAPIBaseURL = "https://api.x.ai/v1"
	// CLIChatProxyBaseURL is the Grok CLI chat-proxy base URL for HTTP chat/stream
	// when auth using_api is false, including the OAuth default.
	// Image/video must not use this host; they stay on DefaultAPIBaseURL.
	CLIChatProxyBaseURL = "https://cli-chat-proxy.grok.com/v1"
	// Issuer is xAI's OAuth issuer.
	Issuer = "https://auth.x.ai"
	// DiscoveryURL is the OIDC discovery endpoint used to resolve OAuth endpoints.
	DiscoveryURL = Issuer + "/.well-known/openid-configuration"
	// ClientID is the public xAI Grok CLI OAuth client ID.
	ClientID = "b1a00492-073a-47ea-816f-4c329264a828"
	// Scope is the OAuth scope set required for xAI API access.
	Scope = "openid profile email offline_access grok-cli:access api:access"
	// DeviceCodeGrantType is the OAuth2 device authorization grant type (RFC 8628).
	DeviceCodeGrantType = "urn:ietf:params:oauth:grant-type:device_code"
	// defaultPollInterval is used when the device endpoint omits interval.
	defaultPollInterval = 5 * time.Second
	// httpClientTimeout bounds credential-acquisition HTTP calls (device/token/refresh).
	httpClientTimeout = 30 * time.Second
	// MaxPollDuration is the upper bound for waiting on user authorization.
	MaxPollDuration = 30 * time.Minute
)

var refreshLead = 5 * time.Minute

// RefreshLead returns the refresh lead time for xAI OAuth credentials.
func RefreshLead() time.Duration {
	return refreshLead
}

// Discovery contains OAuth endpoints resolved from xAI OIDC discovery.
type Discovery struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

// DeviceCodeResponse represents xAI's device authorization response.
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
	TokenEndpoint           string `json:"-"`
}

// TokenData holds xAI OAuth token data.
type TokenData struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	Expire       string `json:"expired,omitempty"`
	Email        string `json:"email,omitempty"`
	Subject      string `json:"sub,omitempty"`
}

// AuthBundle aggregates token data and OAuth metadata for persistence.
type AuthBundle struct {
	TokenData     TokenData
	LastRefresh   string
	BaseURL       string
	RedirectURI   string
	TokenEndpoint string
}

func normalizeBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func isDefaultAPIBaseURL(baseURL string) bool {
	return normalizeBaseURL(baseURL) == normalizeBaseURL(DefaultAPIBaseURL)
}

func isCLIChatProxyBaseURL(baseURL string) bool {
	return normalizeBaseURL(baseURL) == normalizeBaseURL(CLIChatProxyBaseURL)
}

// SelectChatBaseURL returns the base URL for xAI HTTP chat/stream requests.
// When usingAPI is false (including the OAuth default), an empty or official
// default base_url is rewritten to the CLI chat-proxy. An explicit non-default
// base_url is still honored.
func SelectChatBaseURL(baseURL string, usingAPI bool) string {
	baseURL = strings.TrimSpace(baseURL)
	if usingAPI {
		if baseURL == "" {
			return DefaultAPIBaseURL
		}
		return baseURL
	}
	if baseURL != "" && !isDefaultAPIBaseURL(baseURL) {
		return baseURL
	}
	return CLIChatProxyBaseURL
}

// SelectMediaBaseURL returns the base URL for xAI image/video requests.
// Media stays on the official API host (or an explicit custom base_url) even
// when usingAPI is false. The CLI chat-proxy does not serve image/video
// endpoints, so an empty, official-default, or CLI-proxy base_url is pinned
// to DefaultAPIBaseURL.
func SelectMediaBaseURL(baseURL string, usingAPI bool) string {
	baseURL = strings.TrimSpace(baseURL)
	if usingAPI {
		if baseURL == "" {
			return DefaultAPIBaseURL
		}
		if isCLIChatProxyBaseURL(baseURL) {
			return DefaultAPIBaseURL
		}
		return baseURL
	}
	if baseURL == "" || isDefaultAPIBaseURL(baseURL) || isCLIChatProxyBaseURL(baseURL) {
		return DefaultAPIBaseURL
	}
	return baseURL
}
