package handlers

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func TestRequestExecutionMetadataCopiesClientAccessMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginContext.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	ginContext.Set("accessMetadata", map[string]string{
		coreexecutor.ClientKeyIDMetadataKey:          "42",
		coreexecutor.ClientTenantIDMetadataKey:       "7",
		coreexecutor.ClientGroupIDsMetadataKey:       "1,3",
		coreexecutor.ClientAllowAllGroupsMetadataKey: "false",
		coreexecutor.ClientAllowUngroupedMetadataKey: "true",
		coreexecutor.ClientReservationIDMetadataKey:  "car_test",
	})
	ctx := context.WithValue(context.Background(), "gin", ginContext)
	metadata := requestExecutionMetadata(ctx)
	if metadata[coreexecutor.ClientKeyIDMetadataKey] != "42" {
		t.Fatalf("client key id = %#v", metadata[coreexecutor.ClientKeyIDMetadataKey])
	}
	if metadata[coreexecutor.ClientTenantIDMetadataKey] != "7" {
		t.Fatalf("client tenant id = %#v", metadata[coreexecutor.ClientTenantIDMetadataKey])
	}
	if metadata[coreexecutor.ClientGroupIDsMetadataKey] != "1,3" {
		t.Fatalf("client group ids = %#v", metadata[coreexecutor.ClientGroupIDsMetadataKey])
	}
	if metadata[coreexecutor.ClientAllowAllGroupsMetadataKey] != false {
		t.Fatalf("allow all groups = %#v", metadata[coreexecutor.ClientAllowAllGroupsMetadataKey])
	}
	if metadata[coreexecutor.ClientAllowUngroupedMetadataKey] != true {
		t.Fatalf("allow ungrouped = %#v", metadata[coreexecutor.ClientAllowUngroupedMetadataKey])
	}
	if metadata[coreexecutor.ClientReservationIDMetadataKey] != "car_test" {
		t.Fatalf("reservation id = %#v", metadata[coreexecutor.ClientReservationIDMetadataKey])
	}
}
