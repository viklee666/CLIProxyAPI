package modelconfig

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
)

// ResolveModelInfo returns a private capability snapshot for a configured model.
// Static capabilities come from the suffix-free upstream name, while explicit
// configuration takes precedence. Unknown names with no static entry and no
// explicit thinking support are marked UserDefined so thinking can pass
// through instead of being stripped.
func ResolveModelInfo(name, modelType string, support *registry.ThinkingSupport) *registry.ModelInfo {
	trimmedName := strings.TrimSpace(name)
	baseName := strings.TrimSpace(thinking.ParseSuffix(trimmedName).ModelName)
	static := registry.LookupStaticModelInfo(baseName)
	info := static
	if info == nil {
		info = &registry.ModelInfo{}
	}
	info.ID = trimmedName
	info.Type = strings.TrimSpace(modelType)
	if support != nil {
		info.Thinking = NormalizeThinkingSupport(support)
	}
	info.UserDefined = static == nil && support == nil
	return info
}

// NormalizeThinkingSupport clones and normalizes configured reasoning levels.
func NormalizeThinkingSupport(raw *registry.ThinkingSupport) *registry.ThinkingSupport {
	if raw == nil {
		return nil
	}
	normalized := *raw
	normalized.Levels = nil
	seen := make(map[string]struct{}, len(raw.Levels))
	for _, value := range raw.Levels {
		level := strings.ToLower(strings.TrimSpace(value))
		if level == "" {
			continue
		}
		switch level {
		case "none":
			normalized.ZeroAllowed = true
		case "auto":
			normalized.DynamicAllowed = true
		}
		if _, exists := seen[level]; exists {
			continue
		}
		seen[level] = struct{}{}
		normalized.Levels = append(normalized.Levels, level)
	}
	return &normalized
}
