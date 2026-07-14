package clientaccess

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	requestWindow5h  = 5 * time.Hour
	requestWindow1d  = 24 * time.Hour
	requestWindow7d  = 7 * 24 * time.Hour
	reservationTTL   = 6 * time.Hour
	settledRecordTTL = 24 * time.Hour
)

func resetUsageWindow(used *int64, startedAt **time.Time, duration time.Duration, now time.Time) {
	if used == nil || startedAt == nil {
		return
	}
	if *startedAt != nil && now.Before((*startedAt).Add(duration)) {
		return
	}
	*used = 0
	started := now.UTC()
	*startedAt = &started
}

func quotaResetAt(startedAt *time.Time, duration time.Duration) *time.Time {
	if startedAt == nil || startedAt.IsZero() {
		return nil
	}
	reset := startedAt.UTC().Add(duration)
	return &reset
}

func quotaExceeded(resource, window string, limit, used int64, resetAt *time.Time) error {
	return &QuotaExceededError{Resource: resource, Window: window, Limit: limit, Used: used, ResetAt: resetAt}
}

func checkRequestQuotas(key Key) error {
	if key.RequestLimitTotal > 0 && key.RequestUsedTotal >= key.RequestLimitTotal {
		return quotaExceeded("request", "total", key.RequestLimitTotal, key.RequestUsedTotal, nil)
	}
	if key.RequestLimit5h > 0 && key.RequestUsed5h >= key.RequestLimit5h {
		return quotaExceeded("request", "5h", key.RequestLimit5h, key.RequestUsed5h, quotaResetAt(key.RequestWindow5hAt, requestWindow5h))
	}
	if key.RequestLimit1d > 0 && key.RequestUsed1d >= key.RequestLimit1d {
		return quotaExceeded("request", "1d", key.RequestLimit1d, key.RequestUsed1d, quotaResetAt(key.RequestWindow1dAt, requestWindow1d))
	}
	if key.RequestLimit7d > 0 && key.RequestUsed7d >= key.RequestLimit7d {
		return quotaExceeded("request", "7d", key.RequestLimit7d, key.RequestUsed7d, quotaResetAt(key.RequestWindow7dAt, requestWindow7d))
	}
	return nil
}

func effectiveTokenReservation(key Key, activeReserved, requested int64) (int64, error) {
	type tokenWindow struct {
		name    string
		limit   int64
		used    int64
		resetAt *time.Time
	}
	windows := []tokenWindow{
		{name: "total", limit: key.TokenLimitTotal, used: key.TokenUsedTotal},
		{name: "5h", limit: key.TokenLimit5h, used: key.TokenUsed5h, resetAt: quotaResetAt(key.TokenWindow5hAt, requestWindow5h)},
		{name: "1d", limit: key.TokenLimit1d, used: key.TokenUsed1d, resetAt: quotaResetAt(key.TokenWindow1dAt, requestWindow1d)},
		{name: "7d", limit: key.TokenLimit7d, used: key.TokenUsed7d, resetAt: quotaResetAt(key.TokenWindow7dAt, requestWindow7d)},
	}
	limited := false
	remaining := int64(0)
	for _, window := range windows {
		if window.limit <= 0 {
			continue
		}
		current := window.used + activeReserved
		if current >= window.limit {
			return 0, quotaExceeded("token", window.name, window.limit, current, window.resetAt)
		}
		windowRemaining := window.limit - current
		if !limited || windowRemaining < remaining {
			remaining = windowRemaining
		}
		limited = true
	}
	if !limited {
		return 0, nil
	}
	if requested <= 0 {
		requested = 1
	}
	if requested > remaining {
		requested = remaining
	}
	return requested, nil
}

func resetKeyQuotaWindows(key *Key, now time.Time) {
	if key == nil {
		return
	}
	resetUsageWindow(&key.RequestUsed5h, &key.RequestWindow5hAt, requestWindow5h, now)
	resetUsageWindow(&key.RequestUsed1d, &key.RequestWindow1dAt, requestWindow1d, now)
	resetUsageWindow(&key.RequestUsed7d, &key.RequestWindow7dAt, requestWindow7d, now)
	resetUsageWindow(&key.TokenUsed5h, &key.TokenWindow5hAt, requestWindow5h, now)
	resetUsageWindow(&key.TokenUsed1d, &key.TokenWindow1dAt, requestWindow1d, now)
	resetUsageWindow(&key.TokenUsed7d, &key.TokenWindow7dAt, requestWindow7d, now)
}

