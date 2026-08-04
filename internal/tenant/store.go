package tenant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type tenantCredential struct {
	Tenant
	passwordHash string
}

func Open(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("tenant database path is empty")
	}
	abs, errAbs := filepath.Abs(path)
	if errAbs != nil {
		return nil, fmt.Errorf("resolve tenant database path: %w", errAbs)
	}
	if errMkdir := os.MkdirAll(filepath.Dir(abs), 0o700); errMkdir != nil {
		return nil, fmt.Errorf("create tenant database directory: %w", errMkdir)
	}
	dsn := "file:" + filepath.ToSlash(abs) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, errOpen := sql.Open("sqlite", dsn)
	if errOpen != nil {
		return nil, fmt.Errorf("open tenant database: %w", errOpen)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	store := &Store{db: db}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if errPing := db.PingContext(ctx); errPing != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping tenant database: %w", errPing)
	}
	if errMigrate := store.migrate(ctx); errMigrate != nil {
		_ = db.Close()
		return nil, errMigrate
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS tenants (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			display_name TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tenant_providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
			channel TEXT NOT NULL,
			name TEXT NOT NULL,
			base_url TEXT NOT NULL DEFAULT '',
			api_key_enc TEXT NOT NULL,
			proxy_url TEXT NOT NULL DEFAULT '',
			priority INTEGER NOT NULL DEFAULT 0,
			disabled INTEGER NOT NULL DEFAULT 0,
			headers_json TEXT NOT NULL DEFAULT '{}',
			models_json TEXT NOT NULL DEFAULT '[]',
			extra_json TEXT NOT NULL DEFAULT '{}',
			auth_index TEXT NOT NULL,
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS credential_ownership (
			auth_index TEXT PRIMARY KEY,
			tenant_id INTEGER NOT NULL REFERENCES tenants(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS tenant_sessions (
			token_hash TEXT PRIMARY KEY,
			tenant_id INTEGER NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
			created_at_ms INTEGER NOT NULL,
			expires_at_ms INTEGER NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_providers_auth_index ON tenant_providers(auth_index)`,
		`CREATE INDEX IF NOT EXISTS idx_tenant_providers_tenant ON tenant_providers(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tenant_sessions_expiry ON tenant_sessions(expires_at_ms)`,
	}
	for _, statement := range statements {
		if _, errExec := s.db.ExecContext(ctx, statement); errExec != nil {
			return fmt.Errorf("migrate tenant database: %w", errExec)
		}
	}
	return nil
}

func scanTenant(scanner interface{ Scan(...any) error }) (Tenant, error) {
	var item Tenant
	var enabled int
	var createdMS, updatedMS int64
	if errScan := scanner.Scan(&item.ID, &item.DisplayName, &enabled, &createdMS, &updatedMS); errScan != nil {
		return Tenant{}, errScan
	}
	item.Enabled = enabled != 0
	item.CreatedAt = time.UnixMilli(createdMS).UTC()
	item.UpdatedAt = time.UnixMilli(updatedMS).UTC()
	return item, nil
}

func (s *Store) createTenant(ctx context.Context, displayName, passwordHash string) (Tenant, error) {
	now := time.Now().UTC()
	result, errExec := s.db.ExecContext(ctx, `INSERT INTO tenants(display_name, password_hash, enabled, created_at_ms, updated_at_ms) VALUES(?, ?, 1, ?, ?)`, displayName, passwordHash, now.UnixMilli(), now.UnixMilli())
	if errExec != nil {
		return Tenant{}, fmt.Errorf("create tenant: %w", errExec)
	}
	id, errID := result.LastInsertId()
	if errID != nil {
		return Tenant{}, fmt.Errorf("read tenant id: %w", errID)
	}
	return s.GetTenant(ctx, id)
}

func (s *Store) GetTenant(ctx context.Context, id int64) (Tenant, error) {
	return scanTenant(s.db.QueryRowContext(ctx, `SELECT id, display_name, enabled, created_at_ms, updated_at_ms FROM tenants WHERE id = ?`, id))
}

func (s *Store) ListTenants(ctx context.Context) ([]Tenant, error) {
	rows, errQuery := s.db.QueryContext(ctx, `SELECT id, display_name, enabled, created_at_ms, updated_at_ms FROM tenants ORDER BY id`)
	if errQuery != nil {
		return nil, fmt.Errorf("list tenants: %w", errQuery)
	}
	defer rows.Close()
	items := make([]Tenant, 0)
	for rows.Next() {
		item, errScan := scanTenant(rows)
		if errScan != nil {
			return nil, fmt.Errorf("scan tenant: %w", errScan)
		}
		items = append(items, item)
	}
	if errRows := rows.Err(); errRows != nil {
		return nil, fmt.Errorf("iterate tenants: %w", errRows)
	}
	return items, nil
}

func (s *Store) UpdateTenant(ctx context.Context, id int64, input UpdateInput) (Tenant, error) {
	current, errGet := s.GetTenant(ctx, id)
	if errGet != nil {
		return Tenant{}, errGet
	}
	if input.DisplayName != nil {
		current.DisplayName = strings.TrimSpace(*input.DisplayName)
		if current.DisplayName == "" {
			return Tenant{}, errors.New("tenant display name is required")
		}
	}
	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return Tenant{}, errBegin
	}
	defer func() { _ = tx.Rollback() }()
	if _, errExec := tx.ExecContext(ctx, `UPDATE tenants SET display_name = ?, enabled = ?, updated_at_ms = ? WHERE id = ?`, current.DisplayName, boolInt(current.Enabled), time.Now().UTC().UnixMilli(), id); errExec != nil {
		return Tenant{}, fmt.Errorf("update tenant: %w", errExec)
	}
	if !current.Enabled {
		if _, errDelete := tx.ExecContext(ctx, `DELETE FROM tenant_sessions WHERE tenant_id = ?`, id); errDelete != nil {
			return Tenant{}, fmt.Errorf("revoke disabled tenant sessions: %w", errDelete)
		}
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return Tenant{}, errCommit
	}
	return s.GetTenant(ctx, id)
}

func (s *Store) DeleteTenant(ctx context.Context, id int64) error {
	result, errExec := s.db.ExecContext(ctx, `DELETE FROM tenants WHERE id = ?`, id)
	if errExec != nil {
		return fmt.Errorf("delete tenant: %w", errExec)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) listTenantCredentials(ctx context.Context) ([]tenantCredential, error) {
	rows, errQuery := s.db.QueryContext(ctx, `SELECT id, display_name, enabled, created_at_ms, updated_at_ms, password_hash FROM tenants ORDER BY id`)
	if errQuery != nil {
		return nil, fmt.Errorf("list tenant credentials: %w", errQuery)
	}
	defer rows.Close()
	items := make([]tenantCredential, 0)
	for rows.Next() {
		var item tenantCredential
		var enabled int
		var createdMS, updatedMS int64
		if errScan := rows.Scan(&item.ID, &item.DisplayName, &enabled, &createdMS, &updatedMS, &item.passwordHash); errScan != nil {
			return nil, fmt.Errorf("scan tenant credential: %w", errScan)
		}
		item.Enabled = enabled != 0
		item.CreatedAt = time.UnixMilli(createdMS).UTC()
		item.UpdatedAt = time.UnixMilli(updatedMS).UTC()
		items = append(items, item)
	}
	if errRows := rows.Err(); errRows != nil {
		return nil, fmt.Errorf("iterate tenant credentials: %w", errRows)
	}
	return items, nil
}

func (s *Store) credential(ctx context.Context, id int64) (tenantCredential, error) {
	var item tenantCredential
	var enabled int
	var createdMS, updatedMS int64
	if errScan := s.db.QueryRowContext(ctx, `SELECT id, display_name, enabled, created_at_ms, updated_at_ms, password_hash FROM tenants WHERE id = ?`, id).Scan(&item.ID, &item.DisplayName, &enabled, &createdMS, &updatedMS, &item.passwordHash); errScan != nil {
		return tenantCredential{}, errScan
	}
	item.Enabled = enabled != 0
	item.CreatedAt = time.UnixMilli(createdMS).UTC()
	item.UpdatedAt = time.UnixMilli(updatedMS).UTC()
	return item, nil
}

func (s *Store) replacePasswordAndRevokeSessions(ctx context.Context, tenantID int64, passwordHash string) error {
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return errBegin
	}
	defer func() { _ = tx.Rollback() }()
	result, errExec := tx.ExecContext(ctx, `UPDATE tenants SET password_hash = ?, updated_at_ms = ? WHERE id = ?`, passwordHash, time.Now().UTC().UnixMilli(), tenantID)
	if errExec != nil {
		return fmt.Errorf("update tenant password: %w", errExec)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	if _, errDelete := tx.ExecContext(ctx, `DELETE FROM tenant_sessions WHERE tenant_id = ?`, tenantID); errDelete != nil {
		return fmt.Errorf("revoke tenant sessions: %w", errDelete)
	}
	return tx.Commit()
}

func (s *Store) createSession(ctx context.Context, tokenHash string, tenantID int64, expiresAt time.Time) error {
	now := time.Now().UTC()
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return errBegin
	}
	defer func() { _ = tx.Rollback() }()
	if _, errCleanup := tx.ExecContext(ctx, `DELETE FROM tenant_sessions WHERE expires_at_ms <= ?`, now.UnixMilli()); errCleanup != nil {
		return fmt.Errorf("remove expired tenant sessions: %w", errCleanup)
	}
	if _, errExec := tx.ExecContext(ctx, `INSERT INTO tenant_sessions(token_hash, tenant_id, created_at_ms, expires_at_ms) VALUES(?, ?, ?, ?)`, tokenHash, tenantID, now.UnixMilli(), expiresAt.UTC().UnixMilli()); errExec != nil {
		return fmt.Errorf("create tenant session: %w", errExec)
	}
	return tx.Commit()
}

func (s *Store) tenantBySession(ctx context.Context, tokenHash string, now time.Time) (Tenant, error) {
	if _, errCleanup := s.db.ExecContext(ctx, `DELETE FROM tenant_sessions WHERE expires_at_ms <= ?`, now.UTC().UnixMilli()); errCleanup != nil {
		return Tenant{}, fmt.Errorf("remove expired tenant sessions: %w", errCleanup)
	}
	return scanTenant(s.db.QueryRowContext(ctx, `SELECT t.id, t.display_name, t.enabled, t.created_at_ms, t.updated_at_ms
		FROM tenant_sessions s JOIN tenants t ON t.id = s.tenant_id
		WHERE s.token_hash = ? AND s.expires_at_ms > ? AND t.enabled = 1`, tokenHash, now.UTC().UnixMilli()))
}

func (s *Store) revokeSessions(ctx context.Context, tenantID int64) error {
	_, errExec := s.db.ExecContext(ctx, `DELETE FROM tenant_sessions WHERE tenant_id = ?`, tenantID)
	return errExec
}

func (s *Store) revokeSession(ctx context.Context, tokenHash string) error {
	_, errExec := s.db.ExecContext(ctx, `DELETE FROM tenant_sessions WHERE token_hash = ?`, tokenHash)
	return errExec
}

func (s *Store) putCredentialOwnership(ctx context.Context, authIndex string, tenantID int64) error {
	_, errExec := s.db.ExecContext(ctx, `INSERT INTO credential_ownership(auth_index, tenant_id) VALUES(?, ?)
		ON CONFLICT(auth_index) DO UPDATE SET tenant_id = excluded.tenant_id`, authIndex, tenantID)
	if errExec != nil {
		return fmt.Errorf("set credential ownership: %w", errExec)
	}
	return nil
}

func (s *Store) deleteCredentialOwnership(ctx context.Context, authIndex string) error {
	_, errExec := s.db.ExecContext(ctx, `DELETE FROM credential_ownership WHERE auth_index = ?`, authIndex)
	if errExec != nil {
		return fmt.Errorf("remove credential ownership: %w", errExec)
	}
	return nil
}

func (s *Store) deleteCredentialOwnershipForTenant(ctx context.Context, tenantID int64) error {
	_, errExec := s.db.ExecContext(ctx, `DELETE FROM credential_ownership WHERE tenant_id = ?`, tenantID)
	if errExec != nil {
		return fmt.Errorf("remove tenant credential ownership: %w", errExec)
	}
	return nil
}

func (s *Store) listCredentialOwnership(ctx context.Context) (map[string]int64, error) {
	rows, errQuery := s.db.QueryContext(ctx, `SELECT auth_index, tenant_id FROM credential_ownership`)
	if errQuery != nil {
		return nil, fmt.Errorf("list credential ownership: %w", errQuery)
	}
	defer rows.Close()
	items := make(map[string]int64)
	for rows.Next() {
		var authIndex string
		var tenantID int64
		if errScan := rows.Scan(&authIndex, &tenantID); errScan != nil {
			return nil, fmt.Errorf("scan credential ownership: %w", errScan)
		}
		if strings.TrimSpace(authIndex) != "" && tenantID > 0 {
			items[authIndex] = tenantID
		}
	}
	if errRows := rows.Err(); errRows != nil {
		return nil, fmt.Errorf("iterate credential ownership: %w", errRows)
	}
	return items, nil
}

const providerColumns = `id, tenant_id, channel, name, base_url, api_key_enc, proxy_url, priority, disabled,
	headers_json, models_json, extra_json, auth_index, created_at_ms, updated_at_ms`

const providerColumnsQualified = `p.id, p.tenant_id, p.channel, p.name, p.base_url, p.api_key_enc, p.proxy_url, p.priority, p.disabled,
	p.headers_json, p.models_json, p.extra_json, p.auth_index, p.created_at_ms, p.updated_at_ms`

func scanProvider(scanner interface{ Scan(...any) error }) (providerRecord, error) {
	var item providerRecord
	var disabled int
	var createdMS, updatedMS int64
	if errScan := scanner.Scan(
		&item.ID,
		&item.TenantID,
		&item.Channel,
		&item.Name,
		&item.BaseURL,
		&item.apiKeyEnc,
		&item.ProxyURL,
		&item.Priority,
		&disabled,
		&item.headersJSON,
		&item.modelsJSON,
		&item.extraJSON,
		&item.AuthIndex,
		&createdMS,
		&updatedMS,
	); errScan != nil {
		return providerRecord{}, errScan
	}
	item.Disabled = disabled != 0
	item.CreatedAt = time.UnixMilli(createdMS).UTC()
	item.UpdatedAt = time.UnixMilli(updatedMS).UTC()
	if errDecode := item.decodeJSON(); errDecode != nil {
		return providerRecord{}, errDecode
	}
	return item, nil
}

func (p *providerRecord) decodeJSON() error {
	if p == nil {
		return nil
	}
	if p.headersJSON == "" {
		p.headersJSON = "{}"
	}
	if p.modelsJSON == "" {
		p.modelsJSON = "[]"
	}
	if p.extraJSON == "" {
		p.extraJSON = "{}"
	}
	if errUnmarshal := json.Unmarshal([]byte(p.headersJSON), &p.Headers); errUnmarshal != nil {
		return fmt.Errorf("decode provider headers: %w", errUnmarshal)
	}
	p.Models = json.RawMessage(p.modelsJSON)
	p.Extra = json.RawMessage(p.extraJSON)
	return nil
}

func (s *Store) createProvider(ctx context.Context, tenantID int64, input ProviderCreateInput, apiKeyEnc, headersJSON, modelsJSON, extraJSON, authIndex string) (providerRecord, error) {
	now := time.Now().UTC()
	result, errExec := s.db.ExecContext(ctx, `INSERT INTO tenant_providers(
		tenant_id, channel, name, base_url, api_key_enc, proxy_url, priority, disabled, headers_json, models_json, extra_json, auth_index, created_at_ms, updated_at_ms
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tenantID, input.Channel, input.Name, input.BaseURL, apiKeyEnc, input.ProxyURL, input.Priority, boolInt(input.Disabled), headersJSON, modelsJSON, extraJSON, authIndex, now.UnixMilli(), now.UnixMilli())
	if errExec != nil {
		return providerRecord{}, fmt.Errorf("create tenant provider: %w", errExec)
	}
	providerID, errID := result.LastInsertId()
	if errID != nil {
		return providerRecord{}, fmt.Errorf("read tenant provider id: %w", errID)
	}
	return s.getProvider(ctx, tenantID, providerID)
}

func (s *Store) getProvider(ctx context.Context, tenantID, providerID int64) (providerRecord, error) {
	return scanProvider(s.db.QueryRowContext(ctx, `SELECT `+providerColumns+` FROM tenant_providers WHERE id = ? AND tenant_id = ?`, providerID, tenantID))
}

func (s *Store) listProviders(ctx context.Context, tenantID int64) ([]providerRecord, error) {
	rows, errQuery := s.db.QueryContext(ctx, `SELECT `+providerColumns+` FROM tenant_providers WHERE tenant_id = ? ORDER BY id`, tenantID)
	if errQuery != nil {
		return nil, fmt.Errorf("list tenant providers: %w", errQuery)
	}
	defer rows.Close()
	items := make([]providerRecord, 0)
	for rows.Next() {
		item, errScan := scanProvider(rows)
		if errScan != nil {
			return nil, fmt.Errorf("scan tenant provider: %w", errScan)
		}
		items = append(items, item)
	}
	if errRows := rows.Err(); errRows != nil {
		return nil, fmt.Errorf("iterate tenant providers: %w", errRows)
	}
	return items, nil
}

func (s *Store) listProvidersForSynthesis(ctx context.Context) ([]providerRecord, error) {
	rows, errQuery := s.db.QueryContext(ctx, `SELECT `+providerColumnsQualified+`
		FROM tenant_providers p JOIN tenants t ON t.id = p.tenant_id
		WHERE t.enabled = 1 ORDER BY p.id`)
	if errQuery != nil {
		return nil, fmt.Errorf("list tenant providers for synthesis: %w", errQuery)
	}
	defer rows.Close()
	items := make([]providerRecord, 0)
	for rows.Next() {
		item, errScan := scanProvider(rows)
		if errScan != nil {
			return nil, fmt.Errorf("scan tenant provider for synthesis: %w", errScan)
		}
		items = append(items, item)
	}
	if errRows := rows.Err(); errRows != nil {
		return nil, fmt.Errorf("iterate tenant providers for synthesis: %w", errRows)
	}
	return items, nil
}

func (s *Store) updateProvider(ctx context.Context, item providerRecord) (providerRecord, error) {
	_, errExec := s.db.ExecContext(ctx, `UPDATE tenant_providers SET name = ?, base_url = ?, api_key_enc = ?, proxy_url = ?, priority = ?, disabled = ?, headers_json = ?, models_json = ?, extra_json = ?, updated_at_ms = ? WHERE id = ? AND tenant_id = ?`,
		item.Name, item.BaseURL, item.apiKeyEnc, item.ProxyURL, item.Priority, boolInt(item.Disabled), item.headersJSON, item.modelsJSON, item.extraJSON, time.Now().UTC().UnixMilli(), item.ID, item.TenantID)
	if errExec != nil {
		return providerRecord{}, fmt.Errorf("update tenant provider: %w", errExec)
	}
	return s.getProvider(ctx, item.TenantID, item.ID)
}

func (s *Store) deleteProvider(ctx context.Context, tenantID, providerID int64) error {
	result, errExec := s.db.ExecContext(ctx, `DELETE FROM tenant_providers WHERE id = ? AND tenant_id = ?`, providerID, tenantID)
	if errExec != nil {
		return fmt.Errorf("delete tenant provider: %w", errExec)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) updateProviderAuthIndex(ctx context.Context, providerID, tenantID int64, authIndex string) error {
	_, errExec := s.db.ExecContext(ctx, `UPDATE tenant_providers SET auth_index = ?, updated_at_ms = ? WHERE id = ? AND tenant_id = ?`, authIndex, time.Now().UTC().UnixMilli(), providerID, tenantID)
	if errExec != nil {
		return fmt.Errorf("update tenant provider auth index: %w", errExec)
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
