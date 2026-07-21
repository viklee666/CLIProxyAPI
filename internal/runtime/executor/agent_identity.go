package executor

import (
	"context"
	"crypto/ed25519"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

// agentAssertionScheme is the Authorization scheme used by Codex agent identity auths.
const (
	agentAssertionScheme                 = "AgentAssertion"
	agentIdentityTaskRegistrationTimeout = 30 * time.Second
)

var agentIdentityAuthBaseURL = "https://auth.openai.com/api/accounts"

// agentAssertion is the signed identity envelope carried in the Authorization header.
type agentAssertion struct {
	AgentRuntimeID string `json:"agent_runtime_id"`
	TaskID         string `json:"task_id"`
	Timestamp      string `json:"timestamp"`
	Signature      string `json:"signature"`
}

// agentIdentityCreds holds agent identity credential material extracted from auth metadata.
type agentIdentityCreds struct {
	runtimeID     string
	privateKeyB64 string
	taskID        string
	accountID     string
}

type agentIdentityTaskRegistrationResponse struct {
	TaskID               string `json:"task_id"`
	TaskIDCamel          string `json:"taskId"`
	EncryptedTaskID      string `json:"encrypted_task_id"`
	EncryptedTaskIDCamel string `json:"encryptedTaskId"`
}

func agentIdentityMetadataString(auth *cliproxyauth.Auth, keys ...string) string {
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	for _, key := range keys {
		if v, ok := auth.Metadata[key].(string); ok {
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

// agentIdentityCredsFromAuth extracts agent identity credentials from auth metadata.
// Canonical keys are agent_runtime_id, agent_private_key and task_id; legacy
// private_key_pkcs8_base64 and private_key spellings are accepted as aliases.
func agentIdentityCredsFromAuth(auth *cliproxyauth.Auth) agentIdentityCreds {
	return agentIdentityCreds{
		runtimeID:     agentIdentityMetadataString(auth, "agent_runtime_id"),
		privateKeyB64: agentIdentityMetadataString(auth, "agent_private_key", "private_key_pkcs8_base64", "private_key"),
		taskID:        agentIdentityMetadataString(auth, "task_id"),
		accountID:     agentIdentityMetadataString(auth, "account_id", "chatgpt_account_id"),
	}
}

// isAgentIdentityAuth reports whether the auth carries agent identity credentials.
// Delegates to the shared classifier so executor and auth manager agree.
func isAgentIdentityAuth(auth *cliproxyauth.Auth) bool {
	return cliproxyauth.IsAgentIdentityAuth(auth)
}

// agentIdentityAccountID returns the ChatGPT account id associated with an agent identity auth.
func agentIdentityAccountID(auth *cliproxyauth.Auth) string {
	return agentIdentityCredsFromAuth(auth).accountID
}

// agentIdentityPrivateKey decodes the base64 PKCS#8 DER private key into an Ed25519 key.
// Metadata often contains whitespace/newlines or omits padding, so decoding accepts both
// standard and raw base64 after stripping whitespace.
func agentIdentityPrivateKey(privateKeyB64 string) (ed25519.PrivateKey, error) {
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '	':
			return -1
		default:
			return r
		}
	}, privateKeyB64)
	der, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		var errRaw error
		der, errRaw = base64.RawStdEncoding.DecodeString(cleaned)
		if errRaw != nil {
			return nil, fmt.Errorf("agent identity auth: decode private key: %w", err)
		}
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("agent identity auth: parse private key: %w", err)
	}
	privateKey, ok := parsedKey.(ed25519.PrivateKey)
	if !ok || len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("agent identity auth: private key is not ed25519")
	}
	return privateKey, nil
}

// buildAgentAssertion signs "<agent_runtime_id>:<task_id>:<timestamp>" with the Ed25519
// private key and returns the Authorization header value
// "AgentAssertion <base64url(JSON envelope)>".
func buildAgentAssertion(creds agentIdentityCreds, now time.Time) (string, error) {
	if creds.runtimeID == "" || creds.privateKeyB64 == "" || creds.taskID == "" {
		return "", fmt.Errorf("agent identity auth: missing agent_runtime_id, agent_private_key or task_id")
	}
	privateKey, err := agentIdentityPrivateKey(creds.privateKeyB64)
	if err != nil {
		return "", err
	}
	timestamp := now.UTC().Format(time.RFC3339)
	payload := creds.runtimeID + ":" + creds.taskID + ":" + timestamp
	signature := ed25519.Sign(privateKey, []byte(payload))
	envelope, err := json.Marshal(agentAssertion{
		AgentRuntimeID: creds.runtimeID,
		TaskID:         creds.taskID,
		Timestamp:      timestamp,
		Signature:      base64.StdEncoding.EncodeToString(signature),
	})
	if err != nil {
		return "", fmt.Errorf("agent identity auth: marshal assertion: %w", err)
	}
	return agentAssertionScheme + " " + base64.RawURLEncoding.EncodeToString(envelope), nil
}

// generateAgentAssertion builds a fresh Authorization header value for the auth.
func generateAgentAssertion(auth *cliproxyauth.Auth) (string, error) {
	return buildAgentAssertion(agentIdentityCredsFromAuth(auth), time.Now())
}

