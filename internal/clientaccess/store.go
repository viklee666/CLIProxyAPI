package clientaccess

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db      *sql.DB
	quotaMu sync.Mutex
}

func Open(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("client access database path is empty")
	}
	abs, errAbs := filepath.Abs(path)
	if errAbs != nil {
		return nil, fmt.Errorf("resolve client access database path: %w", errAbs)
	}
	if errMkdir := os.MkdirAll(filepath.Dir(abs), 0o700); errMkdir != nil {
		return nil, fmt.Errorf("create client access database directory: %w", errMkdir)
	}
	dsn := "file:" + filepath.ToSlash(abs) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, errOpen := sql.Open("sqlite", dsn)
	if errOpen != nil {
		return nil, fmt.Errorf("open client access database: %w", errOpen)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	store := &Store{db: db}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if errPing := db.PingContext(ctx); errPing != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping client access database: %w", errPing)
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
		`CREATE TABLE IF NOT EXISTS client_access_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER,
			name TEXT NOT NULL COLLATE NOCASE UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS client_access_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER,
			name TEXT NOT NULL,
			key_secret TEXT NOT NULL DEFAULT '',
			key_prefix TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			enabled INTEGER NOT NULL DEFAULT 1,
			allow_all_groups INTEGER NOT NULL DEFAULT 1,
			allow_ungrouped INTEGER NOT NULL DEFAULT 0,
			expires_at_ms INTEGER,
			rpm_limit INTEGER NOT NULL DEFAULT 0,
			concurrency_limit INTEGER NOT NULL DEFAULT 0,
			request_limit_total INTEGER NOT NULL DEFAULT 0,
			request_used_total INTEGER NOT NULL DEFAULT 0,
			request_limit_5h INTEGER NOT NULL DEFAULT 0,
			request_used_5h INTEGER NOT NULL DEFAULT 0,
			request_window_5h_ms INTEGER,
			request_limit_1d INTEGER NOT NULL DEFAULT 0,
			request_used_1d INTEGER NOT NULL DEFAULT 0,
			request_window_1d_ms INTEGER,
			request_limit_7d INTEGER NOT NULL DEFAULT 0,
			request_used_7d INTEGER NOT NULL DEFAULT 0,
			request_window_7d_ms INTEGER,
			token_limit_total INTEGER NOT NULL DEFAULT 0,
			token_used_total INTEGER NOT NULL DEFAULT 0,
			token_limit_5h INTEGER NOT NULL DEFAULT 0,
			token_used_5h INTEGER NOT NULL DEFAULT 0,
			token_window_5h_ms INTEGER,
			token_limit_1d INTEGER NOT NULL DEFAULT 0,
			token_used_1d INTEGER NOT NULL DEFAULT 0,
			token_window_1d_ms INTEGER,
			token_limit_7d INTEGER NOT NULL DEFAULT 0,
			token_used_7d INTEGER NOT NULL DEFAULT 0,
			token_window_7d_ms INTEGER,
			last_used_at_ms INTEGER,
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS client_access_key_groups (
			key_id INTEGER NOT NULL REFERENCES client_access_keys(id) ON DELETE CASCADE,
			group_id INTEGER NOT NULL REFERENCES client_access_groups(id) ON DELETE CASCADE,
			PRIMARY KEY (key_id, group_id)
		)`,
		`CREATE TABLE IF NOT EXISTS client_access_credential_groups (
			auth_index TEXT NOT NULL,
			group_id INTEGER NOT NULL REFERENCES client_access_groups(id) ON DELETE CASCADE,
			priority INTEGER NOT NULL DEFAULT 0,
			created_at_ms INTEGER NOT NULL,
			PRIMARY KEY (auth_index, group_id)
		)`,
		`CREATE TABLE IF NOT EXISTS client_access_token_reservations (
			id TEXT PRIMARY KEY,
			key_id INTEGER NOT NULL REFERENCES client_access_keys(id) ON DELETE CASCADE,
			reserved_tokens INTEGER NOT NULL DEFAULT 0,
			settled INTEGER NOT NULL DEFAULT 0,
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL,
			expires_at_ms INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_client_access_keys_enabled ON client_access_keys(enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_client_access_groups_tenant ON client_access_groups(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_client_access_keys_tenant ON client_access_keys(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_client_access_key_groups_group ON client_access_key_groups(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_client_access_credential_groups_group ON client_access_credential_groups(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_client_access_token_reservations_key ON client_access_token_reservations(key_id, settled, expires_at_ms)`,
	}
	for _, statement := range statements {
		if _, errExec := s.db.ExecContext(ctx, statement); errExec != nil {
			return fmt.Errorf("migrate client access database: %w", errExec)
		}
	}
	if errColumn := s.ensureColumn(ctx, "client_access_keys", "key_secret", "TEXT NOT NULL DEFAULT ''"); errColumn != nil {
		return errColumn
	}
	if errColumn := s.ensureColumn(ctx, "client_access_groups", "tenant_id", "INTEGER"); errColumn != nil {
		return errColumn
	}
	if errColumn := s.ensureColumn(ctx, "client_access_keys", "tenant_id", "INTEGER"); errColumn != nil {
		return errColumn
	}
	return nil
}

func (s *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, errQuery := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if errQuery != nil {
		return fmt.Errorf("inspect %s columns: %w", table, errQuery)
	}
	found := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if errScan := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); errScan != nil {
			_ = rows.Close()
			return fmt.Errorf("inspect %s columns: %w", table, errScan)
		}
		if name == column {
			found = true
		}
	}
	if errRows := rows.Err(); errRows != nil {
		_ = rows.Close()
		return fmt.Errorf("inspect %s columns: %w", table, errRows)
	}
	if errClose := rows.Close(); errClose != nil {
		return fmt.Errorf("inspect %s columns: %w", table, errClose)
	}
	if found {
		return nil
	}
	if _, errExec := s.db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+definition); errExec != nil {
		return fmt.Errorf("add %s.%s column: %w", table, column, errExec)
	}
	return nil
}

func normalizeListOptions(opts ListOptions) ListOptions {
	if opts.Page <= 0 {
		opts.Page = 1
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 20
	}
	if opts.PageSize > 200 {
		opts.PageSize = 200
	}
	opts.Search = strings.TrimSpace(opts.Search)
	return opts
}

const (
	sqliteVariableChunkSize  = 400
	sqliteInsertRowsPerBatch = 200
)

func nullableMillis(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC().UnixMilli()
}

func nullableTenantID(tenantID int64) any {
	if tenantID <= 0 {
		return nil
	}
	return tenantID
}

func appendTenantScope(where *string, args *[]any, column string, tenantID *int64) {
	if tenantID == nil {
		return
	}
	if *tenantID > 0 {
		*where += " AND " + column + " = ?"
		*args = append(*args, *tenantID)
		return
	}
	*where += " AND " + column + " IS NULL"
}

