package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// RequestLimits caps simultaneous requests and requests admitted in any rolling minute.
// Zero leaves the corresponding limit disabled.
type RequestLimits struct {
	ConcurrencyLimit int `yaml:"concurrency-limit,omitempty" json:"concurrency-limit,omitempty" binding:"gte=0"`
	RPMLimit         int `yaml:"rpm-limit,omitempty" json:"rpm-limit,omitempty" binding:"gte=0"`
}

// Validate rejects negative request limits.
func (limits RequestLimits) Validate() error {
	if limits.ConcurrencyLimit < 0 || limits.RPMLimit < 0 {
		return fmt.Errorf("concurrency-limit and rpm-limit must be non-negative integers (0 means unlimited)")
	}
	return nil
}

func validateRequestLimitsNode(node *yaml.Node) error {
	root, err := flattenV8(node)
	if err != nil {
		return err
	}
	families := map[string]bool{
		"gemini-api-key": true, "interactions-api-key": true, "claude-api-key": true,
		"codex-api-key": true, "xai-api-key": true, "meta-api-key": true,
		"vertex-api-key": true, "openai-compatibility": true,
	}
	for index := 0; root != nil && root.Kind == yaml.MappingNode && index+1 < len(root.Content); index += 2 {
		family, sequence := root.Content[index].Value, root.Content[index+1]
		if !families[family] || sequence.Kind != yaml.SequenceNode {
			continue
		}
		for recordIndex, record := range sequence.Content {
			for field := 0; record.Kind == yaml.MappingNode && field+1 < len(record.Content); field += 2 {
				name, value := record.Content[field].Value, record.Content[field+1]
				if name != "concurrency-limit" && name != "rpm-limit" {
					continue
				}
				var limit int
				if value.Kind != yaml.ScalarNode || value.Tag != "!!int" || value.Decode(&limit) != nil || limit < 0 {
					return fmt.Errorf("%s[%d].%s must be a non-negative integer", family, recordIndex, name)
				}
			}
		}
	}
	return nil
}

// ValidateRequestLimits validates all configured provider records.
func (cfg *Config) ValidateRequestLimits() error {
	if cfg == nil {
		return nil
	}
	groups := map[string][]RequestLimits{}
	for _, entry := range cfg.GeminiKey {
		groups["gemini-api-key"] = append(groups["gemini-api-key"], entry.RequestLimits)
	}
	for _, entry := range cfg.InteractionsKey {
		groups["interactions-api-key"] = append(groups["interactions-api-key"], entry.RequestLimits)
	}
	for _, entry := range cfg.ClaudeKey {
		groups["claude-api-key"] = append(groups["claude-api-key"], entry.RequestLimits)
	}
	for _, entry := range cfg.CodexKey {
		groups["codex-api-key"] = append(groups["codex-api-key"], entry.RequestLimits)
	}
	for _, entry := range cfg.XAIKey {
		groups["xai-api-key"] = append(groups["xai-api-key"], entry.RequestLimits)
	}
	for _, entry := range cfg.MetaKey {
		groups["meta-api-key"] = append(groups["meta-api-key"], entry.RequestLimits)
	}
	for _, entry := range cfg.VertexCompatAPIKey {
		groups["vertex-api-key"] = append(groups["vertex-api-key"], entry.RequestLimits)
	}
	for _, entry := range cfg.OpenAICompatibility {
		groups["openai-compatibility"] = append(groups["openai-compatibility"], entry.RequestLimits)
	}
	for family, entries := range groups {
		for index, limits := range entries {
			if errValidate := limits.Validate(); errValidate != nil {
				return fmt.Errorf("%s[%d]: %w", family, index, errValidate)
			}
		}
	}
	return nil
}
