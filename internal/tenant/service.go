package tenant

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid tenant credentials")
	ErrInvalidSession     = errors.New("invalid tenant session")
)

type ownershipSnapshot struct {
	byAuthIndex map[string]int64
}

type Service struct {
	store     *Store
	protector *Protector
	ownership atomic.Pointer[ownershipSnapshot]
}

func ResolveDatabasePath(configPath string) string {
	if envPath := strings.TrimSpace(os.Getenv("TENANT_DB_PATH")); envPath != "" {
		return envPath
	}
	base := filepath.Dir(strings.TrimSpace(configPath))
	if base == "" || base == "." {
		base, _ = os.Getwd()
	}
	return filepath.Join(base, "data", "tenant.sqlite")
}

func New(path string) (*Service, error) {
	protector, errProtector := newProtector(path)
	if errProtector != nil {
		return nil, errProtector
	}
	store, errOpen := Open(path)
	if errOpen != nil {
		return nil, errOpen
	}
	service := &Service{store: store, protector: protector}
	if errReload := service.reloadOwnership(context.Background()); errReload != nil {
		_ = store.Close()
		return nil, fmt.Errorf("load credential ownership: %w", errReload)
	}
	return service, nil
}

func (s *Service) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

func GeneratePassword() (string, error) {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	output := make([]byte, GeneratedPasswordLength)
	buffer := make([]byte, GeneratedPasswordLength*2)
	limit := byte(len(alphabet) * (256 / len(alphabet)))
	for index := 0; index < len(output); {
		if _, errRead := rand.Read(buffer); errRead != nil {
			return "", fmt.Errorf("generate tenant password: %w", errRead)
		}
		for _, value := range buffer {
			if value >= limit {
				continue
			}
			output[index] = alphabet[int(value)%len(alphabet)]
			index++
			if index == len(output) {
				break
			}
		}
	}
	return string(output), nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Tenant, string, error) {
	if s == nil || s.store == nil {
		return Tenant{}, "", errors.New("tenant service is unavailable")
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		return Tenant{}, "", errors.New("tenant display name is required")
	}
	password := input.Password
	if password == "" {
		generated, errGenerate := GeneratePassword()
		if errGenerate != nil {
			return Tenant{}, "", errGenerate
		}
		password = generated
	}
	if len(password) < 6 {
		return Tenant{}, "", errors.New("tenant password must be at least 6 characters")
	}
	hash, errHash := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if errHash != nil {
		return Tenant{}, "", fmt.Errorf("hash tenant password: %w", errHash)
	}
	item, errCreate := s.store.createTenant(ctx, displayName, string(hash))
	if errCreate != nil {
		return Tenant{}, "", errCreate
	}
	return item, password, nil
}

func (s *Service) Get(ctx context.Context, tenantID int64) (Tenant, error) {
	if s == nil || s.store == nil {
		return Tenant{}, errors.New("tenant service is unavailable")
	}
	return s.store.GetTenant(ctx, tenantID)
}

func (s *Service) List(ctx context.Context) ([]Tenant, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("tenant service is unavailable")
	}
	return s.store.ListTenants(ctx)
}

func (s *Service) Update(ctx context.Context, tenantID int64, input UpdateInput) (Tenant, error) {
	if s == nil || s.store == nil {
		return Tenant{}, errors.New("tenant service is unavailable")
	}
	item, errUpdate := s.store.UpdateTenant(ctx, tenantID, input)
	if errUpdate != nil {
		return Tenant{}, errUpdate
	}
	// A disabled tenant must fail closed before runtime Auth removal completes.
	// syncTenantAuths will rebuild ownership if the tenant is later re-enabled.
	if !item.Enabled {
		if errOwnership := s.store.deleteCredentialOwnershipForTenant(ctx, tenantID); errOwnership != nil {
			return Tenant{}, errOwnership
		}
		if errReload := s.reloadOwnership(ctx); errReload != nil {
			return Tenant{}, errReload
		}
	}
	return item, nil
}

func (s *Service) Delete(ctx context.Context, tenantID int64) error {
	if s == nil || s.store == nil {
		return errors.New("tenant service is unavailable")
	}
	if errDelete := s.store.DeleteTenant(ctx, tenantID); errDelete != nil {
		return errDelete
	}
	return s.reloadOwnership(ctx)
}

func (s *Service) AuthenticatePassword(ctx context.Context, password string) (Tenant, error) {
	if s == nil || s.store == nil || password == "" {
		return Tenant{}, ErrInvalidCredentials
	}
	credentials, errList := s.store.listTenantCredentials(ctx)
	if errList != nil {
		return Tenant{}, errList
	}
	for _, item := range credentials {
		if bcrypt.CompareHashAndPassword([]byte(item.passwordHash), []byte(password)) != nil {
			continue
		}
		if !item.Enabled {
			return Tenant{}, ErrInvalidCredentials
		}
		return item.Tenant, nil
	}
	return Tenant{}, ErrInvalidCredentials
}

