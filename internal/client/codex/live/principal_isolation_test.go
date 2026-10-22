package live

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLiveOwnershipUsesAuthenticatedPrincipalInsteadOfHomeUsageKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/live/call-123", nil)
	c.Set("userApiKey", "shared-home-usage-key")
	c.Set("accessPrincipal", "caller-a")
	c.Set("accessProvider", "config-api-key")
	session := liveSession{ownerPrincipal: "caller-a", ownerProvider: "config-api-key"}
	if !authorizeLiveSession(c, session) {
		t.Fatal("a Home usage-key rewrite rejected the original authenticated owner")
	}
	c.Set("accessPrincipal", "caller-b")
	if authorizeLiveSession(c, session) {
		t.Fatal("two callers sharing a Home usage key were treated as the same session owner")
	}
}
