package auth

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/credentialweight"
)

// ValidateAuthWeight validates every explicit credential weight source.
func ValidateAuthWeight(auth *Auth) error {
	if auth == nil {
		return nil
	}
	if rawWeight := strings.TrimSpace(auth.ReadAttribute(AttributeWeight)); rawWeight != "" {
		if _, errParse := credentialweight.ParseString(rawWeight); errParse != nil {
			return fmt.Errorf("invalid attributes weight: %w", errParse)
		}
	}
	if rawWeight, ok := auth.ReadMetadata(AttributeWeight); ok {
		if _, errParse := credentialweight.ParseValue(rawWeight); errParse != nil {
			return fmt.Errorf("invalid metadata weight: %w", errParse)
		}
	}
	return nil
}

// ApplyAuthWeightMetadata validates the auth and applies a source metadata weight.
func ApplyAuthWeightMetadata(auth *Auth, metadata map[string]any) error {
	if errWeight := ValidateAuthWeight(auth); errWeight != nil {
		return errWeight
	}
	if auth == nil || metadata == nil {
		return nil
	}
	rawWeight, ok := metadata[AttributeWeight]
	if !ok {
		return nil
	}
	weight, errParse := credentialweight.ParseValue(rawWeight)
	if errParse != nil {
		return fmt.Errorf("invalid metadata weight: %w", errParse)
	}
	auth.MutateAttributes(func(attrs map[string]string) {
		attrs[AttributeWeight] = strconv.FormatInt(weight, 10)
	})
	return nil
}
