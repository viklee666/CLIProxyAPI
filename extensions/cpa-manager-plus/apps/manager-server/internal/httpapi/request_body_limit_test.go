package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/testutil"
)

func TestManagerJSONBodyLimitRejectsOversizedMonitoringRequest(t *testing.T) {
	handler, _ := newCompatHandler(t, testutil.NewConfig(t), nil)
	body := `{"accounts":[],"padding":"` + strings.Repeat("x", int(response.MaxManagerJSONBodyBytes)) + `"}`
	rr := testutil.Request(
		t,
		handler,
		http.MethodPost,
		"/v0/management/monitoring/account-history",
		body,
		testutil.AdminKey,
	)
	testutil.RequireStatus(t, rr, http.StatusRequestEntityTooLarge)
}
