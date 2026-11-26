package auth

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
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

// A reusable ring retains every outcome in the rolling window, regardless of RPM.
type priorityHealthRecord struct {
	samples                       []priorityHealthSample
	head, count, failures, streak int
	last                          time.Time
}

type priorityHealthRecordKey struct {
	authID, model, scope string
	registrationEpoch    uint64
}

type priorityHealthTracker struct {
	mu          sync.Mutex
	records     map[priorityHealthRecordKey]*priorityHealthRecord
	nowFunc     func() time.Time
	limits      *requestLimiter
	lastCleanup time.Time
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
	r.streak = min(r.streak, r.count)
	if r.count == 0 {
		r.head, r.streak = 0, 0
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
	r.last = at
	if success {
		r.streak = 0
	} else {
		r.streak++
		r.failures++
	}
}

func priorityHealthExcludedError(err *Error) bool {
	return err == nil || isRequestInvalidError(err) || isRequestStopError(err) ||
		isRequestTerminatedError(err) || isCredentialTerminatedError(err) || isSchedulerRejectError(err) ||
		isSessionAffinityRetryError(err) || err.Code == ErrorCodeStreamTruncated
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
	model := canonicalModelLookupKey(priorityHealthResultModel(result))
	if model == "" {
		return
	}
	now := h.now()
	scope := priorityHealthScope(result.Options.Metadata)
	h.mu.Lock()
	defer h.mu.Unlock()
	if now.Sub(h.lastCleanup) >= priorityHealthWindow {
		for key, record := range h.records {
			if !record.last.After(now.Add(-priorityHealthWindow)) {
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

func (h *priorityHealthTracker) hasRecords(authID, model string, metadata map[string]any) bool {
	if h == nil || strings.TrimSpace(authID) == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	record := h.records[priorityHealthRecordKey{authID: authID, model: canonicalModelLookupKey(model), scope: priorityHealthScope(metadata)}]
	return record != nil && record.count > 0
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
	return record.streak >= priorityHealthFailureStreak || record.count >= priorityHealthMinSamples && record.failures*5 > record.count*3
}

func (h *priorityHealthTracker) unhealthy(auth *Auth, model string, metadata map[string]any, now time.Time) bool {
	return h.unhealthyInScope(auth, canonicalModelLookupKey(model), priorityHealthScope(metadata), now)
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

// Select the tier before cooldown and tried-ID filtering, never because a target
// was merely busy, rate limited, or already attempted. Pins never fail over.
func (h *priorityHealthTracker) filter(auths []*Auth, model string, metadata map[string]any, now time.Time) []*Auth {
	if pinnedAuthIDFromMetadata(metadata) != "" {
		return auths
	}
	scope := priorityHealthScope(metadata)
	model = canonicalModelLookupKey(model)
	best, found := 0, false
	filtered := make([]*Auth, 0, len(auths))
	for _, auth := range auths {
		if auth == nil || isAuthDisabled(auth) || h.unhealthyInScope(auth, model, scope, now) || h.concurrencyAtLimit(auth) {
			continue
		}
		priority := authPriority(auth)
		if !found || priority > best {
			best, found = priority, true
			filtered = filtered[:0]
		}
		if priority == best {
			filtered = append(filtered, auth)
		}
	}
	return filtered
}