func tenantScopeCondition(column string, tenantID int64) (string, []any) {
	if tenantID > 0 {
		return " AND " + column + " = ?", []any{tenantID}
	}
	return " AND " + column + " IS NULL", nil
}

func timeFromNullMillis(value sql.NullInt64) *time.Time {
	if !value.Valid || value.Int64 <= 0 {
		return nil
	}
	parsed := time.UnixMilli(value.Int64).UTC()
	return &parsed
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Store) CreateGroup(ctx context.Context, input GroupCreate) (Group, error) {
	if input.TenantID < 0 {
		return Group{}, errors.New("tenant id cannot be negative")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Group{}, errors.New("group name is required")
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now := time.Now().UTC()
	result, errExec := s.db.ExecContext(ctx, `INSERT INTO client_access_groups(tenant_id, name, description, enabled, created_at_ms, updated_at_ms) VALUES(?, ?, ?, ?, ?, ?)`, nullableTenantID(input.TenantID), name, strings.TrimSpace(input.Description), boolInt(enabled), now.UnixMilli(), now.UnixMilli())
	if errExec != nil {
		return Group{}, fmt.Errorf("create client group: %w", errExec)
	}
	id, errID := result.LastInsertId()
	if errID != nil {
		return Group{}, fmt.Errorf("read client group id: %w", errID)
	}
	return s.GetGroupScoped(ctx, id, input.TenantID)
}

func (s *Store) GetGroup(ctx context.Context, id int64) (Group, error) {
	return s.getGroup(ctx, id, nil)
}

func (s *Store) GetGroupScoped(ctx context.Context, id, tenantID int64) (Group, error) {
	return s.getGroup(ctx, id, &tenantID)
}

func (s *Store) getGroup(ctx context.Context, id int64, tenantID *int64) (Group, error) {
	var item Group
	var enabled int
	var storedTenantID sql.NullInt64
	var createdMS, updatedMS int64
	where := " WHERE g.id = ?"
	args := []any{id}
	appendTenantScope(&where, &args, "g.tenant_id", tenantID)
	errScan := s.db.QueryRowContext(ctx, `SELECT g.id, g.tenant_id, g.name, g.description, g.enabled, g.created_at_ms, g.updated_at_ms,
		(SELECT COUNT(*) FROM client_access_key_groups kg WHERE kg.group_id = g.id),
		(SELECT COUNT(*) FROM client_access_credential_groups cg WHERE cg.group_id = g.id)
		FROM client_access_groups g`+where, args...).Scan(&item.ID, &storedTenantID, &item.Name, &item.Description, &enabled, &createdMS, &updatedMS, &item.KeyCount, &item.CredentialCount)
	if errScan != nil {
		return Group{}, errScan
	}
	item.Enabled = enabled != 0
	if storedTenantID.Valid && storedTenantID.Int64 > 0 {
		item.TenantID = storedTenantID.Int64
	}
	item.CreatedAt = time.UnixMilli(createdMS).UTC()
	item.UpdatedAt = time.UnixMilli(updatedMS).UTC()
	return item, nil
}

func (s *Store) ListGroups(ctx context.Context, opts ListOptions) (Page[Group], error) {
	opts = normalizeListOptions(opts)
	where := " WHERE 1=1"
	args := make([]any, 0, 3)
	appendTenantScope(&where, &args, "tenant_id", opts.TenantID)
	if opts.Search != "" {
		where += " AND (name LIKE ? OR description LIKE ?)"
		like := "%" + opts.Search + "%"
		args = append(args, like, like)
	}
	if opts.Enabled != nil {
		where += " AND enabled = ?"
		args = append(args, boolInt(*opts.Enabled))
	}
	var total int64
	if errCount := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM client_access_groups"+where, args...).Scan(&total); errCount != nil {
		return Page[Group]{}, fmt.Errorf("count client groups: %w", errCount)
	}
	queryArgs := append(append([]any(nil), args...), opts.PageSize, (opts.Page-1)*opts.PageSize)
	rows, errQuery := s.db.QueryContext(ctx, `WITH page AS (
		SELECT id, tenant_id, name, description, enabled, created_at_ms, updated_at_ms
		FROM client_access_groups`+where+`
		ORDER BY name COLLATE NOCASE ASC, id ASC
		LIMIT ? OFFSET ?
	), key_counts AS (
		SELECT group_id, COUNT(*) AS key_count
		FROM client_access_key_groups
		WHERE group_id IN (SELECT id FROM page)
		GROUP BY group_id
	), credential_counts AS (
		SELECT group_id, COUNT(*) AS credential_count
		FROM client_access_credential_groups
		WHERE group_id IN (SELECT id FROM page)
		GROUP BY group_id
	)
	SELECT p.id, p.tenant_id, p.name, p.description, p.enabled, p.created_at_ms, p.updated_at_ms,
		COALESCE(k.key_count, 0), COALESCE(c.credential_count, 0)
	FROM page p
	LEFT JOIN key_counts k ON k.group_id = p.id
	LEFT JOIN credential_counts c ON c.group_id = p.id
	ORDER BY p.name COLLATE NOCASE ASC, p.id ASC`, queryArgs...)
	if errQuery != nil {
		return Page[Group]{}, fmt.Errorf("list client groups: %w", errQuery)
	}
	defer rows.Close()
	items := make([]Group, 0, opts.PageSize)
	for rows.Next() {
		var item Group
		var enabled int
		var tenantID sql.NullInt64
		var createdMS, updatedMS int64
		if errScan := rows.Scan(&item.ID, &tenantID, &item.Name, &item.Description, &enabled, &createdMS, &updatedMS, &item.KeyCount, &item.CredentialCount); errScan != nil {
			return Page[Group]{}, fmt.Errorf("scan client group: %w", errScan)
		}
		item.Enabled = enabled != 0
		if tenantID.Valid && tenantID.Int64 > 0 {
			item.TenantID = tenantID.Int64
		}
		item.CreatedAt = time.UnixMilli(createdMS).UTC()
		item.UpdatedAt = time.UnixMilli(updatedMS).UTC()
		items = append(items, item)
	}
	if errRows := rows.Err(); errRows != nil {
		return Page[Group]{}, fmt.Errorf("iterate client groups: %w", errRows)
	}
	return Page[Group]{Items: items, Total: total, Page: opts.Page, PageSize: opts.PageSize}, nil
}

func (s *Store) UpdateGroup(ctx context.Context, id int64, input GroupUpdate) (Group, error) {
	return s.updateGroup(ctx, id, nil, input)
}

func (s *Store) UpdateGroupScoped(ctx context.Context, id, tenantID int64, input GroupUpdate) (Group, error) {
	return s.updateGroup(ctx, id, &tenantID, input)
}

