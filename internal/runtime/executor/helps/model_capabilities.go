package helps

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// ApplyRequestThinking routes thinking configuration through the registry lookup
// path. This fork keeps its own credential scheduling, so the upstream
// API-key model binding shortcut is not available here.
func ApplyRequestThinking(body []byte, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, fromFormat, toFormat, provider string) ([]byte, error) {
	return thinking.ApplyThinking(body, req.Model, fromFormat, toFormat, provider)
}