func signAgentTaskRegistration(creds agentIdentityCreds, now time.Time) (string, string, error) {
	if creds.runtimeID == "" || creds.privateKeyB64 == "" {
		return "", "", fmt.Errorf("agent identity auth: missing agent_runtime_id or agent_private_key")
	}
	privateKey, err := agentIdentityPrivateKey(creds.privateKeyB64)
	if err != nil {
		return "", "", err
	}
	timestamp := now.UTC().Format(time.RFC3339)
	signature := ed25519.Sign(privateKey, []byte(creds.runtimeID+":"+timestamp))
	return timestamp, base64.StdEncoding.EncodeToString(signature), nil
}

func decryptAgentTaskID(creds agentIdentityCreds, encoded string) (string, error) {
	privateKey, err := agentIdentityPrivateKey(creds.privateKeyB64)
	if err != nil {
		return "", err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", fmt.Errorf("agent identity auth: encrypted task id is not valid base64")
	}
	digest := sha512.Sum512(privateKey.Seed())
	var curvePrivate [32]byte
	copy(curvePrivate[:], digest[:32])
	curvePrivate[0] &= 248
	curvePrivate[31] &= 127
	curvePrivate[31] |= 64
	curvePublicBytes, err := curve25519.X25519(curvePrivate[:], curve25519.Basepoint)
	if err != nil {
		return "", fmt.Errorf("agent identity auth: derive task decryption key: %w", err)
	}
	var curvePublic [32]byte
	copy(curvePublic[:], curvePublicBytes)
	plaintext, ok := box.OpenAnonymous(nil, ciphertext, &curvePublic, &curvePrivate)
	if !ok {
		return "", fmt.Errorf("agent identity auth: decrypt encrypted task id")
	}
	taskID := strings.TrimSpace(string(plaintext))
	if taskID == "" {
		return "", fmt.Errorf("agent identity auth: decrypted task id is empty")
	}
	return taskID, nil
}

func registerAgentIdentityTask(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth) (string, error) {
	creds := agentIdentityCredsFromAuth(auth)
	timestamp, signature, err := signAgentTaskRegistration(creds, time.Now())
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
	endpoint := strings.TrimRight(strings.TrimSpace(agentIdentityAuthBaseURL), "/") + "/v1/agent/" + url.PathEscape(creds.runtimeID) + "/task/register"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("agent identity auth: build task registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := helps.NewProxyAwareHTTPClient(ctx, cfg, auth, agentIdentityTaskRegistrationTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("agent identity auth: task registration request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
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
	return decryptAgentTaskID(creds, encrypted)
}

func updateAgentIdentityTask(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if auth == nil || !isAgentIdentityAuth(auth) {
		return auth, nil
	}
	taskID, err := registerAgentIdentityTask(ctx, cfg, auth)
	if err != nil {
		return nil, err
	}
	updated := auth.Clone()
	if updated.Metadata == nil {
		updated.Metadata = make(map[string]any)
	}
	updated.Metadata["task_id"] = taskID
	updated.UpdatedAt = time.Now().UTC()
	return updated, nil
}

func isAgentIdentityTaskInvalidError(err error) bool {
	if err == nil {
		return false
	}
	statusErr, ok := err.(interface{ StatusCode() int })
	if !ok || statusErr.StatusCode() != http.StatusUnauthorized {
		return false
	}
	lower := strings.ToLower(err.Error())
	compact := strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "").Replace(lower)
	for _, marker := range []string{
		`"code":"invalid_task_id"`,
		`"code":"task_not_found"`,
		`"code":"task_expired"`,
		`"error":"invalid_task_id"`,
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	for _, marker := range []string{
		"invalid task_id",
		"invalid task id",
		"task_id is invalid",
		"task id is invalid",
		"task not found",
		"task expired",
		"unknown task_id",
		"unknown task id",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (e *CodexExecutor) ShouldPrepareRequestAuth(auth *cliproxyauth.Auth) bool {
	return isAgentIdentityAuth(auth) && agentIdentityMetadataString(auth, "task_id") == ""
}

func (e *CodexExecutor) PrepareRequestAuth(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if !e.ShouldPrepareRequestAuth(auth) {
		return auth, nil
	}
	return updateAgentIdentityTask(ctx, e.cfg, auth)
}

func (e *CodexExecutor) ShouldRecoverRequestAuth(auth *cliproxyauth.Auth, execErr error) bool {
	return isAgentIdentityAuth(auth) && isAgentIdentityTaskInvalidError(execErr)
}

func (e *CodexExecutor) RequestAuthRecoveryState(auth *cliproxyauth.Auth) string {
	return agentIdentityMetadataString(auth, "task_id")
}

func (e *CodexExecutor) RecoverRequestAuth(ctx context.Context, auth *cliproxyauth.Auth, execErr error) (*cliproxyauth.Auth, error) {
	if !e.ShouldRecoverRequestAuth(auth, execErr) {
		return auth, nil
	}
	return updateAgentIdentityTask(ctx, e.cfg, auth)
}

func (e *CodexExecutor) RequestAuthRecovered(auth *cliproxyauth.Auth) {
	if auth == nil {
		return
	}
	CloseCodexWebsocketSessionsForAuthID(auth.ID, "agent_identity_task_recovered")
}
