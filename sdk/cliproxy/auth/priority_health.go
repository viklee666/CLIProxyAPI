package auth

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

const (
	priorityHealthWindow        = time.Minute
	priorityHealthMinSamples    = 10
	priorityHealthFailureStreak = 6
)

type priorityHealthSample struct {
	at      time.Time
	success bool
}

// A reusable ring retains the rolling error rate; streaks outlive that window.
type priorityHealthRecord struct {
	samples                       []priorityHealthSample
	head, count, failures, streak int
	isolatedUntil                 time.Time
}

type priorityHealthRecordKey struct {
	authID, model, scope string
	registrationEpoch    uint64
}

type priorityHealthTracker struct {
	mu            sync.Mutex
	records       map[priorityHealthRecordKey]*priorityHealthRecord
	registrations map[string]uint64
	nowFunc       func() time.Time
	limits        *requestLimiter
	lastCleanup   time.Time
}

type priorityHealthMetadataKey struct{}
type priorityHealthTriedKey struct{}

func withPriorityHealthMetadata(ctx context.Context, metadata map[string]any) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, priorityHealthMetadataKey{}, metadata)
}

func priorityHealthMetadata(ctx context.Context) map[string]any {
	if ctx == nil {
		return nil
	}
	metadata, _ := ctx.Value(priorityHealthMetadataKey{}).(map[string]any)
	return metadata
}

func withPriorityHealthTried(ctx context.Context, tried map[string]struct{}) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, priorityHealthTriedKey{}, tried)
}

func priorityHealthTried(ctx context.Context) map[string]struct{} {
	if ctx == nil {
		return nil
	}
	tried, _ := ctx.Value(priorityHealthTriedKey{}).(map[string]struct{})
	return tried
}

func newPriorityHealthTracker(limits *requestLimiter) *priorityHealthTracker {
	return &priorityHealthTracker{records: make(map[priorityHealthRecordKey]*priorityHealthRecord), limits: limits}
}

func priorityHealthScope(metadata map[string]any) string {
	access := clientCredentialAccessFromMetadata(metadata)
	if !access.active || access.allowAll {
		return ""
	}
	groups := slices.Clone(access.groupIDs)
	slices.Sort(groups)
	groups = slices.Compact(groups)
	var key strings.Builder
	key.WriteString(strconv.FormatInt(TenantIDFromMetadata(metadata), 10))
	key.WriteByte(':')
	key.WriteString(strconv.FormatBool(access.allowUngrouped))
	for _, group := range groups {
		key.WriteByte(':')
		key.WriteString(strconv.FormatInt(group, 10))
	}
	return key.String()
}

func (h *priorityHealthTracker) now() time.Time {
	if h != nil && h.nowFunc != nil {
		return h.nowFunc()
	}
	return time.Now()
}

func (r *priorityHealthRecord) prune(now time.Time) {
	cutoff := now.Add(-priorityHealthWindow)
	for r.count > 0 && !r.samples[r.head].at.After(cutoff) {
		if !r.samples[r.head].success {
			r.failures--
		}
		r.head = (r.head + 1) % len(r.samples)
		r.count--
	}
	if r.count == 0 {
		r.head = 0
		if len(r.samples) > 16 {
			r.samples = nil
		}
	}
}

func (r *priorityHealthRecord) append(at time.Time, success bool) {
	r.prune(at)
	if r.count == len(r.samples) {
		samples := make([]priorityHealthSample, max(16, len(r.samples)*2))
		if r.count > 0 {
			n := copy(samples, r.samples[r.head:])
			copy(samples[n:], r.samples[:r.head])
		}
		r.samples, r.head = samples, 0
	}
	r.samples[(r.head+r.count)%len(r.samples)] = priorityHealthSample{at: at, success: success}
	r.count++
	if success {
		r.streak = 0
		r.isolatedUntil = time.Time{}
	} else {
		r.streak = min(r.streak+1, priorityHealthFailureStreak)
		if r.streak >= priorityHealthFailureStreak {
			r.isolatedUntil = at.Add(priorityHealthWindow)
		}
		r.failures++
	}
}

func priorityHealthExcludedError(err *Error) bool {
	return err == nil || isRequestInvalidError(err) || isRequestStopError(err) ||
		isRequestTerminatedError(err) || isSessionAffinityRetryError(err) ||
		err.Code == "scheduler_reject" || err.Code == "stream_truncated"
}

func priorityHealthResultModel(result Result) string {
	if requested := strings.TrimSpace(contextStringValue(result.Options.Metadata[cliproxyexecutor.RequestedModelMetadataKey])); requested != "" {
		return requested
	}
	if requested := strings.TrimSpace(result.RouteModel); requested != "" {
		return requested
	}
	return result.Model
}

func (h *priorityHealthTracker) observe(result Result) {
	if h == nil || result.AuthID == "" || result.SkipQuotaObservation || result.Success && !result.CompletedResponse || !result.Success && priorityHealthExcludedError(result.Error) {
		return
	}
	model := canonicalModelKey(priorityHealthResultModel(result))
	if model == "" {
		return
	}
	now := h.now()
	scope := priorityHealthScope(result.Options.Metadata)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.registrations != nil {
		if epoch, ok := h.registrations[result.AuthID]; !ok || epoch != result.RegistrationEpoch {
			return
		}
	}
	if now.Sub(h.lastCleanup) >= priorityHealthWindow {
		for key, record := range h.records {
			record.prune(now)
			if record.streak == 0 && record.count == 0 {
				delete(h.records, key)
			}
		}
		h.lastCleanup = now
	}
	key := priorityHealthRecordKey{authID: result.AuthID, model: model, scope: scope, registrationEpoch: result.RegistrationEpoch}
	for {
		record := h.records[key]
		if record == nil {
			record = &priorityHealthRecord{}
			h.records[key] = record
		}
		record.append(now, result.Success)
		// Administrators see aggregate health, while group requests remain isolated.
		if key.scope == "" {
			break
		}
		key.scope = ""
	}
}

