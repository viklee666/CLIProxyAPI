package executor

import (
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// CodexExecutor is a stateless executor for Codex (OpenAI Responses API entrypoint).
// If api_key is unavailable on auth, it falls back to legacy via ClientAdapter.
type CodexExecutor struct {
	cfg *config.Config
}

func NewCodexExecutor(cfg *config.Config) *CodexExecutor { return &CodexExecutor{cfg: cfg} }

func (e *CodexExecutor) Identifier() string { return "codex" }

func (e *CodexExecutor) modelLevelCooling() bool {
	return e != nil && e.cfg != nil && e.cfg.Codex.ModelLevelCooling
}

// streamBootstrapPolicy protects credential-pool failover without changing config-provider streams.
func (e *CodexExecutor) streamBootstrapPolicy(auth *cliproxyauth.Auth) (enabled, credentialFile bool) {
	switch auth.AuthSourceKind() {
	case cliproxyauth.AuthSourceFile, cliproxyauth.AuthSourceGit, cliproxyauth.AuthSourceObjectStore, cliproxyauth.AuthSourcePostgres:
		credentialFile = true
	}
	return credentialFile || e != nil && e.cfg != nil && e.cfg.Codex.StreamBootstrapBuffering, credentialFile
}

// SupportsApplyPatch reports the actual executor contract, independent of its provider name.
func (e *CodexExecutor) SupportsApplyPatch() bool { return e != nil }
