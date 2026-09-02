package api

import (
	"net/http"
	"testing"
)

func TestManagementRegistersPostAPIKeys(t *testing.T) {
	server := newTestServerWithOptions(t, WithLocalManagementPassword("local-mgmt"))
	found := false
	for _, route := range server.engine.Routes() {
		if route.Method == http.MethodPost && route.Path == "/v0/management/api-keys" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("POST /v0/management/api-keys is not registered")
	}
}