func normalizeExpiredKeyUsage(key *Key, now time.Time) {
	if key == nil {
		return
	}
	normalize := func(used *int64, startedAt **time.Time, duration time.Duration) {
		if used == nil || startedAt == nil || *startedAt == nil || now.Before((*startedAt).Add(duration)) {
			return
		}
		*used = 0
		*startedAt = nil
	}
	normalize(&key.RequestUsed5h, &key.RequestWindow5hAt, requestWindow5h)
	normalize(&key.RequestUsed1d, &key.RequestWindow1dAt, requestWindow1d)
	normalize(&key.RequestUsed7d, &key.RequestWindow7dAt, requestWindow7d)
	normalize(&key.TokenUsed5h, &key.TokenWindow5hAt, requestWindow5h)
	normalize(&key.TokenUsed1d, &key.TokenWindow1dAt, requestWindow1d)
	normalize(&key.TokenUsed7d, &key.TokenWindow7dAt, requestWindow7d)
}

func updateKeyQuotaUsage(ctx context.Context, tx *sql.Tx, key Key, now time.Time) error {
	_, errExec := tx.ExecContext(ctx, `UPDATE client_access_keys SET
		request_used_total = ?, request_used_5h = ?, request_window_5h_ms = ?, request_used_1d = ?, request_window_1d_ms = ?, request_used_7d = ?, request_window_7d_ms = ?,
		token_used_total = ?, token_used_5h = ?, token_window_5h_ms = ?, token_used_1d = ?, token_window_1d_ms = ?, token_used_7d = ?, token_window_7d_ms = ?,
		last_used_at_ms = ?, updated_at_ms = ? WHERE id = ?`,
		key.RequestUsedTotal, key.RequestUsed5h, nullableMillis(key.RequestWindow5hAt), key.RequestUsed1d, nullableMillis(key.RequestWindow1dAt), key.RequestUsed7d, nullableMillis(key.RequestWindow7dAt),
		key.TokenUsedTotal, key.TokenUsed5h, nullableMillis(key.TokenWindow5hAt), key.TokenUsed1d, nullableMillis(key.TokenWindow1dAt), key.TokenUsed7d, nullableMillis(key.TokenWindow7dAt),
		now.UnixMilli(), now.UnixMilli(), key.ID)
	return errExec
}

