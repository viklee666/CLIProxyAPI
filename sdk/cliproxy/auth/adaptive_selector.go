package auth

import (
	"context"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

const (
	defaultAdaptiveTopK         = 3
	defaultAdaptiveEWMAAlpha    = 0.2
	defaultAdaptiveTTFTTargetMS = int64(30_000)
)

type adaptiveRuntimeState struct {
	active         int
	successEWMA    float64
	outcomeSamples int64
	ttftEWMA       float64
	ttftSamples    int64
	lastSampleAt   time.Time
}

// AdaptiveScore describes one credential's current scheduling score.
type AdaptiveScore struct {
	AuthID         string    `json:"auth_id"`
	AuthIndex      string    `json:"auth_index"`
	Provider       string    `json:"provider"`
	Priority       int       `json:"priority"`
	ActiveRequests int       `json:"active_requests"`
	SuccessRate    float64   `json:"success_rate"`
	ErrorRate      float64   `json:"error_rate"`
	TTFTMS         float64   `json:"ttft_ms"`
	OutcomeSamples int64     `json:"outcome_samples"`
	TTFTSamples    int64     `json:"ttft_samples"`
	Score          float64   `json:"score"`
	Rank           int       `json:"rank"`
	Eligible       bool      `json:"eligible"`
	EscapeSticky   bool      `json:"escape_sticky"`
	LastSampleAt   time.Time `json:"last_sample_at,omitempty"`
}

// AdaptiveSelector chooses credentials from a weighted top-K using live runtime metrics.
type AdaptiveSelector struct {
	config internalconfig.AdaptiveRoutingConfig

	mu     sync.RWMutex
	states map[string]*adaptiveRuntimeState
	rand   func() float64
}

// NewAdaptiveSelector creates a runtime-aware selector with normalized defaults.
func NewAdaptiveSelector(cfg internalconfig.AdaptiveRoutingConfig) *AdaptiveSelector {
	return &AdaptiveSelector{
		config: normalizeAdaptiveRoutingConfig(cfg),
		states: make(map[string]*adaptiveRuntimeState),
		rand:   rand.Float64,
	}
}

func normalizeAdaptiveRoutingConfig(cfg internalconfig.AdaptiveRoutingConfig) internalconfig.AdaptiveRoutingConfig {
	if cfg.TopK <= 0 {
		cfg.TopK = defaultAdaptiveTopK
	}
	if cfg.TopK > 64 {
		cfg.TopK = 64
	}
	if cfg.EWMAAlpha <= 0 || cfg.EWMAAlpha > 1 {
		cfg.EWMAAlpha = defaultAdaptiveEWMAAlpha
	}
	if cfg.TTFTTargetMS <= 0 {
		cfg.TTFTTargetMS = defaultAdaptiveTTFTTargetMS
	}
	if cfg.Weights.Priority <= 0 && cfg.Weights.Load <= 0 && cfg.Weights.SuccessRate <= 0 && cfg.Weights.TTFT <= 0 {
		cfg.Weights = internalconfig.AdaptiveRoutingWeights{
			Priority:    4,
			Load:        2,
			SuccessRate: 2,
			TTFT:        1,
		}
	}
	if cfg.StickyEscape.Enabled {
		if cfg.StickyEscape.MinSamples <= 0 {
			cfg.StickyEscape.MinSamples = 3
		}
		if cfg.StickyEscape.ErrorRateThreshold <= 0 || cfg.StickyEscape.ErrorRateThreshold > 1 {
			cfg.StickyEscape.ErrorRateThreshold = 0.5
		}
		if cfg.StickyEscape.TTFTThresholdMS <= 0 {
			cfg.StickyEscape.TTFTThresholdMS = cfg.TTFTTargetMS
		}
	}
	return cfg
}

// Config returns the normalized selector configuration.
func (s *AdaptiveSelector) Config() internalconfig.AdaptiveRoutingConfig {
	if s == nil {
		return normalizeAdaptiveRoutingConfig(internalconfig.AdaptiveRoutingConfig{})
	}
	return s.config
}

// Pick selects one available credential using weighted top-K scoring.
func (s *AdaptiveSelector) Pick(_ context.Context, provider, model string, _ cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	now := time.Now()
	available, errAvailable := getAvailableAuthsAcrossPriorities(auths, provider, model, now)
	if errAvailable != nil {
		return nil, errAvailable
	}
	scores := s.scoreEligible(available)
	if len(scores) == 0 {
		return nil, &Error{Code: "auth_not_found", Message: "no auth available"}
	}
	topK := s.config.TopK
	if topK > len(scores) {
		topK = len(scores)
	}
	if topK <= 1 {
		return scores[0].auth, nil
	}
	base := scores[topK-1].score
	total := 0.0
	weights := make([]float64, topK)
	for i := 0; i < topK; i++ {
		weight := scores[i].score - base + 0.05
		if weight < 0.05 {
			weight = 0.05
		}
		weights[i] = weight
		total += weight
	}
	random := rand.Float64
	if s.rand != nil {
		random = s.rand
	}
	target := random() * total
	for i, weight := range weights {
		target -= weight
		if target <= 0 {
			return scores[i].auth, nil
		}
	}
	return scores[topK-1].auth, nil
}

type adaptiveCandidateScore struct {
	auth  *Auth
	score float64
}

func getAvailableAuthsAcrossPriorities(auths []*Auth, provider, model string, now time.Time) ([]*Auth, error) {
	if len(auths) == 0 {
		return nil, &Error{Code: "auth_not_found", Message: "no auth candidates"}
	}
	available := make([]*Auth, 0, len(auths))
	cooldownCount := 0
	earliest := time.Time{}
	for _, auth := range auths {
		blocked, reason, next := isAuthBlockedForModel(auth, model, now)
		if !blocked {
			available = append(available, auth)
			continue
		}
		if reason == blockReasonCooldown {
			cooldownCount++
			if !next.IsZero() && (earliest.IsZero() || next.Before(earliest)) {
				earliest = next
			}
		}
	}
	if len(available) > 0 {
		return available, nil
	}
	if cooldownCount == len(auths) && !earliest.IsZero() {
		providerForError := provider
		if providerForError == "mixed" {
			providerForError = ""
		}
		return nil, newModelCooldownError(model, providerForError, earliest.Sub(now))
	}
	return nil, &Error{Code: "auth_unavailable", Message: "no auth available"}
}

func (s *AdaptiveSelector) scoreEligible(auths []*Auth) []adaptiveCandidateScore {
	if s == nil || len(auths) == 0 {
		return nil
	}
	minPriority, maxPriority := 0, 0
	for i, auth := range auths {
		priority := authPriority(auth)
		if i == 0 || priority < minPriority {
			minPriority = priority
		}
		if i == 0 || priority > maxPriority {
			maxPriority = priority
		}
	}
	s.mu.RLock()
	scores := make([]adaptiveCandidateScore, 0, len(auths))
	for _, auth := range auths {
		state := s.states[auth.ID]
		priorityScore := 1.0
		if maxPriority != minPriority {
			priorityScore = float64(authPriority(auth)-minPriority) / float64(maxPriority-minPriority)
		}
		active := 0
		successScore := 1.0
		ttftScore := 1.0
		if state != nil {
			active = state.active
			if state.outcomeSamples > 0 {
				successScore = clamp01(state.successEWMA)
			}
			if state.ttftSamples > 0 {
				ttftScore = 1 / (1 + state.ttftEWMA/float64(s.config.TTFTTargetMS))
			}
		}
		loadScore := 1 / (1 + float64(active))
		score := s.config.Weights.Priority*priorityScore +
			s.config.Weights.Load*loadScore +
			s.config.Weights.SuccessRate*successScore +
			s.config.Weights.TTFT*ttftScore
		scores = append(scores, adaptiveCandidateScore{auth: auth, score: score})
	}
	s.mu.RUnlock()
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].score == scores[j].score {
			return scores[i].auth.ID < scores[j].auth.ID
		}
		return scores[i].score > scores[j].score
	})
	return scores
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

