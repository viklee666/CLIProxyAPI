package config

// ClientAccessConfig configures persistent advanced client API keys and credential groups.
type ClientAccessConfig struct {
	// Enabled activates the persistent client access provider and management API.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// DatabasePath stores advanced keys, groups, bindings, and quota counters.
	DatabasePath string `yaml:"database-path,omitempty" json:"database-path,omitempty"`
	// TokenReservation is the provisional token amount held for each in-flight request.
	TokenReservation int64 `yaml:"token-reservation,omitempty" json:"token-reservation,omitempty"`
}
