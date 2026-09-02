package config

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultInFlightMaxPartBytes       = 256 * 1024
	DefaultInFlightMaxPartCount       = 64
	DefaultInFlightMaxRevisionBytes   = 16 * 1024 * 1024
	DefaultInFlightMaxAggregateGroups = 100000
	DefaultInFlightMaxDetails         = 10000
	DefaultInFlightMaxStringBytes     = 256
)

// CredentialInFlightConfig controls in-flight credential observation snapshots.
type CredentialInFlightConfig struct {
	SnapshotInterval   string `yaml:"snapshot-interval" json:"snapshot-interval"`
	StaleAfter         string `yaml:"stale-after" json:"stale-after"`
	MaxPartBytes       int    `yaml:"max-part-bytes" json:"max-part-bytes"`
	MaxPartCount       int    `yaml:"max-part-count" json:"max-part-count"`
	MaxRevisionBytes   int    `yaml:"max-revision-bytes" json:"max-revision-bytes"`
	MaxAggregateGroups int    `yaml:"max-aggregate-groups" json:"max-aggregate-groups"`
	MaxDetails         int    `yaml:"max-details" json:"max-details"`
	MaxStringBytes     int    `yaml:"max-string-bytes" json:"max-string-bytes"`
	StagingRetention   string `yaml:"staging-retention" json:"staging-retention"`

	maxDetailsPresent bool
}

// DefaultCredentialInFlightConfig returns the in-flight observation defaults.
func DefaultCredentialInFlightConfig() CredentialInFlightConfig {
	return CredentialInFlightConfig{
		SnapshotInterval:   "2s",
		StaleAfter:         "10s",
		MaxPartBytes:       DefaultInFlightMaxPartBytes,
		MaxPartCount:       DefaultInFlightMaxPartCount,
		MaxRevisionBytes:   DefaultInFlightMaxRevisionBytes,
		MaxAggregateGroups: DefaultInFlightMaxAggregateGroups,
		MaxDetails:         DefaultInFlightMaxDetails,
		MaxStringBytes:     DefaultInFlightMaxStringBytes,
		StagingRetention:   "1m",
	}
}

// UnmarshalYAML preserves max-details presence so an explicit 0 keeps details
// disabled while an omitted value still receives DefaultInFlightMaxDetails.
func (c *CredentialInFlightConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawCredentialInFlightConfig struct {
		SnapshotInterval   string `yaml:"snapshot-interval"`
		StaleAfter         string `yaml:"stale-after"`
		MaxPartBytes       int    `yaml:"max-part-bytes"`
		MaxPartCount       int    `yaml:"max-part-count"`
		MaxRevisionBytes   int    `yaml:"max-revision-bytes"`
		MaxAggregateGroups int    `yaml:"max-aggregate-groups"`
		MaxDetails         int    `yaml:"max-details"`
		MaxStringBytes     int    `yaml:"max-string-bytes"`
		StagingRetention   string `yaml:"staging-retention"`
	}

	var raw rawCredentialInFlightConfig
	if errDecode := value.Decode(&raw); errDecode != nil {
		return errDecode
	}

	*c = CredentialInFlightConfig{
		SnapshotInterval:   raw.SnapshotInterval,
		StaleAfter:         raw.StaleAfter,
		MaxPartBytes:       raw.MaxPartBytes,
		MaxPartCount:       raw.MaxPartCount,
		MaxRevisionBytes:   raw.MaxRevisionBytes,
		MaxAggregateGroups: raw.MaxAggregateGroups,
		MaxDetails:         raw.MaxDetails,
		MaxStringBytes:     raw.MaxStringBytes,
		StagingRetention:   raw.StagingRetention,
		maxDetailsPresent:  credentialInFlightFieldPresent(value, "max-details"),
	}
	return nil
}

func credentialInFlightFieldPresent(value *yaml.Node, field string) bool {
	if value == nil || value.Kind != yaml.MappingNode {
		return false
	}
	for index := 0; index+1 < len(value.Content); index += 2 {
		if value.Content[index].Value == field {
			return true
		}
	}
	return false
}

