package usageevent

// APIKeyAuthorization is an immutable, server-supplied ownership predicate,
// independent of user-selected filters. A nil authorization is unrestricted;
// a non-nil authorization with no keys denies access to every usage event.
type APIKeyAuthorization struct {
	hashes []string
}

// NewAPIKeyAuthorization copies the authenticated subject's owned key hashes.
// Even a nil or empty key set creates a deny-all predicate, not an admin scope.
func NewAPIKeyAuthorization(hashes []string) *APIKeyAuthorization {
	return &APIKeyAuthorization{hashes: append([]string(nil), hashes...)}
}

func (a *APIKeyAuthorization) condition() (string, []any) {
	if a == nil {
		return "", nil
	}
	if len(a.hashes) == 0 {
		return "1 = 0", nil
	}
	return recentPageAPIKeyCondition(a.hashes)
}
