package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// AttributeRequestLimitGroup shares a provider's admission budget across its API keys.
const AttributeRequestLimitGroup = "request_limit_group"

type requestLimitState struct {
	active   int
	starts   []time.Time
	lastUsed time.Time
}

type requestLimiter struct {
	mu          sync.Mutex
	states      map[string]*requestLimitState
	nowFunc     func() time.Time
	lastCleanup time.Time
}

type requestLimitError struct {
	limit      string
	retryAfter time.Duration
}

func (err *requestLimitError) Error() string {
	return fmt.Sprintf("configured %s limit reached", err.limit)
}

func (err *requestLimitError) StatusCode() int            { return http.StatusTooManyRequests }
func (err *requestLimitError) RetryAfter() *time.Duration { return &err.retryAfter }

// RequestLimits returns the admission limits carried by any provider's credential.
func (auth *Auth) RequestLimits() config.RequestLimits {
	if auth == nil {
		return config.RequestLimits{}
	}
	auth.metadataMu.RLock()
	defer auth.metadataMu.RUnlock()
	read := func(key, alias string) int {
		raw, exists := auth.Metadata[key]
		if !exists {
			raw = auth.Metadata[alias]
		}
		value, errParse := ParseRequestLimit(raw)
		if errParse != nil {
			return 0
		}
		return value
	}
	return config.RequestLimits{
		ConcurrencyLimit: read("concurrency_limit", "concurrency-limit"),
		RPMLimit:         read("rpm_limit", "rpm-limit"),
	}
}

// ValidateAuthRequestLimits validates limits for file, plugin, and config credentials.
func ValidateAuthRequestLimits(auth *Auth) error {
	if auth == nil {
		return nil
	}
	metadata := auth.SnapshotMetadata()
	for _, key := range []string{"concurrency_limit", "rpm_limit"} {
		if value, exists := metadata[key]; exists && value != nil {
			if _, errLimit := ParseRequestLimit(value); errLimit != nil {
				return fmt.Errorf("%s: %w", key, errLimit)
			}
		}
	}
	return nil
}

// ParseRequestLimit accepts only non-negative JSON integer values.
func ParseRequestLimit(value any) (int, error) {
	var text string
	switch number := value.(type) {
	case json.Number:
		text = number.String()
	case int:
		text = strconv.Itoa(number)
	case int64:
		text = strconv.FormatInt(number, 10)
	case float64:
		text = strconv.FormatFloat(number, 'f', -1, 64)
	default:
		return 0, fmt.Errorf("request limit must be a non-negative integer")
	}
	limit, errParse := strconv.Atoi(text)
	if errParse != nil || limit < 0 {
		return 0, fmt.Errorf("request limit must be a non-negative integer")
	}
	return limit, nil
}

func (limiter *requestLimiter) acquire(auth *Auth) (func(), error) {
	limits := auth.RequestLimits()
	key := "auth:" + auth.ID
	if group := auth.Attributes[AttributeRequestLimitGroup]; group != "" {
		key = "provider:" + group
	} else if source := auth.Attributes[AttributeVirtualSource]; source != "" {
		key = "file:" + filepath.Clean(source)
	}
	limiter.mu.Lock()
	now := time.Now()
	if limiter.nowFunc != nil {
		now = limiter.nowFunc()
	}
	if limiter.states == nil {
		limiter.states = make(map[string]*requestLimitState)
	}
	if now.Sub(limiter.lastCleanup) >= time.Minute {
		for id, state := range limiter.states {
			if state.active == 0 && now.Sub(state.lastUsed) >= time.Minute {
				delete(limiter.states, id)
			}
		}
		limiter.lastCleanup = now
	}
	state := limiter.states[key]
	if state == nil {
		state = &requestLimitState{}
		limiter.states[key] = state
	}
	cutoff := now.Add(-time.Minute)
	first := 0
	for first < len(state.starts) && !state.starts[first].After(cutoff) {
		first++
	}
	if first > 0 {
		state.starts = append([]time.Time(nil), state.starts[first:]...)
	}
	state.lastUsed = now
	if limits.ConcurrencyLimit > 0 && state.active >= limits.ConcurrencyLimit {
		limiter.mu.Unlock()
		return nil, &requestLimitError{limit: "concurrency", retryAfter: time.Second}
	}
	if limits.RPMLimit > 0 && len(state.starts) >= limits.RPMLimit {
		retryAfter := state.starts[len(state.starts)-limits.RPMLimit].Add(time.Minute).Sub(now)
		limiter.mu.Unlock()
		return nil, &requestLimitError{limit: "RPM", retryAfter: retryAfter}
	}
	state.active++
	if limits.RPMLimit > 0 {
		state.starts = append(state.starts, now)
	}
	limiter.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			limiter.mu.Lock()
			state.active--
			limiter.mu.Unlock()
		})
	}, nil
}
