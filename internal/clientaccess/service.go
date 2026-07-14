package clientaccess

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdkaccess "github.com/router-for-me/CLIProxyAPI/v7/sdk/access"
)

const keyPrefix = "sk-cpa-"

type authKey struct {
	Key
	Hash string
}

type authSnapshot struct {
	byHash      map[string]*authKey
	credentials map[string]map[int64]int
}

type runtimeState struct {
	minute int64
	count  int
	active int
}

type Service struct {
	store *Store

	snapshot  atomic.Pointer[authSnapshot]
	runtimeMu sync.Mutex
	runtime   map[int64]*runtimeState
}

func Enabled(configEnabled bool) bool {
	raw := strings.TrimSpace(os.Getenv("CLIENT_ACCESS_ENABLED"))
	if raw == "" {
		return configEnabled
	}
	value, errParse := strconv.ParseBool(raw)
	if errParse != nil {
		return configEnabled
	}
	return value
}

func ResolveDatabasePath(configPath, configuredPath string) string {
	if envPath := strings.TrimSpace(os.Getenv("CLIENT_ACCESS_DB_PATH")); envPath != "" {
		return envPath
	}
	configuredPath = strings.TrimSpace(configuredPath)
	if configuredPath != "" {
		if filepath.IsAbs(configuredPath) {
			return filepath.Clean(configuredPath)
		}
		base := filepath.Dir(strings.TrimSpace(configPath))
		if base == "" || base == "." {
			base, _ = os.Getwd()
		}
		return filepath.Join(base, configuredPath)
	}
	base := filepath.Dir(strings.TrimSpace(configPath))
	if base == "" || base == "." {
		base, _ = os.Getwd()
	}
	return filepath.Join(base, "data", "client-access.sqlite")
}

func New(path string) (*Service, error) {
	store, errOpen := Open(path)
	if errOpen != nil {
		return nil, errOpen
	}
	service := &Service{store: store, runtime: make(map[int64]*runtimeState)}
	if errReload := service.reload(context.Background()); errReload != nil {
		_ = store.Close()
		return nil, fmt.Errorf("load client access keys: %w", errReload)
	}
	return service, nil
}

func (s *Service) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

func (s *Service) Identifier() string { return ProviderIdentifier }

func (s *Service) reload(ctx context.Context) error {
	keys, errList := s.store.ListAllStoredKeys(ctx)
	if errList != nil {
		return errList
	}
	bindings, errBindings := s.store.ListAllCredentialBindings(ctx)
	if errBindings != nil {
		return errBindings
	}
	next := &authSnapshot{
		byHash:      make(map[string]*authKey, len(keys)),
		credentials: make(map[string]map[int64]int),
	}
	for i := range keys {
		item := keys[i]
		copyKey := &authKey{Key: item.Key, Hash: item.Hash}
		next.byHash[item.Hash] = copyKey
	}
	for _, binding := range bindings {
		groups := next.credentials[binding.AuthIndex]
		if groups == nil {
			groups = make(map[int64]int)
			next.credentials[binding.AuthIndex] = groups
		}
		groups[binding.GroupID] = binding.Priority
	}
	s.snapshot.Store(next)
	return nil
}

func (s *Service) Authenticate(_ context.Context, request *http.Request) (*sdkaccess.Result, *sdkaccess.AuthError) {
	if s == nil {
		return nil, sdkaccess.NewNotHandledError()
	}
	snapshot := s.snapshot.Load()
	if snapshot == nil || len(snapshot.byHash) == 0 {
		return nil, sdkaccess.NewNotHandledError()
	}
	secret, source := requestAPIKey(request)
	if secret == "" {
		return nil, sdkaccess.NewNoCredentialsError()
	}
	hash := hashSecret(secret)
	key := snapshot.byHash[hash]
	if key == nil {
		return nil, sdkaccess.NewInvalidCredentialError()
	}
	now := time.Now().UTC()
	if !key.Enabled {
		return nil, sdkaccess.NewAccessDeniedError("API key is disabled")
	}
	if key.ExpiresAt != nil && !now.Before(*key.ExpiresAt) {
		return nil, sdkaccess.NewAccessDeniedError("API key has expired")
	}
	release, authErr := s.acquireRuntime(key, now)
	if authErr != nil {
		return nil, authErr
	}
	go s.touchLastUsed(key.ID, now)
	return &sdkaccess.Result{
		Provider:  ProviderIdentifier,
		Principal: secret,
		Metadata: map[string]string{
			"source":                  source,
			MetadataKeyID:             strconv.FormatInt(key.ID, 10),
			MetadataKeyHash:           hash,
			MetadataKeyGroupIDs:       joinInt64s(key.GroupIDs),
			MetadataKeyAllowAllGroups: strconv.FormatBool(key.AllowAllGroups),
			MetadataKeyAllowUngrouped: strconv.FormatBool(key.AllowUngrouped),
		},
		Release: release,
	}, nil
}

