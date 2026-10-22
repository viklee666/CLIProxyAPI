package usageevent

import (
	"reflect"
	"strings"
	"testing"
)

func TestAPIKeyAuthorizationCondition(t *testing.T) {
	for _, test := range []struct {
		name          string
		authorization *APIKeyAuthorization
		wantCondition string
		wantArgs      []any
	}{
		{name: "admin", wantCondition: ""},
		{name: "nil keys deny", authorization: NewAPIKeyAuthorization(nil), wantCondition: "1 = 0"},
		{name: "empty keys deny", authorization: NewAPIKeyAuthorization([]string{}), wantCondition: "1 = 0"},
		{name: "blank keys deny", authorization: NewAPIKeyAuthorization([]string{" ", ""}), wantCondition: "1 = 0"},
		{name: "zero value denies", authorization: &APIKeyAuthorization{}, wantCondition: "1 = 0"},
		{
			name: "normalized owned keys", authorization: NewAPIKeyAuthorization([]string{" ABC ", "abc", "def"}),
			wantCondition: "lower(coalesce(api_key_hash, '')) in (?,?)", wantArgs: []any{"abc", "def"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			condition, args := test.authorization.condition()
			if condition != test.wantCondition || !reflect.DeepEqual(args, test.wantArgs) {
				t.Fatalf("condition=%q args=%v, want %q %v", condition, args, test.wantCondition, test.wantArgs)
			}
		})
	}
}

func TestAPIKeyAuthorizationCopiesOwnershipAndIntersectsFilters(t *testing.T) {
	hashes := []string{"owned"}
	authorization := NewAPIKeyAuthorization(hashes)
	hashes[0] = "foreign"
	where, args := analyticsWhere(AnalyticsFilter{
		Authorization: authorization,
		APIKeyHashes:  []string{"foreign"},
		IncludeFailed: true,
	})
	if !strings.Contains(where, "lower(coalesce(api_key_hash, '')) in (?)") ||
		!strings.Contains(where, "coalesce(api_key_hash, '') in (?)") ||
		!reflect.DeepEqual(args, []any{int64(0), int64(0), "owned", "foreign"}) {
		t.Fatalf("ownership must be copied and ANDed with caller filters: %q %v", where, args)
	}
}
