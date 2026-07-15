package management

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestReadLatestReleaseInfoUsesResponseLimit(t *testing.T) {
	info, err := readLatestReleaseInfo(strings.NewReader(`{"tag_name":"v1.2.3"}`))
	if err != nil {
		t.Fatalf("decode valid response: %v", err)
	}
	if info.TagName != "v1.2.3" {
		t.Fatalf("tag name = %q, want %q", info.TagName, "v1.2.3")
	}

	_, err = readLatestReleaseInfo(bytes.NewReader(bytes.Repeat([]byte("x"), maxLatestReleaseBytes+1)))
	if !errors.Is(err, errManagementBodyTooLarge) {
		t.Fatalf("error = %v, want %v", err, errManagementBodyTooLarge)
	}
}
