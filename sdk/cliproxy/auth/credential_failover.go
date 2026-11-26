package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type credentialFailoverContextKey struct{}

// Stored credentials share the file-management surface regardless of storage backend.
func isStoredCredential(auth *Auth) bool {
	switch auth.AuthSourceKind() {
	case AuthSourceFile, AuthSourceGit, AuthSourceObjectStore, AuthSourcePostgres:
		return true
	default:
		return false
	}
}

func credentialFailoverFailures(ctx context.Context) map[string]struct{} {
	if ctx == nil {
		return nil
	}
	failed, _ := ctx.Value(credentialFailoverContextKey{}).(map[string]struct{})
	return failed
}

func credentialFailoverActive(ctx context.Context) bool {
	return len(credentialFailoverFailures(ctx)) > 0
}

// Activate only after a retryable credential failure, never after local admission,
// request policy rejection, cancellation, or an explicitly pinned execution.
func withCredentialFailover(ctx context.Context, auth *Auth, opts cliproxyexecutor.Options, err error) context.Context {
	if !isStoredCredential(auth) || pinnedAuthIDFromMetadata(opts.Metadata) != "" ||
		cliproxyexecutor.RequestRetriesDisabled(ctx) || err == nil || (ctx != nil && ctx.Err() != nil) ||
		isTerminalRequestStopError(err) || isCredentialTerminatedError(err) || isTOSViolationError(err) || isRequestInvalidError(err) ||
		(!isRequestRetryRoundError(err) && statusCodeFromError(err) != http.StatusUnauthorized && !isModelSupportError(err)) {
		return ctx
	}
	var localLimit *requestLimitError
	if errors.As(err, &localLimit) {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	failed := credentialFailoverFailures(ctx)
	if failed == nil {
		failed = make(map[string]struct{})
		ctx = context.WithValue(ctx, credentialFailoverContextKey{}, failed)
	}
	failed[auth.ID] = struct{}{}
	return ctx
}

// A completed credential sweep must not restart the same identities in an outer
// retry round. Keep the upstream status/body/headers through the existing wrapper.
func credentialFailoverTerminalError(ctx context.Context, err error) error {
	if credentialFailoverActive(ctx) {
		return wrapRequestStopError(err)
	}
	return err
}

// Exclude unavailable stored credentials before choosing a tier. Configured
// provider entries retain the strict health gate; local RPM never unlocks tiers.
func (m *Manager) credentialCandidatesForSelection(ctx context.Context, auths []*Auth, model string, metadata map[string]any, now time.Time, headers ...http.Header) []*Auth {
	if pinnedAuthIDFromMetadata(metadata) != "" {
		return auths
	}
	var filtered []*Auth
	failed := credentialFailoverFailures(ctx)
	for i, candidate := range auths {
		skip := false
		if isStoredCredential(candidate) {
			_, skip = failed[candidate.ID]
			if !skip {
				checkModel := m.selectionModelForAuth(candidate, model)
				skip, _, _ = isAuthBlockedForModelForRequest(ctx, candidate, checkModel, now, headers...)
			}
		}
		if skip && filtered == nil {
			filtered = make([]*Auth, 0, len(auths)-1)
			filtered = append(filtered, auths[:i]...)
		} else if !skip && filtered != nil {
			filtered = append(filtered, candidate)
		}
	}
	if len(filtered) > 0 || (filtered != nil && credentialFailoverActive(ctx)) {
		return filtered
	}
	return auths
}