func (s *Service) acquireRuntime(key *authKey, now time.Time) (func(), *sdkaccess.AuthError) {
	minute := now.Unix() / 60
	s.runtimeMu.Lock()
	state := s.runtime[key.ID]
	if state == nil {
		state = &runtimeState{minute: minute}
		s.runtime[key.ID] = state
	}
	if state.minute != minute {
		state.minute = minute
		state.count = 0
	}
	if key.RPMLimit > 0 && state.count >= key.RPMLimit {
		s.runtimeMu.Unlock()
		return nil, sdkaccess.NewRateLimitedError("API key RPM limit exceeded")
	}
	if key.ConcurrencyLimit > 0 && state.active >= key.ConcurrencyLimit {
		s.runtimeMu.Unlock()
		return nil, sdkaccess.NewRateLimitedError("API key concurrency limit exceeded")
	}
	state.count++
	state.active++
	s.runtimeMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.runtimeMu.Lock()
			if current := s.runtime[key.ID]; current != nil && current.active > 0 {
				current.active--
			}
			s.runtimeMu.Unlock()
		})
	}, nil
}

func (s *Service) touchLastUsed(id int64, now time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.store.TouchKey(ctx, id, now)
}

func (s *Service) currentConcurrency(id int64) int {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if state := s.runtime[id]; state != nil {
		return state.active
	}
	return 0
}

func (s *Service) CreateGroup(ctx context.Context, input GroupCreate) (Group, error) {
	group, errCreate := s.store.CreateGroup(ctx, input)
	if errCreate == nil {
		errCreate = s.reload(ctx)
	}
	return group, errCreate
}

func (s *Service) GetGroup(ctx context.Context, id int64) (Group, error) {
	return s.store.GetGroup(ctx, id)
}

func (s *Service) ListGroups(ctx context.Context, opts ListOptions) (Page[Group], error) {
	return s.store.ListGroups(ctx, opts)
}

func (s *Service) UpdateGroup(ctx context.Context, id int64, input GroupUpdate) (Group, error) {
	group, errUpdate := s.store.UpdateGroup(ctx, id, input)
	if errUpdate == nil {
		errUpdate = s.reload(ctx)
	}
	return group, errUpdate
}

func (s *Service) DeleteGroup(ctx context.Context, id int64) error {
	if errDelete := s.store.DeleteGroup(ctx, id); errDelete != nil {
		return errDelete
	}
	return s.reload(ctx)
}

func (s *Service) CreateKey(ctx context.Context, input KeyCreate) (CreatedKey, error) {
	secret := strings.TrimSpace(input.CustomSecret)
	if secret == "" {
		generated, errGenerate := generateSecret()
		if errGenerate != nil {
			return CreatedKey{}, errGenerate
		}
		secret = generated
	}
	if errValidate := validateSecret(secret); errValidate != nil {
		return CreatedKey{}, errValidate
	}
	prefix := secret
	if len(prefix) > 14 {
		prefix = prefix[:14]
	}
	key, errCreate := s.store.CreateKey(ctx, input, secret, prefix, hashSecret(secret))
	if errCreate != nil {
		return CreatedKey{}, errCreate
	}
	if errReload := s.reload(ctx); errReload != nil {
		return CreatedKey{}, errReload
	}
	return CreatedKey{Key: key, Secret: secret}, nil
}

func (s *Service) GetKey(ctx context.Context, id int64) (Key, error) {
	key, errGet := s.store.GetKey(ctx, id)
	if errGet == nil {
		key.CurrentConcurrency = s.currentConcurrency(id)
	}
	return key, errGet
}

