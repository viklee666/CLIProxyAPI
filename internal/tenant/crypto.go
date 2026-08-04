package tenant

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	tenantEncryptedPrefix = "tenant:v1:"
	tenantProtectorSalt   = "cpa-manager-plus"
	tenantProtectorInfo   = "tenant-provider-secrets-v1"
	tenantProtectorAAD    = "tenant-provider-secret"
)

type Protector struct {
	key []byte
}

func newProtector(databasePath string) (*Protector, error) {
	rawKey := firstEnvironment("TENANT_DATA_KEY", "CPA_MANAGER_DATA_KEY")
	if rawKey == "" {
		rawKey = firstSecretFile("TENANT_DATA_KEY_FILE", "CPA_MANAGER_DATA_KEY_FILE")
	}
	keyPath := firstEnvironment("TENANT_DATA_KEY_PATH", "CPA_MANAGER_DATA_KEY_PATH")
	if keyPath == "" {
		keyPath = filepath.Join(filepath.Dir(databasePath), "data.key")
	}
	key, _, errLoad := loadOrCreateDataKey(rawKey, keyPath)
	if errLoad != nil {
		return nil, errLoad
	}
	derived, errDerive := hkdf.Key(sha256.New, key, []byte(tenantProtectorSalt), tenantProtectorInfo, 32)
	if errDerive != nil {
		return nil, fmt.Errorf("derive tenant provider encryption key: %w", errDerive)
	}
	return &Protector{key: derived}, nil
}

func (p *Protector) ProtectString(value string) (string, error) {
	if p == nil {
		return "", errors.New("tenant protector is unavailable")
	}
	if value == "" || strings.HasPrefix(value, tenantEncryptedPrefix) {
		return value, nil
	}
	nonce := make([]byte, 12)
	if _, errRead := rand.Read(nonce); errRead != nil {
		return "", fmt.Errorf("generate tenant provider encryption nonce: %w", errRead)
	}
	aead, errAEAD := p.aead()
	if errAEAD != nil {
		return "", errAEAD
	}
	ciphertext := aead.Seal(nil, nonce, []byte(value), []byte(tenantProtectorAAD))
	return tenantEncryptedPrefix + base64.RawStdEncoding.EncodeToString(nonce) + ":" + base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func (p *Protector) UnprotectString(value string) (string, error) {
	if p == nil {
		return "", errors.New("tenant protector is unavailable")
	}
	if value == "" || !strings.HasPrefix(value, tenantEncryptedPrefix) {
		return value, nil
	}
	parts := strings.Split(strings.TrimPrefix(value, tenantEncryptedPrefix), ":")
	if len(parts) != 2 {
		return "", errors.New("invalid tenant provider secret format")
	}
	nonce, errNonce := base64.RawStdEncoding.DecodeString(parts[0])
	if errNonce != nil {
		return "", errors.New("invalid tenant provider secret nonce")
	}
	ciphertext, errCiphertext := base64.RawStdEncoding.DecodeString(parts[1])
	if errCiphertext != nil {
		return "", errors.New("invalid tenant provider secret ciphertext")
	}
	aead, errAEAD := p.aead()
	if errAEAD != nil {
		return "", errAEAD
	}
	plaintext, errOpen := aead.Open(nil, nonce, ciphertext, []byte(tenantProtectorAAD))
	if errOpen != nil {
		return "", errors.New("decrypt tenant provider secret: invalid data key or corrupted ciphertext")
	}
	return string(plaintext), nil
}

func (p *Protector) aead() (cipher.AEAD, error) {
	if p == nil || len(p.key) != 32 {
		return nil, errors.New("tenant provider encryption key is unavailable")
	}
	block, errBlock := aes.NewCipher(p.key)
	if errBlock != nil {
		return nil, errBlock
	}
	return cipher.NewGCM(block)
}

func loadOrCreateDataKey(rawValue, keyPath string) ([]byte, bool, error) {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue != "" {
		key, errParse := parseDataKey(rawValue)
		return key, false, errParse
	}
	keyPath = strings.TrimSpace(keyPath)
	if keyPath == "" {
		return nil, false, errors.New("tenant data key path is required")
	}
	if data, errRead := os.ReadFile(keyPath); errRead == nil {
		key, errParse := parseDataKey(string(data))
		return key, false, errParse
	} else if !os.IsNotExist(errRead) {
		return nil, false, fmt.Errorf("read tenant data key: %w", errRead)
	}
	key := make([]byte, 32)
	if _, errRead := rand.Read(key); errRead != nil {
		return nil, false, fmt.Errorf("generate tenant data key: %w", errRead)
	}
	if errMkdir := os.MkdirAll(filepath.Dir(keyPath), 0o700); errMkdir != nil {
		return nil, false, fmt.Errorf("create tenant data key directory: %w", errMkdir)
	}
	if errWrite := os.WriteFile(keyPath, []byte(base64.RawStdEncoding.EncodeToString(key)+"\n"), 0o600); errWrite != nil {
		return nil, false, fmt.Errorf("write tenant data key: %w", errWrite)
	}
	return key, true, nil
}

func parseDataKey(rawValue string) ([]byte, error) {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return nil, errors.New("tenant data key is empty")
	}
	for _, decoder := range []func(string) ([]byte, error){
		base64.RawStdEncoding.DecodeString,
		base64.StdEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
	} {
		if key, errDecode := decoder(rawValue); errDecode == nil && len(key) == 32 {
			return key, nil
		}
	}
	if len([]byte(rawValue)) == 32 {
		return []byte(rawValue), nil
	}
	return nil, errors.New("tenant data key must be 32 bytes or base64-encoded 32 bytes")
}

func firstEnvironment(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func firstSecretFile(keys ...string) string {
	for _, key := range keys {
		path := strings.TrimSpace(os.Getenv(key))
		if path == "" {
			continue
		}
		if data, errRead := os.ReadFile(path); errRead == nil {
			if value := strings.TrimSpace(string(data)); value != "" {
				return value
			}
		}
	}
	return ""
}