// ReserveUsage atomically reserves one request and provisional tokens.
func (s *Store) ReserveUsage(ctx context.Context, keyID int64, reservationID string, requestedTokens int64, now time.Time) (Key, int64, error) {
	if s == nil || s.db == nil {
		return Key{}, 0, errors.New("client access store is unavailable")
	}
	if keyID <= 0 || reservationID == "" {
		return Key{}, 0, errors.New("invalid quota reservation")
	}
	now = now.UTC()
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return Key{}, 0, errBegin
	}
	defer func() { _ = tx.Rollback() }()
	if _, errCleanup := tx.ExecContext(ctx, `DELETE FROM client_access_token_reservations WHERE expires_at_ms <= ?`, now.UnixMilli()); errCleanup != nil {
		return Key{}, 0, errCleanup
	}
	key, _, errScan := scanKey(tx.QueryRowContext(ctx, `SELECT `+keyColumns+` FROM client_access_keys WHERE id = ?`, keyID))
	if errScan != nil {
		return Key{}, 0, errScan
	}
	resetKeyQuotaWindows(&key, now)
	if errQuota := checkRequestQuotas(key); errQuota != nil {
		return Key{}, 0, errQuota
	}
	var activeReserved int64
	if errReserved := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(reserved_tokens), 0) FROM client_access_token_reservations WHERE key_id = ? AND settled = 0 AND expires_at_ms > ?`, keyID, now.UnixMilli()).Scan(&activeReserved); errReserved != nil {
		return Key{}, 0, errReserved
	}
	effectiveReservation, errTokenQuota := effectiveTokenReservation(key, activeReserved, requestedTokens)
	if errTokenQuota != nil {
		return Key{}, 0, errTokenQuota
	}
	key.RequestUsedTotal++
	key.RequestUsed5h++
	key.RequestUsed1d++
	key.RequestUsed7d++
	if errUpdate := updateKeyQuotaUsage(ctx, tx, key, now); errUpdate != nil {
		return Key{}, 0, fmt.Errorf("reserve client quota: %w", errUpdate)
	}
	if _, errInsert := tx.ExecContext(ctx, `INSERT INTO client_access_token_reservations(id, key_id, reserved_tokens, settled, created_at_ms, updated_at_ms, expires_at_ms) VALUES(?, ?, ?, 0, ?, ?, ?)`, reservationID, keyID, effectiveReservation, now.UnixMilli(), now.UnixMilli(), now.Add(reservationTTL).UnixMilli()); errInsert != nil {
		return Key{}, 0, fmt.Errorf("create token reservation: %w", errInsert)
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return Key{}, 0, errCommit
	}
	key.TokenReserved = activeReserved + effectiveReservation
	return key, effectiveReservation, nil
}

func (s *Store) addTokenUsageTx(ctx context.Context, tx *sql.Tx, key *Key, actualTokens int64, now time.Time) error {
	if key == nil {
		return nil
	}
	if actualTokens < 0 {
		actualTokens = 0
	}
	resetKeyQuotaWindows(key, now)
	key.TokenUsedTotal += actualTokens
	key.TokenUsed5h += actualTokens
	key.TokenUsed1d += actualTokens
	key.TokenUsed7d += actualTokens
	return updateKeyQuotaUsage(ctx, tx, *key, now)
}

// SettleTokenReservation releases a reservation and records actual token usage.
func (s *Store) SettleTokenReservation(ctx context.Context, reservationID string, actualTokens int64, now time.Time) (bool, error) {
	if s == nil || s.db == nil || reservationID == "" {
		return false, nil
	}
	now = now.UTC()
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return false, errBegin
	}
	defer func() { _ = tx.Rollback() }()
	var keyID int64
	if errQuery := tx.QueryRowContext(ctx, `SELECT key_id FROM client_access_token_reservations WHERE id = ?`, reservationID).Scan(&keyID); errQuery != nil {
		if errors.Is(errQuery, sql.ErrNoRows) {
			return false, nil
		}
		return false, errQuery
	}
	key, _, errScan := scanKey(tx.QueryRowContext(ctx, `SELECT `+keyColumns+` FROM client_access_keys WHERE id = ?`, keyID))
	if errScan != nil {
		return false, errScan
	}
	if errAdd := s.addTokenUsageTx(ctx, tx, &key, actualTokens, now); errAdd != nil {
		return false, errAdd
	}
	if _, errUpdate := tx.ExecContext(ctx, `UPDATE client_access_token_reservations SET reserved_tokens = 0, settled = 1, updated_at_ms = ?, expires_at_ms = ? WHERE id = ?`, now.UnixMilli(), now.Add(settledRecordTTL).UnixMilli(), reservationID); errUpdate != nil {
		return false, errUpdate
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return false, errCommit
	}
	return true, nil
}

// AddTokenUsageByHash records usage when a request lacks a reservation correlation ID.
func (s *Store) AddTokenUsageByHash(ctx context.Context, hash string, actualTokens int64, now time.Time) (bool, error) {
	if s == nil || s.db == nil || hash == "" {
		return false, nil
	}
	now = now.UTC()
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return false, errBegin
	}
	defer func() { _ = tx.Rollback() }()
	key, _, errScan := scanKey(tx.QueryRowContext(ctx, `SELECT `+keyColumns+` FROM client_access_keys WHERE key_hash = ?`, hash))
	if errScan != nil {
		if errors.Is(errScan, sql.ErrNoRows) {
			return false, nil
		}
		return false, errScan
	}
	if errAdd := s.addTokenUsageTx(ctx, tx, &key, actualTokens, now); errAdd != nil {
		return false, errAdd
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return false, errCommit
	}
	return true, nil
}

func (s *Store) ReservedTokens(ctx context.Context, keyID int64, now time.Time) (int64, error) {
	if s == nil || s.db == nil || keyID <= 0 {
		return 0, nil
	}
	var reserved int64
	errQuery := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(reserved_tokens), 0) FROM client_access_token_reservations WHERE key_id = ? AND settled = 0 AND expires_at_ms > ?`, keyID, now.UTC().UnixMilli()).Scan(&reserved)
	return reserved, errQuery
}
