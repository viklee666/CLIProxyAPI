package clientaccess

import "time"

const (
	ProviderIdentifier = "client-access"
	ProviderType       = "client-access"
)

const (
	MetadataKeyID             = "client_key_id"
	MetadataKeyHash           = "client_key_hash"
	MetadataKeyGroupIDs       = "client_group_ids"
	MetadataKeyAllowAllGroups = "client_allow_all_groups"
	MetadataKeyAllowUngrouped = "client_allow_ungrouped"
	MetadataKeyReservationID  = "client_reservation_id"
)

type Group struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	Enabled         bool      `json:"enabled"`
	KeyCount        int64     `json:"key_count"`
	CredentialCount int64     `json:"credential_count"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Key struct {
	ID                 int64      `json:"id"`
	Name               string     `json:"name"`
	Secret             string     `json:"secret,omitempty"`
	KeyPrefix          string     `json:"key_prefix"`
	KeyMask            string     `json:"key_mask"`
	Enabled            bool       `json:"enabled"`
	AllowAllGroups     bool       `json:"allow_all_groups"`
	AllowUngrouped     bool       `json:"allow_ungrouped"`
	GroupIDs           []int64    `json:"group_ids"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	RPMLimit           int        `json:"rpm_limit"`
	ConcurrencyLimit   int        `json:"concurrency_limit"`
	CurrentConcurrency int        `json:"current_concurrency"`
	RequestLimitTotal  int64      `json:"request_limit_total"`
	RequestUsedTotal   int64      `json:"request_used_total"`
	RequestLimit5h     int64      `json:"request_limit_5h"`
	RequestUsed5h      int64      `json:"request_used_5h"`
	RequestWindow5hAt  *time.Time `json:"request_window_5h_at,omitempty"`
	RequestLimit1d     int64      `json:"request_limit_1d"`
	RequestUsed1d      int64      `json:"request_used_1d"`
	RequestWindow1dAt  *time.Time `json:"request_window_1d_at,omitempty"`
	RequestLimit7d     int64      `json:"request_limit_7d"`
	RequestUsed7d      int64      `json:"request_used_7d"`
	RequestWindow7dAt  *time.Time `json:"request_window_7d_at,omitempty"`
	TokenLimitTotal    int64      `json:"token_limit_total"`
	TokenUsedTotal     int64      `json:"token_used_total"`
	TokenReserved      int64      `json:"token_reserved"`
	TokenLimit5h       int64      `json:"token_limit_5h"`
	TokenUsed5h        int64      `json:"token_used_5h"`
	TokenWindow5hAt    *time.Time `json:"token_window_5h_at,omitempty"`
	TokenLimit1d       int64      `json:"token_limit_1d"`
	TokenUsed1d        int64      `json:"token_used_1d"`
	TokenWindow1dAt    *time.Time `json:"token_window_1d_at,omitempty"`
	TokenLimit7d       int64      `json:"token_limit_7d"`
	TokenUsed7d        int64      `json:"token_used_7d"`
	TokenWindow7dAt    *time.Time `json:"token_window_7d_at,omitempty"`
	LastUsedAt         *time.Time `json:"last_used_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type CreatedKey struct {
	Key
}

type CredentialBinding struct {
	AuthIndex string    `json:"auth_index"`
	GroupID   int64     `json:"group_id"`
	Priority  int       `json:"priority"`
	CreatedAt time.Time `json:"created_at"`
}

type CredentialGroupInput struct {
	GroupID  int64 `json:"group_id"`
	Priority int   `json:"priority"`
}

type CredentialBindingBatch struct {
	AuthIndices []string               `json:"auth_indices"`
	Groups      []CredentialGroupInput `json:"groups"`
}

// GroupCredentialBindingBatch replaces the credential membership of one client group.
// It intentionally leaves memberships in other groups unchanged.
type GroupCredentialBindingBatch struct {
	AuthIndices []string `json:"auth_indices"`
	Priority    int      `json:"priority"`
}

type CredentialBindingChangeStats struct {
	Matched   int `json:"matched"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
}

type GroupCreate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

type GroupUpdate struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
}

type KeyCreate struct {
	Name              string     `json:"name"`
	CustomSecret      string     `json:"secret,omitempty"`
	Enabled           *bool      `json:"enabled,omitempty"`
	AllowAllGroups    *bool      `json:"allow_all_groups,omitempty"`
	AllowUngrouped    bool       `json:"allow_ungrouped"`
	GroupIDs          []int64    `json:"group_ids"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	RPMLimit          int        `json:"rpm_limit"`
	ConcurrencyLimit  int        `json:"concurrency_limit"`
	RequestLimitTotal int64      `json:"request_limit_total"`
	RequestLimit5h    int64      `json:"request_limit_5h"`
	RequestLimit1d    int64      `json:"request_limit_1d"`
	RequestLimit7d    int64      `json:"request_limit_7d"`
	TokenLimitTotal   int64      `json:"token_limit_total"`
	TokenLimit5h      int64      `json:"token_limit_5h"`
	TokenLimit1d      int64      `json:"token_limit_1d"`
	TokenLimit7d      int64      `json:"token_limit_7d"`
}

type KeyUpdate struct {
	Name              *string    `json:"name,omitempty"`
	Enabled           *bool      `json:"enabled,omitempty"`
	AllowAllGroups    *bool      `json:"allow_all_groups,omitempty"`
	AllowUngrouped    *bool      `json:"allow_ungrouped,omitempty"`
	GroupIDs          *[]int64   `json:"group_ids,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	ClearExpiresAt    bool       `json:"clear_expires_at,omitempty"`
	RPMLimit          *int       `json:"rpm_limit,omitempty"`
	ConcurrencyLimit  *int       `json:"concurrency_limit,omitempty"`
	RequestLimitTotal *int64     `json:"request_limit_total,omitempty"`
	RequestLimit5h    *int64     `json:"request_limit_5h,omitempty"`
	RequestLimit1d    *int64     `json:"request_limit_1d,omitempty"`
	RequestLimit7d    *int64     `json:"request_limit_7d,omitempty"`
	TokenLimitTotal   *int64     `json:"token_limit_total,omitempty"`
	TokenLimit5h      *int64     `json:"token_limit_5h,omitempty"`
	TokenLimit1d      *int64     `json:"token_limit_1d,omitempty"`
	TokenLimit7d      *int64     `json:"token_limit_7d,omitempty"`
	ResetRequestUsage bool       `json:"reset_request_usage,omitempty"`
	ResetTokenUsage   bool       `json:"reset_token_usage,omitempty"`
}

type ListOptions struct {
	Page        int
	PageSize    int
	Search      string
	Enabled     *bool
	AuthIndices []string
	GroupIDs    []int64
}

type Page[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// QuotaExceededError describes the first persistent quota that blocked a request.
type QuotaExceededError struct {
	Resource string
	Window   string
	Limit    int64
	Used     int64
	ResetAt  *time.Time
}

func (e *QuotaExceededError) Error() string {
	if e == nil {
		return "quota exceeded"
	}
	return e.Resource + " " + e.Window + " quota exceeded"
}
