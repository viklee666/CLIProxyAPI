// Package tenantauth validates CPA tenant sessions for Manager Plus routes.
package tenantauth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrInvalidSession = errors.New("invalid tenant session")
	ErrUnavailable    = errors.New("tenant session store is unavailable")
)

type Subject struct {
	TenantID int64
}

type Service struct {
	tenantDBPath       string
	clientAccessDBPath string
}

func New(tenantDBPath, clientAccessDBPath string) *Service {
	return &Service{
		tenantDBPath:       strings.TrimSpace(tenantDBPath),
		clientAccessDBPath: strings.TrimSpace(clientAccessDBPath),
	}
}

func (s *Service) Authenticate(ctx context.Context, token string) (Subject, error) {
	if s == nil {
		return Subject{}, ErrUnavailable
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return Subject{}, ErrInvalidSession
	}
	db, errOpen := openReadOnly(s.tenantDBPath)
	if errOpen != nil {
		return Subject{}, fmt.Errorf("%w: %v", ErrUnavailable, errOpen)
	}
	defer db.Close()
	var tenantID int64
	errQuery := db.QueryRowContext(ctx, `SELECT t.id
		FROM tenant_sessions s JOIN tenants t ON t.id = s.tenant_id
		WHERE s.token_hash = ? AND s.expires_at_ms > ? AND t.enabled = 1`, hashToken(token), time.Now().UTC().UnixMilli()).Scan(&tenantID)
	if errors.Is(errQuery, sql.ErrNoRows) {
		return Subject{}, ErrInvalidSession
	}
	if errQuery != nil {
		return Subject{}, fmt.Errorf("%w: %v", ErrUnavailable, errQuery)
	}
	return Subject{TenantID: tenantID}, nil
}

// APIKeyHashes returns only hashes currently owned by tenantID. The caller
// must use the result as a mandatory query predicate; an empty result must
// never be interpreted as an unfiltered query.
func (s *Service) APIKeyHashes(ctx context.Context, tenantID int64) ([]string, error) {
	if s == nil || tenantID <= 0 {
		return nil, ErrUnavailable
	}
	db, errOpen := openReadOnly(s.clientAccessDBPath)
	if errOpen != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, errOpen)
	}
	defer db.Close()
	rows, errQuery := db.QueryContext(ctx, `SELECT key_hash FROM client_access_keys WHERE tenant_id = ? ORDER BY id`, tenantID)
	if errQuery != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, errQuery)
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var hash string
		if errScan := rows.Scan(&hash); errScan != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, errScan)
		}
		hash = strings.ToLower(strings.TrimSpace(hash))
		if hash != "" {
			result = append(result, hash)
		}
	}
	if errRows := rows.Err(); errRows != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, errRows)
	}
	return result, nil
}

func BearerToken(request *http.Request) string {
	if request == nil {
		return ""
	}
	parts := strings.Fields(strings.TrimSpace(request.Header.Get("Authorization")))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func openReadOnly(path string) (*sql.DB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("database path is empty")
	}
	absPath, errAbs := filepath.Abs(path)
	if errAbs != nil {
		return nil, errAbs
	}
	if _, errStat := os.Stat(absPath); errStat != nil {
		return nil, errStat
	}
	dsn := "file:" + filepath.ToSlash(absPath) + "?mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(1)"
	db, errOpen := sql.Open("sqlite", dsn)
	if errOpen != nil {
		return nil, errOpen
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}