// BeginAttempt increments active concurrency and returns an idempotent release callback.
func (s *AdaptiveSelector) BeginAttempt(authID string) func() {
	if s == nil || strings.TrimSpace(authID) == "" {
		return func() {}
	}
	s.mu.Lock()
	state := s.stateLocked(authID)
	state.active++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			if current := s.states[authID]; current != nil && current.active > 0 {
				current.active--
			}
			s.mu.Unlock()
		})
	}
}

// ObserveResult updates the success-rate EWMA from AuthManager execution outcomes.
func (s *AdaptiveSelector) ObserveResult(result Result) {
	if s == nil || strings.TrimSpace(result.AuthID) == "" {
		return
	}
	value := 0.0
	if result.Success {
		value = 1
	}
	s.mu.Lock()
	state := s.stateLocked(result.AuthID)
	state.successEWMA = updateEWMA(state.successEWMA, value, state.outcomeSamples, s.config.EWMAAlpha)
	state.outcomeSamples++
	state.lastSampleAt = time.Now().UTC()
	s.mu.Unlock()
}

// HandleUsage updates TTFT EWMA from executor usage records.
func (s *AdaptiveSelector) HandleUsage(_ context.Context, record coreusage.Record) {
	if s == nil || strings.TrimSpace(record.AuthID) == "" || record.TTFT <= 0 {
		return
	}
	s.mu.Lock()
	state := s.stateLocked(record.AuthID)
	value := float64(record.TTFT) / float64(time.Millisecond)
	state.ttftEWMA = updateEWMA(state.ttftEWMA, value, state.ttftSamples, s.config.EWMAAlpha)
	state.ttftSamples++
	state.lastSampleAt = time.Now().UTC()
	s.mu.Unlock()
}