func (s *Store) updateGroup(ctx context.Context, id int64, tenantID *int64, input GroupUpdate) (Group, error) {
	current, errGet := s.getGroup(ctx, id, tenantID)
	if errGet != nil {
		return Group{}, errGet
	}
	if input.Name != nil {
		current.Name = strings.TrimSpace(*input.Name)
		if current.Name == "" {
			return Group{}, errors.New("group name is required")
		}
	}
	if input.Description != nil {
		current.Description = strings.TrimSpace(*input.Description)
	}
	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}
	where := " WHERE id = ?"
	args := []any{current.Name, current.Description, boolInt(current.Enabled), time.Now().UTC().UnixMilli(), id}
	appendTenantScope(&where, &args, "tenant_id", tenantID)
	_, errExec := s.db.ExecContext(ctx, `UPDATE client_access_groups SET name = ?, description = ?, enabled = ?, updated_at_ms = ?`+where, args...)
	if errExec != nil {
		return Group{}, fmt.Errorf("update client group: %w", errExec)
	}
	return s.getGroup(ctx, id, tenantID)
}

func (s *Store) DeleteGroup(ctx context.Context, id int64) error {
	return s.deleteGroup(ctx, id, nil)
}

func (s *Store) DeleteGroupScoped(ctx context.Context, id, tenantID int64) error {
	return s.deleteGroup(ctx, id, &tenantID)
}

