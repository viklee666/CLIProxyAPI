package auth

import (
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestPriorityHealthConsecutiveFailureBoundary(t *testing.T) {
	now := time.Date(2026, time.November, 26, 12, 0, 0, 0, time.UTC)
	health := newPriorityHealthTracker(nil)
	health.nowFunc = func() time.Time { return now }
	auth := &Auth{ID: "high", Attributes: map[string]string{"priority": "10"}}
	failure := Result{AuthID: auth.ID, Model: "alias(low)", Error: &Error{Code: "upstream", Retryable: true}}
	for range 5 {
		health.observe(failure)
	}
	if health.unhealthy(auth, "alias(low)", nil, now) {
		t.Fatal("five consecutive failures demoted the credential")
	}
	health.observe(failure)
	if !health.unhealthy(auth, "alias(low)", nil, now) {
		t.Fatal("sixth consecutive failure did not demote the credential")
	}
	health.observe(Result{AuthID: auth.ID, Model: "alias(low)", Success: true, CompletedResponse: true})
	if health.unhealthy(auth, "alias(low)", nil, now) {
		t.Fatal("success did not recover the credential")
	}
	health.observe(failure)
	now = now.Add(priorityHealthWindow + time.Nanosecond)
	if health.unhealthy(auth, "alias(low)", nil, now) {
		t.Fatal("expired failure window kept the credential demoted")
	}
}

func TestPriorityHealthSustainedRateNeedsTenSamplesAndExceedsSixtyPercent(t *testing.T) {
	now := time.Date(2026, time.November, 26, 12, 0, 0, 0, time.UTC)
	health := newPriorityHealthTracker(nil)
	health.nowFunc = func() time.Time { return now }
	auth := &Auth{ID: "rate", Attributes: map[string]string{"priority": "10"}}
	for _, success := range []bool{false, true, false, true, false, true, false, true, false} {
		if success {
			health.observe(Result{AuthID: auth.ID, Model: "model", Success: true, CompletedResponse: true})
			continue
		}
		health.observe(Result{AuthID: auth.ID, Model: "model", Error: &Error{Code: "upstream", Retryable: true}})
	}
	if health.unhealthy(auth, "model", nil, now) {
		t.Fatal("fewer than ten samples demoted the credential")
	}
	health.observe(Result{AuthID: auth.ID, Model: "model", Error: &Error{Code: "upstream", Retryable: true}})
	if health.unhealthy(auth, "model", nil, now) {
		t.Fatal("exactly sixty percent errors demoted the credential")
	}
	health.observe(Result{AuthID: auth.ID, Model: "model", Error: &Error{Code: "upstream", Retryable: true}})
	if !health.unhealthy(auth, "model", nil, now) {
		t.Fatal("more than sixty percent errors did not demote the credential")
	}
}

func TestPriorityHealthScopeIsolationAndConcurrencyGate(t *testing.T) {
	now := time.Date(2026, time.November, 26, 12, 0, 0, 0, time.UTC)
	limits := &requestLimiter{states: map[string]*requestLimitState{
		"auth:high": {active: 1, starts: []time.Time{now}},
	}}
	health := newPriorityHealthTracker(limits)
	health.nowFunc = func() time.Time { return now }
	high := &Auth{ID: "high", Attributes: map[string]string{"priority": "10"}, Metadata: map[string]any{"concurrency_limit": 1}}
	low := &Auth{ID: "low", Attributes: map[string]string{"priority": "0"}}
	if got := health.filter([]*Auth{high, low}, "model", nil, now); len(got) != 1 || got[0].ID != low.ID {
		t.Fatalf("at-cap high tier = %#v, want low tier", got)
	}
	limits.states["auth:high"].active = 0
	if got := health.filter([]*Auth{high, low}, "model", nil, now); len(got) != 1 || got[0].ID != high.ID {
		t.Fatalf("below-cap high tier = %#v, want high tier", got)
	}
	high.Metadata["concurrency_limit"] = 0
	limits.states["auth:high"].active = 1
	if got := health.filter([]*Auth{high, low}, "model", nil, now); len(got) != 1 || got[0].ID != high.ID {
		t.Fatalf("unlimited high tier = %#v, want high tier", got)
	}
	limits.states["auth:high"].active = 0
	limits.states["auth:high"].starts = make([]time.Time, 12)
	if got := health.filter([]*Auth{high, low}, "model", nil, now); len(got) != 1 || got[0].ID != high.ID {
		t.Fatalf("RPM-only high tier = %#v, want high tier", got)
	}

	clientA := map[string]any{cliproxyexecutor.ClientKeyIDMetadataKey: "a", cliproxyexecutor.ClientGroupIDsMetadataKey: []int64{1}}
	clientB := map[string]any{cliproxyexecutor.ClientKeyIDMetadataKey: "b", cliproxyexecutor.ClientGroupIDsMetadataKey: []int64{2}}
	for range 6 {
		health.observe(Result{AuthID: high.ID, Model: "model", Options: optionsWithMetadata(clientA), Error: &Error{Code: "upstream", Retryable: true}})
	}

	if !health.unhealthy(high, "model", clientA, now) || health.unhealthy(high, "model", clientB, now) {
		t.Fatal("client health crossed group scopes")
	}
}

func TestPriorityHealthRollingWindowRetainsAllSamplesAndSharesGroupScope(t *testing.T) {
	now := time.Date(2026, time.November, 26, 12, 0, 0, 0, time.UTC)
	health := newPriorityHealthTracker(nil)
	health.nowFunc = func() time.Time { return now }
	auth := &Auth{ID: "rolling", Attributes: map[string]string{"priority": "10"}}
	first := map[string]any{cliproxyexecutor.ClientKeyIDMetadataKey: "one", cliproxyexecutor.ClientGroupIDsMetadataKey: []int64{1}}
	second := map[string]any{cliproxyexecutor.ClientKeyIDMetadataKey: "two", cliproxyexecutor.ClientGroupIDsMetadataKey: []int64{1}}
	for index := 0; index < 130; index++ {
		success := index%2 == 0
		result := Result{AuthID: auth.ID, Model: "model", Options: optionsWithMetadata(first)}
		if success {
			result.Success = true
			result.CompletedResponse = true
		} else {
			result.Error = &Error{Code: "upstream", Retryable: true}
		}
		health.observe(result)
	}
	if health.unhealthy(auth, "model", second, now) {
		t.Fatal("same-group client key did not share health")
	}
	for range 6 {
		health.observe(Result{AuthID: auth.ID, Model: "model", Options: optionsWithMetadata(first), Error: &Error{Code: "upstream", Retryable: true}})
	}
	if !health.unhealthy(auth, "model", second, now) {
		t.Fatal("rolling samples did not retain the sixth consecutive failure")
	}
}

func TestPriorityHealthIgnoresIncompleteAndPreflightResults(t *testing.T) {
	health := newPriorityHealthTracker(nil)
	auth := &Auth{ID: "actual", Attributes: map[string]string{"priority": "10"}}
	for range 6 {
		health.observe(Result{AuthID: auth.ID, Model: "model", Success: true})
	}
	if health.hasRecords(auth.ID, "model", nil) {
		t.Fatal("incomplete success was recorded")
	}
	for range 6 {
		health.observe(Result{AuthID: auth.ID, Model: "model", Error: nil})
	}
	if health.hasRecords(auth.ID, "model", nil) {
		t.Fatal("preflight result was recorded")
	}
}

func optionsWithMetadata(metadata map[string]any) cliproxyexecutor.Options {
	return cliproxyexecutor.Options{Metadata: metadata}
}
