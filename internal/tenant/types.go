package tenant

import "time"

const (
	DefaultSessionTTL       = 30 * 24 * time.Hour
	GeneratedPasswordLength = 16
)

type Tenant struct {
	ID          int64     `json:"id"`
	DisplayName string    `json:"display_name"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateInput struct {
	DisplayName string
	Password    string
}

type UpdateInput struct {
	DisplayName *string
	Enabled     *bool
}

type Session struct {
	TenantID  int64
	CreatedAt time.Time
	ExpiresAt time.Time
}