func (s *Service) ListKeys(ctx context.Context, opts ListOptions) (Page[Key], error) {
	page, errList := s.store.ListKeys(ctx, opts)
	if errList != nil {
		return Page[Key]{}, errList
	}
	for i := range page.Items {
		page.Items[i].CurrentConcurrency = s.currentConcurrency(page.Items[i].ID)
	}
	return page, nil
}

func (s *Service) UpdateKey(ctx context.Context, id int64, input KeyUpdate) (Key, error) {
	key, errUpdate := s.store.UpdateKey(ctx, id, input)
	if errUpdate != nil {
		return Key{}, errUpdate
	}
	if errReload := s.reload(ctx); errReload != nil {
		return Key{}, errReload
	}
	key.CurrentConcurrency = s.currentConcurrency(id)
	return key, nil
}

func (s *Service) DeleteKey(ctx context.Context, id int64) error {
	if errDelete := s.store.DeleteKey(ctx, id); errDelete != nil {
		return errDelete
	}
	s.runtimeMu.Lock()
	delete(s.runtime, id)
	s.runtimeMu.Unlock()
	return s.reload(ctx)
}

func (s *Service) ListCredentialBindings(ctx context.Context, opts ListOptions) (Page[CredentialBinding], error) {
	return s.store.ListCredentialBindings(ctx, opts)
}

func (s *Service) ReplaceCredentialBindings(ctx context.Context, input CredentialBindingBatch) error {
	if len(input.AuthIndices) == 0 {
		return errors.New("auth_indices are required")
	}
	if errReplace := s.store.ReplaceCredentialBindings(ctx, input.AuthIndices, input.Groups); errReplace != nil {
		return errReplace
	}
	return s.reload(ctx)
}

// ResolveCredentialAccess applies client group isolation and returns a request-specific priority.
func (s *Service) ResolveCredentialAccess(authIndex string, allowedGroupIDs []int64, allowAllGroups, allowUngrouped bool) (bool, int, bool) {
	if s == nil || allowAllGroups {
		return true, 0, false
	}
	snapshot := s.snapshot.Load()
	if snapshot == nil {
		return false, 0, false
	}
	memberships := snapshot.credentials[strings.TrimSpace(authIndex)]
	if len(memberships) == 0 {
		return allowUngrouped, 0, false
	}
	bestPriority := 0
	found := false
	for _, groupID := range allowedGroupIDs {
		priority, ok := memberships[groupID]
		if !ok {
			continue
		}
		if !found || priority > bestPriority {
			bestPriority = priority
		}
		found = true
	}
	return found, bestPriority, found
}

func generateSecret() (string, error) {
	random := make([]byte, 32)
	if _, errRead := rand.Read(random); errRead != nil {
		return "", fmt.Errorf("generate API key: %w", errRead)
	}
	return keyPrefix + base64.RawURLEncoding.EncodeToString(random), nil
}

func validateSecret(secret string) error {
	if len(secret) < 16 || len(secret) > 256 {
		return errors.New("API key must contain between 16 and 256 characters")
	}
	if strings.IndexFunc(secret, func(r rune) bool { return r <= ' ' || r == 0x7f }) >= 0 {
		return errors.New("API key cannot contain whitespace or control characters")
	}
	return nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func requestAPIKey(request *http.Request) (string, string) {
	if request == nil {
		return "", ""
	}
	authorization := strings.TrimSpace(request.Header.Get("Authorization"))
	if authorization != "" {
		parts := strings.SplitN(authorization, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			return strings.TrimSpace(parts[1]), "authorization"
		}
		return authorization, "authorization"
	}
	for _, header := range []string{"X-Goog-Api-Key", "X-Api-Key"} {
		if value := strings.TrimSpace(request.Header.Get(header)); value != "" {
			return value, strings.ToLower(header)
		}
	}
	if request.URL != nil {
		if value := strings.TrimSpace(request.URL.Query().Get("key")); value != "" {
			return value, "query-key"
		}
		if value := strings.TrimSpace(request.URL.Query().Get("auth_token")); value != "" {
			return value, "query-auth-token"
		}
	}
	return "", ""
}

func joinInt64s(values []int64) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if value > 0 {
			parts = append(parts, strconv.FormatInt(value, 10))
		}
	}
	return strings.Join(parts, ",")
}
