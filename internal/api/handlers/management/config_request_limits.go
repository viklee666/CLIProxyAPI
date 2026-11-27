package management

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// requestLimitsPatch preserves omitted limits when updating a provider record.
type requestLimitsPatch struct {
	ConcurrencyLimit *int `json:"concurrency-limit" binding:"omitempty,gte=0"`
	RPMLimit         *int `json:"rpm-limit" binding:"omitempty,gte=0"`
}

func (patch requestLimitsPatch) apply(limits *config.RequestLimits) {
	if patch.ConcurrencyLimit != nil {
		limits.ConcurrencyLimit = *patch.ConcurrencyLimit
	}
	if patch.RPMLimit != nil {
		limits.RPMLimit = *patch.RPMLimit
	}
}

// validateRequestLimitsJSON checks collection PUT payloads before any config mutation.
func validateRequestLimitsJSON(data []byte) error {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if errDecode := decoder.Decode(&value); errDecode != nil {
		return errDecode
	}
	return validateRequestLimitsValue(value)
}

func validateRequestLimitsValue(value any) error {
	switch item := value.(type) {
	case []any:
		for _, entry := range item {
			if errValidate := validateRequestLimitsValue(entry); errValidate != nil {
				return errValidate
			}
		}
	case map[string]any:
		for _, key := range []string{"concurrency-limit", "rpm-limit", "concurrency_limit", "rpm_limit"} {
			if raw, exists := item[key]; exists && raw != nil {
				number, ok := raw.(json.Number)
				if !ok {
					return fmt.Errorf("%s must be a non-negative integer", key)
				}
				limit, errParse := number.Int64()
				if errParse != nil || limit < 0 || int64(int(limit)) != limit {
					return fmt.Errorf("%s must be a non-negative integer", key)
				}
			}
		}
		for _, key := range []string{"items", "value"} {
			if nested, exists := item[key]; exists {
				if errValidate := validateRequestLimitsValue(nested); errValidate != nil {
					return errValidate
				}
			}
		}
	}
	return nil
}
