package config

// AdaptiveRoutingConfig configures runtime-aware credential selection.
type AdaptiveRoutingConfig struct {
	TopK         int                    `yaml:"top-k,omitempty" json:"top-k,omitempty"`
	EWMAAlpha    float64                `yaml:"ewma-alpha,omitempty" json:"ewma-alpha,omitempty"`
	TTFTTargetMS int64                  `yaml:"ttft-target-ms,omitempty" json:"ttft-target-ms,omitempty"`
	Weights      AdaptiveRoutingWeights `yaml:"weights,omitempty" json:"weights,omitempty"`
	StickyEscape AdaptiveStickyEscape   `yaml:"sticky-escape,omitempty" json:"sticky-escape,omitempty"`
}

// AdaptiveRoutingWeights controls each normalized score component.
type AdaptiveRoutingWeights struct {
	Priority    float64 `yaml:"priority,omitempty" json:"priority,omitempty"`
	Load        float64 `yaml:"load,omitempty" json:"load,omitempty"`
	SuccessRate float64 `yaml:"success-rate,omitempty" json:"success-rate,omitempty"`
	TTFT        float64 `yaml:"ttft,omitempty" json:"ttft,omitempty"`
}

// AdaptiveStickyEscape controls when session affinity abandons a slow or unhealthy credential.
type AdaptiveStickyEscape struct {
	Enabled                bool    `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	MinSamples             int64   `yaml:"min-samples,omitempty" json:"min-samples,omitempty"`
	ErrorRateThreshold     float64 `yaml:"error-rate-threshold,omitempty" json:"error-rate-threshold,omitempty"`
	TTFTThresholdMS        int64   `yaml:"ttft-threshold-ms,omitempty" json:"ttft-threshold-ms,omitempty"`
	ActiveRequestThreshold int     `yaml:"active-request-threshold,omitempty" json:"active-request-threshold,omitempty"`
}
