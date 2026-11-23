package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MoeForge/moego/json"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

const agentIdentityTaskRegistrationTimeout = 30 * time.Second

var agentIdentityAuthBaseURL = "https://auth.openai.com/api/accounts"

type agentIdentityTaskRegistrationResponse struct {
	TaskID               string `json:"task_id"`
	TaskIDCamel          string `json:"taskId"`
	EncryptedTaskID      string `json:"encrypted_task_id"`
	EncryptedTaskIDCamel string `json:"encryptedTaskId"`
}

func (e *CodexExecutor) registerAgentIdentityTask(ctx context.Context, auth *cliproxyauth.Auth) (string, error) {
	timestamp, signature, err := helps.SignAgentTaskRegistration(auth, time.Now())
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(map[string]string{
		"timestamp": timestamp,
		"signature": signature,
	})
	if err != nil {
		return "", fmt.Errorf("agent identity auth: marshal task registration: %w", err)
	}
	endpoint := strings.TrimRight(strings.TrimSpace(agentIdentityAuthBaseURL), "/") + "/v1/agent/" + url.PathEscape(strings.TrimSpace(auth.ReadMetadataString("agent_runtime_id"))) + "/task/register"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("agent identity auth: build task registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, agentIdentityTaskRegistrationTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("agent identity auth: task registration request failed: %w", err)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("codex executor: close task registration response: %v", errClose)
		}
	}()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("agent identity auth: task registration returned status %d", resp.StatusCode)
	}
	var result agentIdentityTaskRegistrationResponse
	if errDecode := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&result); errDecode != nil {
		return "", fmt.Errorf("agent identity auth: invalid task registration response: %w", errDecode)
	}
	if taskID := strings.TrimSpace(result.TaskID); taskID != "" {
		return taskID, nil
	}
	if taskID := strings.TrimSpace(result.TaskIDCamel); taskID != "" {
		return taskID, nil
	}
	encrypted := strings.TrimSpace(result.EncryptedTaskID)
	if encrypted == "" {
		encrypted = strings.TrimSpace(result.EncryptedTaskIDCamel)
	}
	if encrypted == "" {
		return "", fmt.Errorf("agent identity auth: task registration response omitted task id")
	}
	return helps.DecryptAgentTaskID(auth, encrypted)
}

func (e *CodexExecutor) updateAgentIdentityTask(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if auth == nil || !cliproxyauth.IsAgentIdentityAuth(auth) {
		return auth, nil
	}
	taskID, err := e.registerAgentIdentityTask(ctx, auth)
	if err != nil {
		return nil, err
	}
	updated := auth.Clone()
	updated.StoreMetadataString("task_id", taskID)
	updated.UpdatedAt = time.Now().UTC()
	return updated, nil
}

func (e *CodexExecutor) ShouldPrepareRequestAuth(auth *cliproxyauth.Auth) bool {
	return cliproxyauth.IsAgentIdentityAuth(auth) && strings.TrimSpace(auth.ReadMetadataString("task_id")) == ""
}

func (e *CodexExecutor) PrepareRequestAuth(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if !e.ShouldPrepareRequestAuth(auth) {
		return auth, nil
	}
	return e.updateAgentIdentityTask(ctx, auth)
}

func (e *CodexExecutor) ShouldRecoverRequestAuth(auth *cliproxyauth.Auth, execErr error) bool {
	return cliproxyauth.IsAgentIdentityAuth(auth) && helps.IsAgentIdentityTaskInvalidError(execErr)
}

func (e *CodexExecutor) RequestAuthRecoveryState(auth *cliproxyauth.Auth) string {
	return strings.TrimSpace(auth.ReadMetadataString("task_id"))
}

func (e *CodexExecutor) RecoverRequestAuth(ctx context.Context, auth *cliproxyauth.Auth, execErr error) (*cliproxyauth.Auth, error) {
	if !e.ShouldRecoverRequestAuth(auth, execErr) {
		return auth, nil
	}
	return e.updateAgentIdentityTask(ctx, auth)
}

func (e *CodexExecutor) RequestAuthRecovered(auth *cliproxyauth.Auth) {
	if auth == nil {
		return
	}
	CloseCodexWebsocketSessionsForAuthID(auth.ID, "agent_identity_task_recovered")
}
