package helps

import (
	"crypto/ed25519"
	"crypto/sha512"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/json"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

// AgentAssertionScheme is the Authorization scheme used by Codex agent identity auths.
const (
	AgentAssertionScheme = "AgentAssertion"
)

// AgentAssertion is the signed identity envelope carried in the Authorization header.
type AgentAssertion struct {
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

func agentIdentityMetadataString(auth *cliproxyauth.Auth, keys ...string) string {
	if auth == nil {
		return ""
	}
	for _, key := range keys {
		if v := auth.ReadMetadataString(key); v != "" {
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

// AgentIdentityAccountID returns the ChatGPT account id associated with an agent identity auth.
func AgentIdentityAccountID(auth *cliproxyauth.Auth) string {
	return agentIdentityCredsFromAuth(auth).accountID
}

// AgentIdentityPrivateKey decodes the base64 PKCS#8 DER private key into an Ed25519 key.
// Metadata often contains whitespace/newlines or omits padding, so decoding accepts both
// standard and raw base64 after stripping whitespace.
func AgentIdentityPrivateKey(privateKeyB64 string) (ed25519.PrivateKey, error) {
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
func BuildAgentAssertion(auth *cliproxyauth.Auth, now time.Time) (string, error) {
	creds := agentIdentityCredsFromAuth(auth)
	if creds.runtimeID == "" || creds.privateKeyB64 == "" || creds.taskID == "" {
		return "", fmt.Errorf("agent identity auth: missing agent_runtime_id, agent_private_key or task_id")
	}
	privateKey, err := AgentIdentityPrivateKey(creds.privateKeyB64)
	if err != nil {
		return "", err
	}
	timestamp := now.UTC().Format(time.RFC3339)
	payload := creds.runtimeID + ":" + creds.taskID + ":" + timestamp
	signature := ed25519.Sign(privateKey, []byte(payload))
	envelope, err := json.Marshal(AgentAssertion{
		AgentRuntimeID: creds.runtimeID,
		TaskID:         creds.taskID,
		Timestamp:      timestamp,
		Signature:      base64.StdEncoding.EncodeToString(signature),
	})
	if err != nil {
		return "", fmt.Errorf("agent identity auth: marshal assertion: %w", err)
	}
	return AgentAssertionScheme + " " + base64.RawURLEncoding.EncodeToString(envelope), nil
}

// GenerateAgentAssertion builds a fresh Authorization header value for the auth.
func GenerateAgentAssertion(auth *cliproxyauth.Auth) (string, error) {
	return BuildAgentAssertion(auth, time.Now())
}

func SignAgentTaskRegistration(auth *cliproxyauth.Auth, now time.Time) (string, string, error) {
	creds := agentIdentityCredsFromAuth(auth)
	if creds.runtimeID == "" || creds.privateKeyB64 == "" {
		return "", "", fmt.Errorf("agent identity auth: missing agent_runtime_id or agent_private_key")
	}
	privateKey, err := AgentIdentityPrivateKey(creds.privateKeyB64)
	if err != nil {
		return "", "", err
	}
	timestamp := now.UTC().Format(time.RFC3339)
	signature := ed25519.Sign(privateKey, []byte(creds.runtimeID+":"+timestamp))
	return timestamp, base64.StdEncoding.EncodeToString(signature), nil
}

func DecryptAgentTaskID(auth *cliproxyauth.Auth, encoded string) (string, error) {
	creds := agentIdentityCredsFromAuth(auth)
	privateKey, err := AgentIdentityPrivateKey(creds.privateKeyB64)
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

func IsAgentIdentityTaskInvalidError(err error) bool {
	if err == nil {
		return false
	}
	var statusCoder interface{ StatusCode() int }
	if !errors.As(err, &statusCoder) || statusCoder == nil || statusCoder.StatusCode() != http.StatusUnauthorized {
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