// WithDefaults fills omitted in-flight observation fields so configs that omit
// credential-in-flight (including YAML round-trips of zero-value structs) stay valid.
// MaxDetails 0 is valid ("no details"), so only an omitted max-details is defaulted.
func (c CredentialInFlightConfig) WithDefaults() CredentialInFlightConfig {
	defaults := DefaultCredentialInFlightConfig()
	if strings.TrimSpace(c.SnapshotInterval) == "" {
		c.SnapshotInterval = defaults.SnapshotInterval
	}
	if strings.TrimSpace(c.StaleAfter) == "" {
		c.StaleAfter = defaults.StaleAfter
	}
	if c.MaxPartBytes == 0 {
		c.MaxPartBytes = defaults.MaxPartBytes
	}
	if c.MaxPartCount == 0 {
		c.MaxPartCount = defaults.MaxPartCount
	}
	if c.MaxRevisionBytes == 0 {
		c.MaxRevisionBytes = defaults.MaxRevisionBytes
	}
	if c.MaxAggregateGroups == 0 {
		c.MaxAggregateGroups = defaults.MaxAggregateGroups
	}
	if !c.maxDetailsPresent && c.MaxDetails == 0 {
		c.MaxDetails = defaults.MaxDetails
	}
	if c.MaxStringBytes == 0 {
		c.MaxStringBytes = defaults.MaxStringBytes
	}
	if strings.TrimSpace(c.StagingRetention) == "" {
		c.StagingRetention = defaults.StagingRetention
	}
	return c
}

// Durations parses and validates the in-flight observation durations.
func (c CredentialInFlightConfig) Durations() (time.Duration, time.Duration, time.Duration, error) {
	snapshotInterval, errSnapshot := time.ParseDuration(c.SnapshotInterval)
	if errSnapshot != nil || snapshotInterval <= 0 {
		return 0, 0, 0, fmt.Errorf("credential-in-flight.snapshot-interval must be positive")
	}
	staleAfter, errStale := time.ParseDuration(c.StaleAfter)
	if errStale != nil || staleAfter <= 0 || snapshotInterval > staleAfter/3 {
		return 0, 0, 0, fmt.Errorf("credential-in-flight.stale-after must be at least three snapshot intervals")
	}
	stagingRetention, errRetention := time.ParseDuration(c.StagingRetention)
	if errRetention != nil || stagingRetention <= 0 {
		return 0, 0, 0, fmt.Errorf("credential-in-flight.staging-retention must be positive")
	}
	return snapshotInterval, staleAfter, stagingRetention, nil
}

// Validate verifies the in-flight observation bounds.
func (c CredentialInFlightConfig) Validate() error {
	if _, _, _, errDurations := c.Durations(); errDurations != nil {
		return errDurations
	}
	if c.MaxPartBytes < 1024 || c.MaxPartCount <= 0 || c.MaxPartCount > DefaultInFlightMaxPartCount {
		return fmt.Errorf("credential-in-flight part bounds are invalid")
	}
	if c.MaxRevisionBytes < c.MaxPartBytes || c.MaxRevisionBytes > DefaultInFlightMaxRevisionBytes {
		return fmt.Errorf("credential-in-flight.max-revision-bytes is outside hard bounds")
	}
	requiredParts := (c.MaxRevisionBytes + c.MaxPartBytes - 1) / c.MaxPartBytes
	if requiredParts > c.MaxPartCount {
		return fmt.Errorf("credential-in-flight.max-revision-bytes exceeds part capacity")
	}
	if c.MaxAggregateGroups <= 0 || c.MaxAggregateGroups > DefaultInFlightMaxAggregateGroups {
		return fmt.Errorf("credential-in-flight.max-aggregate-groups is invalid")
	}
	if c.MaxDetails < 0 || c.MaxDetails > DefaultInFlightMaxDetails {
		return fmt.Errorf("credential-in-flight.max-details is invalid")
	}
	if c.MaxStringBytes <= 0 || c.MaxStringBytes > DefaultInFlightMaxStringBytes {
		return fmt.Errorf("credential-in-flight.max-string-bytes is invalid")
	}
	return nil
}