func updateEWMA(current, next float64, samples int64, alpha float64) float64 {
	if samples == 0 || math.IsNaN(current) || math.IsInf(current, 0) {
		return next
	}
	return alpha*next + (1-alpha)*current
}

func (s *AdaptiveSelector) stateLocked(authID string) *adaptiveRuntimeState {
	state := s.states[authID]
	if state == nil {
		state = &adaptiveRuntimeState{}
		s.states[authID] = state
	}
	return state
}

// ShouldEscapeAuth reports whether session affinity should abandon an unhealthy binding.
func (s *AdaptiveSelector) ShouldEscapeAuth(authID string) bool {
	if s == nil || !s.config.StickyEscape.Enabled {
		return false
	}
	s.mu.RLock()
	state := s.states[authID]
	if state == nil {
		s.mu.RUnlock()
		return false
	}
	escape := false
	if s.config.StickyEscape.ActiveRequestThreshold > 0 && state.active >= s.config.StickyEscape.ActiveRequestThreshold {
		escape = true
	}
	if state.outcomeSamples >= s.config.StickyEscape.MinSamples && 1-clamp01(state.successEWMA) >= s.config.StickyEscape.ErrorRateThreshold {
		escape = true
	}
	if state.ttftSamples >= s.config.StickyEscape.MinSamples && state.ttftEWMA >= float64(s.config.StickyEscape.TTFTThresholdMS) {
		escape = true
	}
	s.mu.RUnlock()
	return escape
}

// Scores returns an on-demand score snapshot without changing selection state.
func (s *AdaptiveSelector) Scores(provider, model string, auths []*Auth) []AdaptiveScore {
	if s == nil {
		return nil
	}
	now := time.Now()
	eligible := make([]*Auth, 0, len(auths))
	eligibleSet := make(map[string]struct{}, len(auths))
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		if provider != "" && provider != "mixed" && !strings.EqualFold(executorKeyFromAuth(auth), provider) {
			continue
		}
		blocked, _, _ := isAuthBlockedForModel(auth, model, now)
		if !blocked {
			eligible = append(eligible, auth)
			eligibleSet[auth.ID] = struct{}{}
		}
	}
	ranked := s.scoreEligible(eligible)
	rankByID := make(map[string]int, len(ranked))
	scoreByID := make(map[string]float64, len(ranked))
	for i, item := range ranked {
		rankByID[item.auth.ID] = i + 1
		scoreByID[item.auth.ID] = item.score
	}

	s.mu.RLock()
	items := make([]AdaptiveScore, 0, len(auths))
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		if provider != "" && provider != "mixed" && !strings.EqualFold(executorKeyFromAuth(auth), provider) {
			continue
		}
		state := s.states[auth.ID]
		item := AdaptiveScore{
			AuthID:    auth.ID,
			AuthIndex: auth.Index,
			Provider:  executorKeyFromAuth(auth),
			Priority:  authPriority(auth),
			Score:     scoreByID[auth.ID],
			Rank:      rankByID[auth.ID],
		}
		_, item.Eligible = eligibleSet[auth.ID]
		if state != nil {
			item.ActiveRequests = state.active
			item.OutcomeSamples = state.outcomeSamples
			item.TTFTSamples = state.ttftSamples
			item.LastSampleAt = state.lastSampleAt
			if state.outcomeSamples > 0 {
				item.SuccessRate = clamp01(state.successEWMA)
				item.ErrorRate = 1 - item.SuccessRate
			} else {
				item.SuccessRate = 1
			}
			if state.ttftSamples > 0 {
				item.TTFTMS = state.ttftEWMA
			}
		} else {
			item.SuccessRate = 1
		}
		items = append(items, item)
	}
	s.mu.RUnlock()
	for i := range items {
		items[i].EscapeSticky = s.ShouldEscapeAuth(items[i].AuthID)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Eligible != items[j].Eligible {
			return items[i].Eligible
		}
		if items[i].Rank != items[j].Rank {
			if items[i].Rank == 0 {
				return false
			}
			if items[j].Rank == 0 {
				return true
			}
			return items[i].Rank < items[j].Rank
		}
		return items[i].AuthID < items[j].AuthID
	})
	return items
}

// AdaptiveScores exposes scores and normalized config through selector wrappers.
func (s *AdaptiveSelector) AdaptiveScores(provider, model string, auths []*Auth) ([]AdaptiveScore, internalconfig.AdaptiveRoutingConfig, bool) {
	if s == nil {
		return nil, internalconfig.AdaptiveRoutingConfig{}, false
	}
	return s.Scores(provider, model, auths), s.Config(), true
}