// Lifecycle changes discard obsolete streaks without time-based recovery or
// allowing delayed results from removed registrations to recreate them.
func (h *priorityHealthTracker) register(authID string, epoch uint64) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.registrations == nil {
		h.registrations = make(map[string]uint64)
	}
	if current, ok := h.registrations[authID]; ok && current >= epoch {
		return
	}
	h.registrations[authID] = epoch
	for key := range h.records {
		if key.authID == authID && key.registrationEpoch != epoch {
			delete(h.records, key)
		}
	}
}

func (h *priorityHealthTracker) remove(authID string, epoch uint64) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if current, ok := h.registrations[authID]; ok && current <= epoch {
		delete(h.registrations, authID)
	}
	for key := range h.records {
		if key.authID == authID && key.registrationEpoch <= epoch {
			delete(h.records, key)
		}
	}
}

func (h *priorityHealthTracker) hasRecords(authID, model string, metadata map[string]any) bool {
	if h == nil || strings.TrimSpace(authID) == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	record := h.records[priorityHealthRecordKey{authID: authID, model: canonicalModelKey(model), scope: priorityHealthScope(metadata), registrationEpoch: h.registrations[authID]}]
	return record != nil && (record.count > 0 || record.streak > 0)
}

func (h *priorityHealthTracker) unhealthyInScope(auth *Auth, model, scope string, now time.Time) bool {
	if h == nil || auth == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	record := h.records[priorityHealthRecordKey{authID: auth.ID, model: model, scope: scope, registrationEpoch: auth.RegistrationEpoch}]
	if record == nil {
		return false
	}
	record.prune(now)
	// Expiry permits a probe, not recovery: the next failure immediately isolates
	// the credential again, and only a completed success clears the streak.
	return record.isolatedUntil.After(now) || record.count >= priorityHealthMinSamples && record.failures*5 > record.count*3
}

func (h *priorityHealthTracker) unhealthy(auth *Auth, model string, metadata map[string]any, now time.Time) bool {
	return h.unhealthyInScope(auth, canonicalModelKey(model), priorityHealthScope(metadata), now)
}

func (h *priorityHealthTracker) concurrencyAtLimit(auth *Auth) bool {
	if h == nil || auth == nil || h.limits == nil {
		return false
	}
	limit := auth.RequestLimits().ConcurrencyLimit
	if limit <= 0 {
		return false
	}
	key := "auth:" + auth.ID
	if group := auth.ReadAttribute(AttributeRequestLimitGroup); group != "" {
		key = "provider:" + group
	} else if source := auth.ReadAttribute(AttributeVirtualSource); source != "" {
		key = "file:" + filepath.Clean(source)
	}
	h.limits.mu.Lock()
	state := h.limits.states[key]
	full := state != nil && state.active >= limit
	h.limits.mu.Unlock()
	return full
}

func (h *priorityHealthTracker) viable(auth *Auth, model string, metadata map[string]any, now time.Time) bool {
	return auth != nil && !isAuthDisabled(auth) && !h.unhealthy(auth, model, metadata, now) && !h.concurrencyAtLimit(auth)
}

// Select the tier before cooldown and tried-ID filtering. Only unhealthy or
// concurrency-saturated targets permit demotion; explicit pins never fail over.
func (h *priorityHealthTracker) filter(auths []*Auth, model string, metadata map[string]any, now time.Time) []*Auth {
	if pinnedAuthIDFromMetadata(metadata) != "" {
		return auths
	}
	return highestPriorityAuths(h.filterViable(auths, model, metadata, now))
}

func (h *priorityHealthTracker) filterViable(auths []*Auth, model string, metadata map[string]any, now time.Time) []*Auth {
	if pinnedAuthIDFromMetadata(metadata) != "" {
		return auths
	}
	scope := priorityHealthScope(metadata)
	model = canonicalModelKey(model)
	var filtered []*Auth
	for i, auth := range auths {
		skip := auth == nil || isAuthDisabled(auth) || h.unhealthyInScope(auth, model, scope, now) || h.concurrencyAtLimit(auth)
		if skip && filtered == nil {
			filtered = make([]*Auth, 0, len(auths)-1)
			filtered = append(filtered, auths[:i]...)
		} else if !skip && filtered != nil {
			filtered = append(filtered, auth)
		}
	}
	if filtered != nil {
		return filtered
	}
	return auths
}

func isAuthDisabled(auth *Auth) bool {
	return auth == nil || auth.Disabled || auth.Status == StatusDisabled
}

func isWeightedSelector(selector Selector) bool {
	if affinity, ok := selector.(*SessionAffinitySelector); ok && affinity != nil {
		selector = affinity.fallback
	}
	_, weighted := selector.(*WeightedRoundRobinSelector)
	return weighted
}

func filterAuthsExcluding(auths []*Auth, excluded map[string]struct{}) []*Auth {
	if len(excluded) == 0 {
		return auths
	}
	var filtered []*Auth
	for i, auth := range auths {
		_, skip := excluded[auth.ID]
		if skip && filtered == nil {
			filtered = make([]*Auth, 0, len(auths)-1)
			filtered = append(filtered, auths[:i]...)
		} else if !skip && filtered != nil {
			filtered = append(filtered, auth)
		}
	}
	if filtered != nil {
		return filtered
	}
	return auths
}

func isSessionAffinityRetryError(err error) bool {
	var authErr *Error
	return errors.As(err, &authErr) && authErr != nil && authErr.Code == "session_auth_retry"
}
