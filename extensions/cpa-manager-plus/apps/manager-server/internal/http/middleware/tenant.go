package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/tenantauth"
)

type tenantSubjectContextKey struct{}

// WithTenantSession is the single authorization boundary for tenant-owned
// Manager Plus routes. It is intentionally independent from AdminAuthService.
func WithTenantSession(service *tenantauth.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			response.Error(w, http.StatusServiceUnavailable, tenantauth.ErrUnavailable)
			return
		}
		subject, errAuthenticate := service.Authenticate(r.Context(), tenantauth.BearerToken(r))
		if errAuthenticate != nil {
			status := http.StatusUnauthorized
			if errors.Is(errAuthenticate, tenantauth.ErrUnavailable) {
				status = http.StatusServiceUnavailable
			}
			response.Error(w, status, errAuthenticate)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tenantSubjectContextKey{}, subject)))
	})
}

func TenantSubject(ctx context.Context) (tenantauth.Subject, bool) {
	if ctx == nil {
		return tenantauth.Subject{}, false
	}
	subject, ok := ctx.Value(tenantSubjectContextKey{}).(tenantauth.Subject)
	return subject, ok && subject.TenantID > 0
}