func (s *Service) IssueSession(ctx context.Context, tenantID int64, ttl time.Duration) (string, Session, error) {
	if s == nil || s.store == nil {
		return "", Session{}, errors.New("tenant service is unavailable")
	}
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	tenant, errGet := s.store.GetTenant(ctx, tenantID)
	if errGet != nil {
		return "", Session{}, errGet
	}
	if !tenant.Enabled {
		return "", Session{}, ErrInvalidCredentials
	}
	raw := make([]byte, 32)
	if _, errRead := rand.Read(raw); errRead != nil {
		return "", Session{}, fmt.Errorf("generate tenant session token: %w", errRead)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now().UTC()
	session := Session{TenantID: tenantID, CreatedAt: now, ExpiresAt: now.Add(ttl)}
	if errCreate := s.store.createSession(ctx, hashToken(token), tenantID, session.ExpiresAt); errCreate != nil {
		return "", Session{}, errCreate
	}
	return token, session, nil
}

func (s *Service) AuthenticateSession(ctx context.Context, token string) (Tenant, error) {
	if s == nil || s.store == nil || strings.TrimSpace(token) == "" {
		return Tenant{}, ErrInvalidSession
	}
	item, errGet := s.store.tenantBySession(ctx, hashToken(token), time.Now().UTC())
	if errors.Is(errGet, sql.ErrNoRows) {
		return Tenant{}, ErrInvalidSession
	}
	return item, errGet
}

func (s *Service) ChangePassword(ctx context.Context, tenantID int64, currentPassword, nextPassword string) error {
	if s == nil || s.store == nil {
		return errors.New("tenant service is unavailable")
	}
	if len(nextPassword) < 6 {
		return errors.New("tenant password must be at least 6 characters")
	}
	credential, errCredential := s.store.credential(ctx, tenantID)
	if errCredential != nil {
		return errCredential
	}
	if bcrypt.CompareHashAndPassword([]byte(credential.passwordHash), []byte(currentPassword)) != nil {
		return ErrInvalidCredentials
	}
	return s.replacePassword(ctx, tenantID, nextPassword)
}

func (s *Service) ResetPassword(ctx context.Context, tenantID int64) (string, error) {
	if s == nil || s.store == nil {
		return "", errors.New("tenant service is unavailable")
	}
	password, errGenerate := GeneratePassword()
	if errGenerate != nil {
		return "", errGenerate
	}
	if errReplace := s.replacePassword(ctx, tenantID, password); errReplace != nil {
		return "", errReplace
	}
	return password, nil
}

func (s *Service) replacePassword(ctx context.Context, tenantID int64, password string) error {
	hash, errHash := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if errHash != nil {
		return fmt.Errorf("hash tenant password: %w", errHash)
	}
	return s.store.replacePasswordAndRevokeSessions(ctx, tenantID, string(hash))
}

func (s *Service) RevokeSessions(ctx context.Context, tenantID int64) error {
	if s == nil || s.store == nil {
		return errors.New("tenant service is unavailable")
	}
	return s.store.revokeSessions(ctx, tenantID)
}

// RevokeSession invalidates exactly one bearer session during logout.
func (s *Service) RevokeSession(ctx context.Context, token string) error {
	if s == nil || s.store == nil {
		return errors.New("tenant service is unavailable")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrInvalidSession
	}
	return s.store.revokeSession(ctx, hashToken(token))
}

func (s *Service) SetCredentialOwnership(ctx context.Context, authIndex string, tenantID int64) error {
	if s == nil || s.store == nil {
		return errors.New("tenant service is unavailable")
	}
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" || tenantID <= 0 {
		return errors.New("auth index and tenant id are required")
	}
	if _, errGet := s.store.GetTenant(ctx, tenantID); errGet != nil {
		return errGet
	}
	if errPut := s.store.putCredentialOwnership(ctx, authIndex, tenantID); errPut != nil {
		return errPut
	}
	return s.reloadOwnership(ctx)
}

func (s *Service) RemoveCredentialOwnership(ctx context.Context, authIndex string) error {
	if s == nil || s.store == nil {
		return errors.New("tenant service is unavailable")
	}
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return errors.New("auth index is required")
	}
	if errDelete := s.store.deleteCredentialOwnership(ctx, authIndex); errDelete != nil {
		return errDelete
	}
	return s.reloadOwnership(ctx)
}

func (s *Service) OwnerOf(authIndex string) int64 {
	if s == nil {
		return 0
	}
	snapshot := s.ownership.Load()
	if snapshot == nil {
		return 0
	}
	return snapshot.byAuthIndex[strings.TrimSpace(authIndex)]
}

func (s *Service) HasOwnedCredentials() bool {
	if s == nil {
		return false
	}
	snapshot := s.ownership.Load()
	return snapshot != nil && len(snapshot.byAuthIndex) > 0
}

func (s *Service) reloadOwnership(ctx context.Context) error {
	items, errList := s.store.listCredentialOwnership(ctx)
	if errList != nil {
		return errList
	}
	s.ownership.Store(&ownershipSnapshot{byAuthIndex: items})
	return nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
