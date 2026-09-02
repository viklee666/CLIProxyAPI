package auth

import (
	"sort"
	"strconv"
	"strings"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// CredentialGroupResolver applies request-scoped client group access to credentials.
type CredentialGroupResolver interface {
	ResolveCredentialAccess(authIndex string, allowedGroupIDs []int64, allowAllGroups, allowUngrouped bool) (allowed bool, priority int, priorityOverride bool)
}

// CredentialOwnershipResolver identifies credentials that belong to a tenant.
// A non-zero owner must match the tenant identity attached to the caller.
type CredentialOwnershipResolver interface {
	OwnerOf(authIndex string) int64
	HasOwnedCredentials() bool
}

func (m *Manager) SetCredentialGroupResolver(resolver CredentialGroupResolver) {
	if m == nil {
		return
	}
	m.credentialGroupResolverMu.Lock()
	m.credentialGroupResolver = resolver
	m.credentialGroupResolverMu.Unlock()
}

func (m *Manager) SetCredentialOwnershipResolver(resolver CredentialOwnershipResolver) {
	if m == nil {
		return
	}
	m.credentialOwnershipResolverMu.Lock()
	m.credentialOwnershipResolver = resolver
	m.credentialOwnershipResolverMu.Unlock()
}

type clientCredentialAccess struct {
	active         bool
	allowAll       bool
	allowUngrouped bool
	groupIDs       []int64
}

func clientCredentialAccessFromMetadata(metadata map[string]any) clientCredentialAccess {
	if len(metadata) == 0 || strings.TrimSpace(contextStringValue(metadata[cliproxyexecutor.ClientKeyIDMetadataKey])) == "" {
		return clientCredentialAccess{}
	}
	access := clientCredentialAccess{
		active:         true,
		allowAll:       contextBoolValue(metadata[cliproxyexecutor.ClientAllowAllGroupsMetadataKey]),
		allowUngrouped: contextBoolValue(metadata[cliproxyexecutor.ClientAllowUngroupedMetadataKey]),
	}
	access.groupIDs = contextInt64Slice(metadata[cliproxyexecutor.ClientGroupIDsMetadataKey])
	return access
}

func contextBoolValue(raw any) bool {
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		parsed, errParse := strconv.ParseBool(strings.TrimSpace(value))
		return errParse == nil && parsed
	default:
		return false
	}
}

func contextInt64Slice(raw any) []int64 {
	appendValue := func(out []int64, value int64) []int64 {
		if value <= 0 {
			return out
		}
		for _, current := range out {
			if current == value {
				return out
			}
		}
		return append(out, value)
	}
	values := make([]int64, 0)
	switch typed := raw.(type) {
	case string:
		for _, part := range strings.Split(typed, ",") {
			if parsed, errParse := strconv.ParseInt(strings.TrimSpace(part), 10, 64); errParse == nil {
				values = appendValue(values, parsed)
			}
		}
	case []int64:
		for _, value := range typed {
			values = appendValue(values, value)
		}
	case []int:
		for _, value := range typed {
			values = appendValue(values, int64(value))
		}
	case []any:
		for _, value := range typed {
			switch item := value.(type) {
			case int64:
				values = appendValue(values, item)
			case int:
				values = appendValue(values, int64(item))
			case float64:
				values = appendValue(values, int64(item))
			case string:
				if parsed, errParse := strconv.ParseInt(strings.TrimSpace(item), 10, 64); errParse == nil {
					values = appendValue(values, parsed)
				}
			}
		}
	}
	return values
}

func (m *Manager) credentialGroupFilteringRequired(metadata map[string]any) bool {
	access := clientCredentialAccessFromMetadata(metadata)
	if !access.active || access.allowAll {
		return false
	}
	m.credentialGroupResolverMu.RLock()
	resolver := m.credentialGroupResolver
	m.credentialGroupResolverMu.RUnlock()
	return resolver != nil
}

// TenantIDFromMetadata extracts the tenant identity propagated by client-access.
func TenantIDFromMetadata(metadata map[string]any) int64 {
	if len(metadata) == 0 {
		return 0
	}
	value := contextStringValue(metadata[cliproxyexecutor.ClientTenantIDMetadataKey])
	tenantID, errParse := strconv.ParseInt(value, 10, 64)
	if errParse != nil || tenantID <= 0 {
		return 0
	}
	return tenantID
}

func tenantIDFromMetadata(metadata map[string]any) int64 {
	return TenantIDFromMetadata(metadata)
}