func (s *Store) deleteGroup(ctx context.Context, id int64, tenantID *int64) error {
	where := " WHERE id = ?"
	args := []any{id}
	appendTenantScope(&where, &args, "tenant_id", tenantID)
	result, errExec := s.db.ExecContext(ctx, `DELETE FROM client_access_groups`+where, args...)
	if errExec != nil {
		return fmt.Errorf("delete client group: %w", errExec)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type keyRowScanner interface {
	Scan(dest ...any) error
}

const keyColumns = `id, tenant_id, name, key_secret, key_prefix, key_hash, enabled, allow_all_groups, allow_ungrouped, expires_at_ms,
	rpm_limit, concurrency_limit,
	request_limit_total, request_used_total, request_limit_5h, request_used_5h, request_window_5h_ms,
	request_limit_1d, request_used_1d, request_window_1d_ms, request_limit_7d, request_used_7d, request_window_7d_ms,
	token_limit_total, token_used_total, token_limit_5h, token_used_5h, token_window_5h_ms,
	token_limit_1d, token_used_1d, token_window_1d_ms, token_limit_7d, token_used_7d, token_window_7d_ms,
	last_used_at_ms, created_at_ms, updated_at_ms`

func scanKey(scanner keyRowScanner) (Key, string, error) {
	var item Key
	var hash string
	var enabled, allowAll, allowUngrouped int
	var tenantID, expiresMS, requestWindow5hMS, requestWindow1dMS, requestWindow7dMS sql.NullInt64
	var tokenWindow5hMS, tokenWindow1dMS, tokenWindow7dMS, lastUsedMS sql.NullInt64
	var createdMS, updatedMS int64
	errScan := scanner.Scan(
		&item.ID, &tenantID, &item.Name, &item.Secret, &item.KeyPrefix, &hash, &enabled, &allowAll, &allowUngrouped, &expiresMS,
		&item.RPMLimit, &item.ConcurrencyLimit,
		&item.RequestLimitTotal, &item.RequestUsedTotal, &item.RequestLimit5h, &item.RequestUsed5h, &requestWindow5hMS,
		&item.RequestLimit1d, &item.RequestUsed1d, &requestWindow1dMS, &item.RequestLimit7d, &item.RequestUsed7d, &requestWindow7dMS,
		&item.TokenLimitTotal, &item.TokenUsedTotal, &item.TokenLimit5h, &item.TokenUsed5h, &tokenWindow5hMS,
		&item.TokenLimit1d, &item.TokenUsed1d, &tokenWindow1dMS, &item.TokenLimit7d, &item.TokenUsed7d, &tokenWindow7dMS,
		&lastUsedMS, &createdMS, &updatedMS,
	)
	if errScan != nil {
		return Key{}, "", errScan
	}
	item.Enabled = enabled != 0
	if tenantID.Valid && tenantID.Int64 > 0 {
		item.TenantID = tenantID.Int64
	}
	item.AllowAllGroups = allowAll != 0
	item.AllowUngrouped = allowUngrouped != 0
	item.ExpiresAt = timeFromNullMillis(expiresMS)
	item.RequestWindow5hAt = timeFromNullMillis(requestWindow5hMS)
	item.RequestWindow1dAt = timeFromNullMillis(requestWindow1dMS)
	item.RequestWindow7dAt = timeFromNullMillis(requestWindow7dMS)
	item.TokenWindow5hAt = timeFromNullMillis(tokenWindow5hMS)
	item.TokenWindow1dAt = timeFromNullMillis(tokenWindow1dMS)
	item.TokenWindow7dAt = timeFromNullMillis(tokenWindow7dMS)
	item.LastUsedAt = timeFromNullMillis(lastUsedMS)
	item.CreatedAt = time.UnixMilli(createdMS).UTC()
	item.UpdatedAt = time.UnixMilli(updatedMS).UTC()
	item.KeyMask = item.KeyPrefix + "…"
	return item, hash, nil
}

func (s *Store) keyGroupIDs(ctx context.Context, keyID int64) ([]int64, error) {
	rows, errQuery := s.db.QueryContext(ctx, `SELECT group_id FROM client_access_key_groups WHERE key_id = ? ORDER BY group_id`, keyID)
	if errQuery != nil {
		return nil, errQuery
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if errScan := rows.Scan(&id); errScan != nil {
			return nil, errScan
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func normalizePositiveIDs(values []int64) []int64 {
	result := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func validateGroupsForTenant(ctx context.Context, tx *sql.Tx, tenantID int64, groupIDs []int64) error {
	groupIDs = normalizePositiveIDs(groupIDs)
	if len(groupIDs) == 0 {
		return nil
	}
	for start := 0; start < len(groupIDs); start += sqliteVariableChunkSize {
		end := start + sqliteVariableChunkSize
		if end > len(groupIDs) {
			end = len(groupIDs)
		}
		chunk := groupIDs[start:end]
		placeholders := strings.TrimRight(strings.Repeat("?,", len(chunk)), ",")
		args := make([]any, 0, len(chunk)+1)
		for _, groupID := range chunk {
			args = append(args, groupID)
		}
		condition, tenantArgs := tenantScopeCondition("tenant_id", tenantID)
		args = append(args, tenantArgs...)
		var count int
		if errCount := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM client_access_groups WHERE id IN (`+placeholders+`)`+condition, args...).Scan(&count); errCount != nil {
			return errCount
		}
		if count != len(chunk) {
			return sql.ErrNoRows
		}
	}
	return nil
}

func (s *Store) replaceKeyGroups(ctx context.Context, tx *sql.Tx, keyID, tenantID int64, groupIDs []int64) error {
	groupIDs = normalizePositiveIDs(groupIDs)
	if errValidate := validateGroupsForTenant(ctx, tx, tenantID, groupIDs); errValidate != nil {
		return errValidate
	}
	if _, errDelete := tx.ExecContext(ctx, `DELETE FROM client_access_key_groups WHERE key_id = ?`, keyID); errDelete != nil {
		return errDelete
	}
	for _, groupID := range groupIDs {
		if _, errInsert := tx.ExecContext(ctx, `INSERT INTO client_access_key_groups(key_id, group_id) VALUES(?, ?)`, keyID, groupID); errInsert != nil {
			return errInsert
		}
	}
	return nil
}

func (s *Store) CreateKey(ctx context.Context, input KeyCreate, secret, prefix, hash string) (Key, error) {
	if input.TenantID < 0 {
		return Key{}, errors.New("tenant id cannot be negative")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Key{}, errors.New("key name is required")
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	allowAll := true
	if input.AllowAllGroups != nil {
		allowAll = *input.AllowAllGroups
	}
	if errLimits := validateLimits(int64(input.RPMLimit), int64(input.ConcurrencyLimit), input.RequestLimitTotal, input.RequestLimit5h, input.RequestLimit1d, input.RequestLimit7d, input.TokenLimitTotal, input.TokenLimit5h, input.TokenLimit1d, input.TokenLimit7d); errLimits != nil {
		return Key{}, errLimits
	}
	now := time.Now().UTC()
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return Key{}, errBegin
	}
	defer func() { _ = tx.Rollback() }()
	result, errExec := tx.ExecContext(ctx, `INSERT INTO client_access_keys(
		tenant_id, name, key_secret, key_prefix, key_hash, enabled, allow_all_groups, allow_ungrouped, expires_at_ms, rpm_limit, concurrency_limit,
		request_limit_total, request_limit_5h, request_limit_1d, request_limit_7d,
		token_limit_total, token_limit_5h, token_limit_1d, token_limit_7d, created_at_ms, updated_at_ms
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, nullableTenantID(input.TenantID), name, secret, prefix, hash, boolInt(enabled), boolInt(allowAll), boolInt(input.AllowUngrouped), nullableMillis(input.ExpiresAt), input.RPMLimit, input.ConcurrencyLimit, input.RequestLimitTotal, input.RequestLimit5h, input.RequestLimit1d, input.RequestLimit7d, input.TokenLimitTotal, input.TokenLimit5h, input.TokenLimit1d, input.TokenLimit7d, now.UnixMilli(), now.UnixMilli())
	if errExec != nil {
		return Key{}, fmt.Errorf("create client key: %w", errExec)
	}
	keyID, errID := result.LastInsertId()
	if errID != nil {
		return Key{}, errID
	}
	if errGroups := s.replaceKeyGroups(ctx, tx, keyID, input.TenantID, input.GroupIDs); errGroups != nil {
		return Key{}, fmt.Errorf("assign client key groups: %w", errGroups)
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return Key{}, errCommit
	}
	return s.GetKey(ctx, keyID)
}

func validateLimits(values ...int64) error {
	for _, value := range values {
		if value < 0 {
			return errors.New("limits cannot be negative")
		}
	}
	return nil
}

func (s *Store) GetKey(ctx context.Context, id int64) (Key, error) {
	return s.getKey(ctx, id, nil)
}

func (s *Store) GetKeyScoped(ctx context.Context, id, tenantID int64) (Key, error) {
	return s.getKey(ctx, id, &tenantID)
}

func (s *Store) getKey(ctx context.Context, id int64, tenantID *int64) (Key, error) {
	where := " WHERE id = ?"
	args := []any{id}
	appendTenantScope(&where, &args, "tenant_id", tenantID)
	item, _, errScan := scanKey(s.db.QueryRowContext(ctx, `SELECT `+keyColumns+` FROM client_access_keys`+where, args...))
	if errScan != nil {
		return Key{}, errScan
	}
	ids, errGroups := s.keyGroupIDs(ctx, item.ID)
	if errGroups != nil {
		return Key{}, errGroups
	}
	item.GroupIDs = ids
	normalizeExpiredKeyUsage(&item, time.Now().UTC())
	reserved, errReserved := s.ReservedTokens(ctx, item.ID, time.Now())
	if errReserved != nil {
		return Key{}, errReserved
	}
	item.TokenReserved = reserved
	return item, nil
}

type storedKey struct {
	Key
	Hash string
}

func (s *Store) ListAllStoredKeys(ctx context.Context) ([]storedKey, error) {
	rows, errQuery := s.db.QueryContext(ctx, `SELECT `+keyColumns+` FROM client_access_keys ORDER BY id`)
	if errQuery != nil {
		return nil, errQuery
	}
	items := make([]storedKey, 0)
	now := time.Now().UTC()
	for rows.Next() {
		item, hash, errScan := scanKey(rows)
		if errScan != nil {
			_ = rows.Close()
			return nil, errScan
		}
		normalizeExpiredKeyUsage(&item, now)
		items = append(items, storedKey{Key: item, Hash: hash})
	}
	if errRows := rows.Err(); errRows != nil {
		_ = rows.Close()
		return nil, errRows
	}
	if errClose := rows.Close(); errClose != nil {
		return nil, errClose
	}
	groupRows, errGroups := s.db.QueryContext(ctx, `SELECT key_id, group_id FROM client_access_key_groups ORDER BY key_id, group_id`)
	if errGroups != nil {
		return nil, errGroups
	}
	positions := make(map[int64]int, len(items))
	for index := range items {
		positions[items[index].ID] = index
	}
	for groupRows.Next() {
		var keyID, groupID int64
		if errScan := groupRows.Scan(&keyID, &groupID); errScan != nil {
			_ = groupRows.Close()
			return nil, errScan
		}
		if index, ok := positions[keyID]; ok {
			items[index].GroupIDs = append(items[index].GroupIDs, groupID)
		}
	}
	if errRows := groupRows.Err(); errRows != nil {
		_ = groupRows.Close()
		return nil, errRows
	}
	if errClose := groupRows.Close(); errClose != nil {
		return nil, errClose
	}
	return items, nil
}

func (s *Store) ListKeys(ctx context.Context, opts ListOptions) (Page[Key], error) {
	opts = normalizeListOptions(opts)
	where := " WHERE 1=1"
	args := make([]any, 0, 3)
	appendTenantScope(&where, &args, "tenant_id", opts.TenantID)
	if opts.Search != "" {
		where += " AND (name LIKE ? OR key_prefix LIKE ?)"
		like := "%" + opts.Search + "%"
		args = append(args, like, like)
	}
	if opts.Enabled != nil {
		where += " AND enabled = ?"
		args = append(args, boolInt(*opts.Enabled))
	}
	var total int64
	if errCount := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM client_access_keys"+where, args...).Scan(&total); errCount != nil {
		return Page[Key]{}, errCount
	}
	queryArgs := append(append([]any(nil), args...), opts.PageSize, (opts.Page-1)*opts.PageSize)
	rows, errQuery := s.db.QueryContext(ctx, `SELECT `+keyColumns+` FROM client_access_keys`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if errQuery != nil {
		return Page[Key]{}, errQuery
	}
	items := make([]Key, 0, opts.PageSize)
	for rows.Next() {
		item, _, errScan := scanKey(rows)
		if errScan != nil {
			_ = rows.Close()
			return Page[Key]{}, errScan
		}
		items = append(items, item)
	}
	if errRows := rows.Err(); errRows != nil {
		_ = rows.Close()
		return Page[Key]{}, errRows
	}
	if errClose := rows.Close(); errClose != nil {
		return Page[Key]{}, errClose
	}
	if errRelations := s.populateKeyRelations(ctx, items, time.Now().UTC()); errRelations != nil {
		return Page[Key]{}, errRelations
	}
	return Page[Key]{Items: items, Total: total, Page: opts.Page, PageSize: opts.PageSize}, nil
}

func (s *Store) populateKeyRelations(ctx context.Context, items []Key, now time.Time) error {
	if len(items) == 0 {
		return nil
	}
	positions := make(map[int64]int, len(items))
	ids := make([]int64, 0, len(items))
	for index := range items {
		positions[items[index].ID] = index
		ids = append(ids, items[index].ID)
	}
	for start := 0; start < len(ids); start += sqliteVariableChunkSize {
		end := start + sqliteVariableChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		placeholders := strings.TrimRight(strings.Repeat("?,", len(chunk)), ",")
		args := make([]any, 0, len(chunk))
		for _, id := range chunk {
			args = append(args, id)
		}
		groupRows, errGroups := s.db.QueryContext(ctx, `SELECT key_id, group_id FROM client_access_key_groups WHERE key_id IN (`+placeholders+`) ORDER BY key_id, group_id`, args...)
		if errGroups != nil {
			return errGroups
		}
		for groupRows.Next() {
			var keyID, groupID int64
			if errScan := groupRows.Scan(&keyID, &groupID); errScan != nil {
				_ = groupRows.Close()
				return errScan
			}
			if index, ok := positions[keyID]; ok {
				items[index].GroupIDs = append(items[index].GroupIDs, groupID)
			}
		}
		if errRows := groupRows.Err(); errRows != nil {
			_ = groupRows.Close()
			return errRows
		}
		if errClose := groupRows.Close(); errClose != nil {
			return errClose
		}

		reservationArgs := append(append([]any(nil), args...), now.UnixMilli())
		reservationRows, errReservations := s.db.QueryContext(ctx, `SELECT key_id, COALESCE(SUM(reserved_tokens), 0)
			FROM client_access_token_reservations
			WHERE key_id IN (`+placeholders+`) AND settled = 0 AND expires_at_ms > ?
			GROUP BY key_id`, reservationArgs...)
		if errReservations != nil {
			return errReservations
		}
		for reservationRows.Next() {
			var keyID, reserved int64
			if errScan := reservationRows.Scan(&keyID, &reserved); errScan != nil {
				_ = reservationRows.Close()
				return errScan
			}
			if index, ok := positions[keyID]; ok {
				items[index].TokenReserved = reserved
			}
		}
		if errRows := reservationRows.Err(); errRows != nil {
			_ = reservationRows.Close()
			return errRows
		}
		if errClose := reservationRows.Close(); errClose != nil {
			return errClose
		}
	}
	return nil
}

func (s *Store) UpdateKey(ctx context.Context, id int64, input KeyUpdate) (Key, error) {
	return s.updateKey(ctx, id, nil, input)
}

func (s *Store) UpdateKeyScoped(ctx context.Context, id, tenantID int64, input KeyUpdate) (Key, error) {
	return s.updateKey(ctx, id, &tenantID, input)
}

func (s *Store) updateKey(ctx context.Context, id int64, tenantID *int64, input KeyUpdate) (Key, error) {
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	current, errGet := s.getKey(ctx, id, tenantID)
	if errGet != nil {
		return Key{}, errGet
	}
	if input.Name != nil {
		current.Name = strings.TrimSpace(*input.Name)
		if current.Name == "" {
			return Key{}, errors.New("key name is required")
		}
	}
	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}
	if input.AllowAllGroups != nil {
		current.AllowAllGroups = *input.AllowAllGroups
	}
	if input.AllowUngrouped != nil {
		current.AllowUngrouped = *input.AllowUngrouped
	}
	if input.ClearExpiresAt {
		current.ExpiresAt = nil
	} else if input.ExpiresAt != nil {
		current.ExpiresAt = input.ExpiresAt
	}
	assignInt := func(target *int, value *int) {
		if value != nil {
			*target = *value
		}
	}
	assignInt64 := func(target *int64, value *int64) {
		if value != nil {
			*target = *value
		}
	}
	assignInt(&current.RPMLimit, input.RPMLimit)
	assignInt(&current.ConcurrencyLimit, input.ConcurrencyLimit)
	assignInt64(&current.RequestLimitTotal, input.RequestLimitTotal)
	assignInt64(&current.RequestLimit5h, input.RequestLimit5h)
	assignInt64(&current.RequestLimit1d, input.RequestLimit1d)
	assignInt64(&current.RequestLimit7d, input.RequestLimit7d)
	assignInt64(&current.TokenLimitTotal, input.TokenLimitTotal)
	assignInt64(&current.TokenLimit5h, input.TokenLimit5h)
	assignInt64(&current.TokenLimit1d, input.TokenLimit1d)
	assignInt64(&current.TokenLimit7d, input.TokenLimit7d)
	if errLimits := validateLimits(int64(current.RPMLimit), int64(current.ConcurrencyLimit), current.RequestLimitTotal, current.RequestLimit5h, current.RequestLimit1d, current.RequestLimit7d, current.TokenLimitTotal, current.TokenLimit5h, current.TokenLimit1d, current.TokenLimit7d); errLimits != nil {
		return Key{}, errLimits
	}
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return Key{}, errBegin
	}
	defer func() { _ = tx.Rollback() }()
	requestReset := ""
	if input.ResetRequestUsage {
		requestReset = `, request_used_total = 0, request_used_5h = 0, request_window_5h_ms = NULL, request_used_1d = 0, request_window_1d_ms = NULL, request_used_7d = 0, request_window_7d_ms = NULL`
	}
	tokenReset := ""
	if input.ResetTokenUsage {
		tokenReset = `, token_used_total = 0, token_used_5h = 0, token_window_5h_ms = NULL, token_used_1d = 0, token_window_1d_ms = NULL, token_used_7d = 0, token_window_7d_ms = NULL`
		if _, errDeleteReservations := tx.ExecContext(ctx, `DELETE FROM client_access_token_reservations WHERE key_id = ?`, id); errDeleteReservations != nil {
			return Key{}, fmt.Errorf("reset token reservations: %w", errDeleteReservations)
		}
	}
	where := " WHERE id = ?"
	args := []any{
		current.Name, boolInt(current.Enabled), boolInt(current.AllowAllGroups), boolInt(current.AllowUngrouped), nullableMillis(current.ExpiresAt), current.RPMLimit, current.ConcurrencyLimit,
		current.RequestLimitTotal, current.RequestLimit5h, current.RequestLimit1d, current.RequestLimit7d,
		current.TokenLimitTotal, current.TokenLimit5h, current.TokenLimit1d, current.TokenLimit7d, time.Now().UTC().UnixMilli(), id,
	}
	appendTenantScope(&where, &args, "tenant_id", tenantID)
	_, errExec := tx.ExecContext(ctx, `UPDATE client_access_keys SET name = ?, enabled = ?, allow_all_groups = ?, allow_ungrouped = ?, expires_at_ms = ?, rpm_limit = ?, concurrency_limit = ?,
		request_limit_total = ?, request_limit_5h = ?, request_limit_1d = ?, request_limit_7d = ?,
		token_limit_total = ?, token_limit_5h = ?, token_limit_1d = ?, token_limit_7d = ?, updated_at_ms = ?`+requestReset+tokenReset+where, args...)
	if errExec != nil {
		return Key{}, fmt.Errorf("update client key: %w", errExec)
	}
	if input.GroupIDs != nil {
		if errGroups := s.replaceKeyGroups(ctx, tx, id, current.TenantID, *input.GroupIDs); errGroups != nil {
			return Key{}, fmt.Errorf("update client key groups: %w", errGroups)
		}
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return Key{}, errCommit
	}
	return s.getKey(ctx, id, tenantID)
}

func (s *Store) DeleteKey(ctx context.Context, id int64) error {
	return s.deleteKey(ctx, id, nil)
}

func (s *Store) DeleteKeyScoped(ctx context.Context, id, tenantID int64) error {
	return s.deleteKey(ctx, id, &tenantID)
}

func (s *Store) deleteKey(ctx context.Context, id int64, tenantID *int64) error {
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	where := " WHERE id = ?"
	args := []any{id}
	appendTenantScope(&where, &args, "tenant_id", tenantID)
	result, errExec := s.db.ExecContext(ctx, `DELETE FROM client_access_keys`+where, args...)
	if errExec != nil {
		return errExec
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteTenantResources removes all client groups and API keys owned by one
// tenant. The tenant database owns the tenant record itself, so this is kept
// as an explicit cross-store cleanup operation.
func (s *Store) DeleteTenantResources(ctx context.Context, tenantID int64) ([]int64, error) {
	if tenantID <= 0 {
		return nil, errors.New("tenant id is required")
	}
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return nil, errBegin
	}
	defer func() { _ = tx.Rollback() }()
	rows, errQuery := tx.QueryContext(ctx, `SELECT id FROM client_access_keys WHERE tenant_id = ?`, tenantID)
	if errQuery != nil {
		return nil, errQuery
	}
	keyIDs := make([]int64, 0)
	for rows.Next() {
		var id int64
		if errScan := rows.Scan(&id); errScan != nil {
			_ = rows.Close()
			return nil, errScan
		}
		keyIDs = append(keyIDs, id)
	}
	if errRows := rows.Err(); errRows != nil {
		_ = rows.Close()
		return nil, errRows
	}
	if errClose := rows.Close(); errClose != nil {
		return nil, errClose
	}
	if _, errDeleteKeys := tx.ExecContext(ctx, `DELETE FROM client_access_keys WHERE tenant_id = ?`, tenantID); errDeleteKeys != nil {
		return nil, errDeleteKeys
	}
	if _, errDeleteGroups := tx.ExecContext(ctx, `DELETE FROM client_access_groups WHERE tenant_id = ?`, tenantID); errDeleteGroups != nil {
		return nil, errDeleteGroups
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return nil, errCommit
	}
	return keyIDs, nil
}

func (s *Store) TouchKey(ctx context.Context, id int64, usedAt time.Time) error {
	_, errExec := s.db.ExecContext(ctx, `UPDATE client_access_keys SET last_used_at_ms = ?, updated_at_ms = ? WHERE id = ?`, usedAt.UTC().UnixMilli(), usedAt.UTC().UnixMilli(), id)
	return errExec
}

func (s *Store) ListAllCredentialBindings(ctx context.Context) ([]CredentialBinding, error) {
	rows, errQuery := s.db.QueryContext(ctx, `SELECT cg.auth_index, cg.group_id, cg.priority, cg.created_at_ms
		FROM client_access_credential_groups cg
		JOIN client_access_groups g ON g.id = cg.group_id
		WHERE g.enabled = 1
		ORDER BY cg.auth_index, cg.group_id`)
	if errQuery != nil {
		return nil, errQuery
	}
	defer rows.Close()
	items := make([]CredentialBinding, 0)
	for rows.Next() {
		var item CredentialBinding
		var createdMS int64
		if errScan := rows.Scan(&item.AuthIndex, &item.GroupID, &item.Priority, &createdMS); errScan != nil {
			return nil, errScan
		}
		item.CreatedAt = time.UnixMilli(createdMS).UTC()
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListCredentialBindings(ctx context.Context, opts ListOptions) (Page[CredentialBinding], error) {
	opts = normalizeListOptions(opts)
	conditions := make([]string, 0, 2)
	args := make([]any, 0, len(opts.AuthIndices)+1)
	if opts.TenantID != nil {
		if *opts.TenantID > 0 {
			conditions = append(conditions, "g.tenant_id = ?")
			args = append(args, *opts.TenantID)
		} else {
			conditions = append(conditions, "g.tenant_id IS NULL")
		}
	}
	if opts.Search != "" {
		conditions = append(conditions, "cg.auth_index LIKE ?")
		args = append(args, "%"+opts.Search+"%")
	}
	if len(opts.AuthIndices) > 0 {
		placeholders := make([]string, 0, len(opts.AuthIndices))
		seen := make(map[string]struct{}, len(opts.AuthIndices))
		for _, rawIndex := range opts.AuthIndices {
			authIndex := strings.TrimSpace(rawIndex)
			if authIndex == "" {
				continue
			}
			if _, ok := seen[authIndex]; ok {
				continue
			}
			seen[authIndex] = struct{}{}
			placeholders = append(placeholders, "?")
			args = append(args, authIndex)
		}
		if len(placeholders) > 0 {
			conditions = append(conditions, "cg.auth_index IN ("+strings.Join(placeholders, ",")+")")
		}
	}
	if len(opts.GroupIDs) > 0 {
		placeholders := make([]string, 0, len(opts.GroupIDs))
		seen := make(map[int64]struct{}, len(opts.GroupIDs))
		for _, groupID := range opts.GroupIDs {
			if groupID <= 0 {
				continue
			}
			if _, ok := seen[groupID]; ok {
				continue
			}
			seen[groupID] = struct{}{}
			placeholders = append(placeholders, "?")
			args = append(args, groupID)
		}
		if len(placeholders) > 0 {
			conditions = append(conditions, "cg.group_id IN ("+strings.Join(placeholders, ",")+")")
		}
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	var total int64
	from := ` FROM client_access_credential_groups cg JOIN client_access_groups g ON g.id = cg.group_id`
	if errCount := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+from+where, args...).Scan(&total); errCount != nil {
		return Page[CredentialBinding]{}, errCount
	}
	queryArgs := append(append([]any(nil), args...), opts.PageSize, (opts.Page-1)*opts.PageSize)
	rows, errQuery := s.db.QueryContext(ctx, `SELECT cg.auth_index, cg.group_id, cg.priority, cg.created_at_ms
		`+from+where+` ORDER BY cg.auth_index, cg.group_id LIMIT ? OFFSET ?`, queryArgs...)
	if errQuery != nil {
		return Page[CredentialBinding]{}, errQuery
	}
	defer rows.Close()
	items := make([]CredentialBinding, 0, opts.PageSize)
	for rows.Next() {
		var item CredentialBinding
		var createdMS int64
		if errScan := rows.Scan(&item.AuthIndex, &item.GroupID, &item.Priority, &createdMS); errScan != nil {
			return Page[CredentialBinding]{}, errScan
		}
		item.CreatedAt = time.UnixMilli(createdMS).UTC()
		items = append(items, item)
	}
	if errRows := rows.Err(); errRows != nil {
		return Page[CredentialBinding]{}, errRows
	}
	return Page[CredentialBinding]{Items: items, Total: total, Page: opts.Page, PageSize: opts.PageSize}, nil
}

func (s *Store) ReplaceCredentialBindings(ctx context.Context, authIndices []string, groups []CredentialGroupInput) error {
	_, errReplace := s.ReplaceCredentialBindingsWithStats(ctx, authIndices, groups)
	return errReplace
}

// ReplaceGroupCredentialBindings replaces only the membership rows of groupID.
// Credentials may remain assigned to any other client groups.
func (s *Store) ReplaceGroupCredentialBindings(ctx context.Context, groupID int64, authIndices []string, priority int) (CredentialBindingChangeStats, error) {
	return s.replaceGroupCredentialBindings(ctx, groupID, nil, authIndices, priority)
}

func (s *Store) ReplaceGroupCredentialBindingsScoped(ctx context.Context, groupID, tenantID int64, authIndices []string, priority int) (CredentialBindingChangeStats, error) {
	return s.replaceGroupCredentialBindings(ctx, groupID, &tenantID, authIndices, priority)
}

func (s *Store) replaceGroupCredentialBindings(ctx context.Context, groupID int64, tenantID *int64, authIndices []string, priority int) (CredentialBindingChangeStats, error) {
	if groupID <= 0 {
		return CredentialBindingChangeStats{}, errors.New("group id is required")
	}
	authIndices = normalizeAuthIndices(authIndices)
	stats := CredentialBindingChangeStats{Matched: len(authIndices)}

	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return CredentialBindingChangeStats{}, errBegin
	}
	defer func() { _ = tx.Rollback() }()

	where := " WHERE id = ?"
	args := []any{groupID}
	appendTenantScope(&where, &args, "tenant_id", tenantID)
	var groupCount int
	if errCount := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM client_access_groups`+where, args...).Scan(&groupCount); errCount != nil {
		return CredentialBindingChangeStats{}, errCount
	}
	if groupCount != 1 {
		return CredentialBindingChangeStats{}, sql.ErrNoRows
	}

	existing := make(map[string]int)
	rows, errQuery := tx.QueryContext(ctx, `SELECT auth_index, priority FROM client_access_credential_groups WHERE group_id = ?`, groupID)
	if errQuery != nil {
		return CredentialBindingChangeStats{}, errQuery
	}
	for rows.Next() {
		var authIndex string
		var existingPriority int
		if errScan := rows.Scan(&authIndex, &existingPriority); errScan != nil {
			_ = rows.Close()
			return CredentialBindingChangeStats{}, errScan
		}
		existing[authIndex] = existingPriority
	}
	if errRows := rows.Err(); errRows != nil {
		_ = rows.Close()
		return CredentialBindingChangeStats{}, errRows
	}
	if errClose := rows.Close(); errClose != nil {
		return CredentialBindingChangeStats{}, errClose
	}

	unchanged := len(existing) == len(authIndices)
	if unchanged {
		for _, authIndex := range authIndices {
			if existing[authIndex] != priority {
				unchanged = false
				break
			}
		}
	}
	if unchanged {
		stats.Unchanged = len(authIndices)
		if errCommit := tx.Commit(); errCommit != nil {
			return CredentialBindingChangeStats{}, errCommit
		}
		return stats, nil
	}
	changedAuthIndices := make(map[string]struct{}, len(existing)+len(authIndices))
	for authIndex := range existing {
		changedAuthIndices[authIndex] = struct{}{}
	}
	for _, authIndex := range authIndices {
		changedAuthIndices[authIndex] = struct{}{}
	}

	if _, errDelete := tx.ExecContext(ctx, `DELETE FROM client_access_credential_groups WHERE group_id = ?`, groupID); errDelete != nil {
		return CredentialBindingChangeStats{}, errDelete
	}
	nowMS := time.Now().UTC().UnixMilli()
	values := make([]string, 0, sqliteInsertRowsPerBatch)
	args = make([]any, 0, sqliteInsertRowsPerBatch*4)
	flushInsert := func() error {
		if len(values) == 0 {
			return nil
		}
		_, errInsert := tx.ExecContext(ctx, `INSERT INTO client_access_credential_groups(auth_index, group_id, priority, created_at_ms) VALUES `+strings.Join(values, ","), args...)
		values = values[:0]
		args = args[:0]
		return errInsert
	}
	for _, authIndex := range authIndices {
		values = append(values, "(?, ?, ?, ?)")
		args = append(args, authIndex, groupID, priority, nowMS)
		if len(values) >= sqliteInsertRowsPerBatch {
			if errInsert := flushInsert(); errInsert != nil {
				return CredentialBindingChangeStats{}, errInsert
			}
		}
	}
	if errInsert := flushInsert(); errInsert != nil {
		return CredentialBindingChangeStats{}, errInsert
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return CredentialBindingChangeStats{}, errCommit
	}
	stats.Updated = len(changedAuthIndices)
	return stats, nil
}

func (s *Store) ReplaceCredentialBindingsWithStats(ctx context.Context, authIndices []string, groups []CredentialGroupInput) (CredentialBindingChangeStats, error) {
	authIndices = normalizeAuthIndices(authIndices)
	groups = normalizeCredentialGroups(groups)
	stats := CredentialBindingChangeStats{Matched: len(authIndices)}
	if len(authIndices) == 0 {
		return stats, nil
	}
	tx, errBegin := s.db.BeginTx(ctx, nil)
	if errBegin != nil {
		return CredentialBindingChangeStats{}, errBegin
	}
	defer func() { _ = tx.Rollback() }()
	if errValidate := validateCredentialGroups(ctx, tx, groups); errValidate != nil {
		return CredentialBindingChangeStats{}, errValidate
	}

	existing := make(map[string]map[int64]int, len(authIndices))
	for start := 0; start < len(authIndices); start += sqliteVariableChunkSize {
		end := start + sqliteVariableChunkSize
		if end > len(authIndices) {
			end = len(authIndices)
		}
		chunk := authIndices[start:end]
		placeholders := strings.TrimRight(strings.Repeat("?,", len(chunk)), ",")
		args := make([]any, 0, len(chunk))
		for _, authIndex := range chunk {
			args = append(args, authIndex)
		}
		rows, errQuery := tx.QueryContext(ctx, `SELECT auth_index, group_id, priority FROM client_access_credential_groups WHERE auth_index IN (`+placeholders+`)`, args...)
		if errQuery != nil {
			return CredentialBindingChangeStats{}, errQuery
		}
		for rows.Next() {
			var authIndex string
			var groupID int64
			var priority int
			if errScan := rows.Scan(&authIndex, &groupID, &priority); errScan != nil {
				_ = rows.Close()
				return CredentialBindingChangeStats{}, errScan
			}
			memberships := existing[authIndex]
			if memberships == nil {
				memberships = make(map[int64]int)
				existing[authIndex] = memberships
			}
			memberships[groupID] = priority
		}
		if errRows := rows.Err(); errRows != nil {
			_ = rows.Close()
			return CredentialBindingChangeStats{}, errRows
		}
		if errClose := rows.Close(); errClose != nil {
			return CredentialBindingChangeStats{}, errClose
		}
	}

	desired := make(map[int64]int, len(groups))
	for _, group := range groups {
		desired[group.GroupID] = group.Priority
	}
	changedAuthIndices := make([]string, 0, len(authIndices))
	for _, authIndex := range authIndices {
		if credentialMembershipsEqual(existing[authIndex], desired) {
			stats.Unchanged++
			continue
		}
		changedAuthIndices = append(changedAuthIndices, authIndex)
	}
	stats.Updated = len(changedAuthIndices)
	if len(changedAuthIndices) == 0 {
		if errCommit := tx.Commit(); errCommit != nil {
			return CredentialBindingChangeStats{}, errCommit
		}
		return stats, nil
	}

	for start := 0; start < len(changedAuthIndices); start += sqliteVariableChunkSize {
		end := start + sqliteVariableChunkSize
		if end > len(changedAuthIndices) {
			end = len(changedAuthIndices)
		}
		chunk := changedAuthIndices[start:end]
		placeholders := strings.TrimRight(strings.Repeat("?,", len(chunk)), ",")
		args := make([]any, 0, len(chunk))
		for _, authIndex := range chunk {
			args = append(args, authIndex)
		}
		if _, errDelete := tx.ExecContext(ctx, `DELETE FROM client_access_credential_groups WHERE auth_index IN (`+placeholders+`)`, args...); errDelete != nil {
			return CredentialBindingChangeStats{}, errDelete
		}
	}

	nowMS := time.Now().UTC().UnixMilli()
	values := make([]string, 0, sqliteInsertRowsPerBatch)
	insertArgs := make([]any, 0, sqliteInsertRowsPerBatch*4)
	flushInsert := func() error {
		if len(values) == 0 {
			return nil
		}
		_, errInsert := tx.ExecContext(ctx, `INSERT INTO client_access_credential_groups(auth_index, group_id, priority, created_at_ms) VALUES `+strings.Join(values, ","), insertArgs...)
		values = values[:0]
		insertArgs = insertArgs[:0]
		return errInsert
	}
	for _, authIndex := range changedAuthIndices {
		for _, group := range groups {
			values = append(values, "(?, ?, ?, ?)")
			insertArgs = append(insertArgs, authIndex, group.GroupID, group.Priority, nowMS)
			if len(values) >= sqliteInsertRowsPerBatch {
				if errInsert := flushInsert(); errInsert != nil {
					return CredentialBindingChangeStats{}, errInsert
				}
			}
		}
	}
	if errInsert := flushInsert(); errInsert != nil {
		return CredentialBindingChangeStats{}, errInsert
	}
	if errCommit := tx.Commit(); errCommit != nil {
		return CredentialBindingChangeStats{}, errCommit
	}
	return stats, nil
}

func normalizeAuthIndices(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, rawValue := range values {
		value := strings.TrimSpace(rawValue)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeCredentialGroups(values []CredentialGroupInput) []CredentialGroupInput {
	result := make([]CredentialGroupInput, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, value := range values {
		if value.GroupID <= 0 {
			continue
		}
		if _, ok := seen[value.GroupID]; ok {
			continue
		}
		seen[value.GroupID] = struct{}{}
		result = append(result, value)
	}
	return result
}

func validateCredentialGroups(ctx context.Context, tx *sql.Tx, groups []CredentialGroupInput) error {
	if len(groups) == 0 {
		return nil
	}
	for start := 0; start < len(groups); start += sqliteVariableChunkSize {
		end := start + sqliteVariableChunkSize
		if end > len(groups) {
			end = len(groups)
		}
		chunk := groups[start:end]
		placeholders := strings.TrimRight(strings.Repeat("?,", len(chunk)), ",")
		args := make([]any, 0, len(chunk))
		for _, group := range chunk {
			args = append(args, group.GroupID)
		}
		var count int
		if errCount := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM client_access_groups WHERE id IN (`+placeholders+`)`, args...).Scan(&count); errCount != nil {
			return errCount
		}
		if count != len(chunk) {
			return errors.New("one or more client groups do not exist")
		}
	}
	return nil
}

func credentialMembershipsEqual(existing, desired map[int64]int) bool {
	if len(existing) != len(desired) {
		return false
	}
	for groupID, priority := range desired {
		existingPriority, ok := existing[groupID]
		if !ok || existingPriority != priority {
			return false
		}
	}
	return true
}
