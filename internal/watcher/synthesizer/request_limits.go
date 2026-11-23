package synthesizer

import "github.com/router-for-me/CLIProxyAPI/v8/internal/config"

func addRequestLimitsToMetadata(limits config.RequestLimits, metadata map[string]any) {
	if limits.ConcurrencyLimit > 0 {
		metadata["concurrency_limit"] = limits.ConcurrencyLimit
	}
	if limits.RPMLimit > 0 {
		metadata["rpm_limit"] = limits.RPMLimit
	}
}