func tenantIDFromAuth(candidate *Auth) (int64, bool) {
	if candidate == nil || len(candidate.Attributes) == 0 {
		return 0, false
	}
	rawValue, exists := candidate.Attributes[AttributeTenantID]
	if !exists {
		return 0, false
	}
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return 0, true
	}
	tenantID, errParse := strconv.ParseInt(rawValue, 10, 64)
	if errParse != nil || tenantID <= 0 {
		return 0, true
	}
	return tenantID, true
}

func (m *Manager) credentialOwnershipFilteringRequired() bool {
	if m == nil {
		return false
	}
	m.credentialOwnershipResolverMu.RLock()
	resolver := m.credentialOwnershipResolver
	m.credentialOwnershipResolverMu.RUnlock()
	if resolver != nil && resolver.HasOwnedCredentials() {
		return true
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, candidate := range m.auths {
		if _, declared := tenantIDFromAuth(candidate); declared {
			return true
		}
	}
	return false
}

func (m *Manager) credentialOwnershipCandidate(candidate *Auth, metadata map[string]any) (*Auth, bool) {
	if candidate == nil {
		return nil, false
	}
	declaredOwner, declaresTenant := tenantIDFromAuth(candidate)
	m.credentialOwnershipResolverMu.RLock()
	resolver := m.credentialOwnershipResolver
	m.credentialOwnershipResolverMu.RUnlock()
	ownerTenant := int64(0)
	if resolver != nil {
		ownerTenant = resolver.OwnerOf(candidate.Index)
	}
	if declaresTenant {
		if declaredOwner <= 0 || resolver == nil || ownerTenant != declaredOwner {
			return nil, false
		}
	}
	if ownerTenant == 0 {
		return candidate, true
	}
	if tenantIDFromMetadata(metadata) != ownerTenant {
		return nil, false
	}
	return candidate, true
}

func (m *Manager) credentialGroupCandidate(candidate *Auth, metadata map[string]any) (*Auth, bool) {
	candidate, allowedByOwnership := m.credentialOwnershipCandidate(candidate, metadata)
	if !allowedByOwnership {
		return nil, false
	}
	access := clientCredentialAccessFromMetadata(metadata)
	if !access.active || access.allowAll {
		return candidate, true
	}
	m.credentialGroupResolverMu.RLock()
	resolver := m.credentialGroupResolver
	m.credentialGroupResolverMu.RUnlock()
	if resolver == nil {
		return candidate, true
	}
	allowed, priority, override := resolver.ResolveCredentialAccess(candidate.Index, access.groupIDs, access.allowAll, access.allowUngrouped)
	if !allowed {
		return nil, false
	}
	if !override {
		return candidate, true
	}
	copyAuth := candidate.Clone()
	if copyAuth.Attributes == nil {
		copyAuth.Attributes = make(map[string]string)
	}
	copyAuth.Attributes["priority"] = strconv.Itoa(priority)
	return copyAuth, true
}

// HomeEnabledForMetadata excludes tenant client-access keys from the global
// Home control plane. Tenant requests must use their own runtime providers.
func (m *Manager) HomeEnabledForMetadata(metadata map[string]any) bool {
	return m.HomeEnabled() && TenantIDFromMetadata(metadata) <= 0
}

// TenantModelClientIDs returns the registry client IDs that are usable by the
// tenant represented by client-access metadata. It applies the same ownership
// and group checks as request routing, so a model list cannot disclose another
// tenant's providers or providers outside the calling key's groups.
func (m *Manager) TenantModelClientIDs(metadata map[string]any) []string {
	tenantID := tenantIDFromMetadata(metadata)
	if m == nil || tenantID <= 0 {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	clientIDs := make([]string, 0)
	seen := make(map[string]struct{})
	for _, candidate := range m.auths {
		ownerTenant, declaresTenant := tenantIDFromAuth(candidate)
		if !declaresTenant || ownerTenant != tenantID || candidate == nil || candidate.Disabled {
			continue
		}
		if _, allowed := m.credentialGroupCandidate(candidate, metadata); !allowed {
			continue
		}
		clientID := strings.TrimSpace(candidate.ID)
		if clientID == "" {
			continue
		}
		if _, exists := seen[clientID]; exists {
			continue
		}
		seen[clientID] = struct{}{}
		clientIDs = append(clientIDs, clientID)
	}
	sort.Strings(clientIDs)
	return clientIDs
}
