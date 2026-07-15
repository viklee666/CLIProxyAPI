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
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

const (
	keyPrefix                     = "sk-cpa-"
	defaultTokenReservation int64 = 1024
	reservationReleaseGrace       = 5 * time.Second
)

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
	store            *Store
	tokenReservation int64
	releaseCtx       context.Context
	releaseCancel    context.CancelFunc
	releaseWG        sync.WaitGroup

	snapshot  atomic.Pointer[authSnapshot]
	runtimeMu sync.Mutex
	runtime   map[int64]*runtimeState
}

type Option func(*Service)

// WithTokenReservation sets provisional tokens held by each in-flight request.
func WithTokenReservation(tokens int64) Option {
	return func(service *Service) {
		if service == nil {
			return
		}
		if tokens <= 0 {
			tokens = defaultTokenReservation
		}
		service.tokenReservation = tokens
	}
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

func ResolveTokenReservation(configured int64) int64 {
	if raw := strings.TrimSpace(os.Getenv("CLIENT_ACCESS_TOKEN_RESERVATION")); raw != "" {
		if parsed, errParse := strconv.ParseInt(raw, 10, 64); errParse == nil && parsed > 0 {
			return parsed
		}
	}
	if configured > 0 {
		return configured
	}
	return defaultTokenReservation
}

func New(path string, options ...Option) (*Service, error) {
	store, errOpen := Open(path)
	if errOpen != nil {
		return nil, errOpen
	}
	releaseCtx, releaseCancel := context.WithCancel(context.Background())
	service := &Service{
		store:            store,
		runtime:          make(map[int64]*runtimeState),
		tokenReservation: defaultTokenReservation,
		releaseCtx:       releaseCtx,
		releaseCancel:    releaseCancel,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
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
	if s.releaseCancel != nil {
		s.releaseCancel()
	}
	s.releaseWG.Wait()
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
	reservationID, errReservationID := generateReservationID()
	if errReservationID != nil {
		release()
		return nil, sdkaccess.NewInternalAuthError("create quota reservation", errReservationID)
	}
	_, _, errReserve := s.store.ReserveUsage(request.Context(), key.ID, reservationID, s.tokenReservation, now)
	if errReserve != nil {
		release()
		var quotaErr *QuotaExceededError
		if errors.As(errReserve, &quotaErr) {
			message := fmt.Sprintf("API key %s %s quota exceeded (%d/%d)", quotaErr.Resource, quotaErr.Window, quotaErr.Used, quotaErr.Limit)
			if quotaErr.ResetAt != nil {
				message += "; resets at " + quotaErr.ResetAt.UTC().Format(time.RFC3339)
			}
			return nil, sdkaccess.NewRateLimitedErrorUntil(message, quotaErr.ResetAt)
		}
		return nil, sdkaccess.NewInternalAuthError("reserve API key quota", errReserve)
	}
	var releaseOnce sync.Once
	releaseRequest := func() {
		releaseOnce.Do(func() {
			release()
			s.scheduleReservationRelease(reservationID)
		})
	}
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
			MetadataKeyReservationID:  reservationID,
		},
		Release: releaseRequest,
	}, nil
}

func (s *Service) scheduleReservationRelease(reservationID string) {
	if s == nil || s.store == nil || reservationID == "" {
		return
	}
	s.releaseWG.Add(1)
	go func() {
		defer s.releaseWG.Done()
		timer := time.NewTimer(reservationReleaseGrace)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-s.releaseCtx.Done():
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, errSettle := s.store.SettleTokenReservation(ctx, reservationID, 0, time.Now().UTC()); errSettle != nil && s.releaseCtx.Err() == nil {
			log.WithError(errSettle).Warn("client access: failed to release token reservation")
		}
	}()
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

func (s *Service) currentConcurrency(id int64) int {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if state := s.runtime[id]; state != nil {
		return state.active
	}
	return 0
}

// HandleUsage settles provisional tokens and persists actual usage.
func (s *Service) HandleUsage(_ context.Context, record coreusage.Record) {
	if s == nil || s.store == nil {
		return
	}
	actualTokens := record.Detail.TotalTokens
	if actualTokens == 0 {
		actualTokens = record.Detail.InputTokens + record.Detail.OutputTokens + record.Detail.ReasoningTokens
	}
	if actualTokens < 0 {
		actualTokens = 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	found := false
	var errSettle error
	if reservationID := strings.TrimSpace(record.ClientReservationID); reservationID != "" {
		found, errSettle = s.store.SettleTokenReservation(ctx, reservationID, actualTokens, time.Now().UTC())
	}
	if errSettle == nil && !found {
		if secret := strings.TrimSpace(record.APIKey); secret != "" {
			_, errSettle = s.store.AddTokenUsageByHash(ctx, hashSecret(secret), actualTokens, time.Now().UTC())
		}
	}
	if errSettle != nil {
		log.WithError(errSettle).Warn("client access: failed to settle token usage")
	}
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
	_, errReplace := s.ReplaceCredentialBindingsWithStats(ctx, input)
	return errReplace
}

func (s *Service) ReplaceCredentialBindingsWithStats(ctx context.Context, input CredentialBindingBatch) (CredentialBindingChangeStats, error) {
	stats, errReplace := s.store.ReplaceCredentialBindingsWithStats(ctx, input.AuthIndices, input.Groups)
	if errReplace != nil {
		return CredentialBindingChangeStats{}, errReplace
	}
	if stats.Updated == 0 {
		return stats, nil
	}
	if errReload := s.reload(ctx); errReload != nil {
		return CredentialBindingChangeStats{}, errReload
	}
	return stats, nil
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

func generateReservationID() (string, error) {
	random := make([]byte, 18)
	if _, errRead := rand.Read(random); errRead != nil {
		return "", fmt.Errorf("generate reservation ID: %w", errRead)
	}
	return "car_" + base64.RawURLEncoding.EncodeToString(random), nil
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
