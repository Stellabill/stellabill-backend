package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"stellarbill-backend/internal/auth"
)

func requestWithoutAuth(t *testing.T, router http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	res := httptest.NewRecorder()
	req, err := http.NewRequest(method, path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	router.ServeHTTP(res, req)
	return res
}

// TestOutboxAdmin_EndpointsRequireManageReconciliation verifies that the
// outbox dead-letter inspection and requeue endpoints are registered under the
// admin group and enforce the manage:reconciliation permission. Only the admin
// role holds that permission, so customer/merchant/user callers must be
// rejected with 403 before any handler logic runs.
func TestOutboxAdmin_EndpointsRequireManageReconciliation(t *testing.T) {
	withRouteTestEnv(t)
	router := newRegisteredTestRouter(t)

	denied := []string{"customer", "merchant", "user"}
	for _, role := range denied {
		t.Run("forbidden_"+role, func(t *testing.T) {
			token := makeRouteTestJWT(t, "caller-2", "tenant-1", []string{role})

			res := performAuthorizedRequest(t, router, http.MethodGet, "/api/admin/outbox/dead-letter", token)
			if res.Code != http.StatusForbidden {
				t.Fatalf("GET dead-letter: expected 403 for %s role, got %d", role, res.Code)
			}

			res = performAuthorizedRequest(t, router, http.MethodPost, "/api/admin/outbox/some-id/requeue", token)
			if res.Code != http.StatusForbidden {
				t.Fatalf("POST requeue: expected 403 for %s role, got %d", role, res.Code)
			}
		})
	}
}

// TestOutboxAdmin_UnauthenticatedRejected verifies the endpoints are behind the
// auth middleware and reject unauthenticated callers before RBAC.
func TestOutboxAdmin_UnauthenticatedRejected(t *testing.T) {
	withRouteTestEnv(t)
	router := newRegisteredTestRouter(t)

	for _, path := range []string{"/api/admin/outbox/dead-letter", "/api/admin/outbox/some-id/requeue"} {
		r := requestWithoutAuth(t, router, http.MethodGet, path)
		if r.Code != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401 unauthenticated, got %d", path, r.Code)
		}
	}
}

// TestOutboxAdmin_AdminReachesHandler confirms an admin token passes RBAC and
// reaches the handler. With a nil/unreachable repository (no DATABASE pool in
// the route test env) the handler responds 503, proving registration and
// permission wiring are correct up to the repository boundary.
func TestOutboxAdmin_AdminReachesHandler(t *testing.T) {
	withRouteTestEnv(t)
	router := newRegisteredTestRouter(t)

	token := makeRouteTestJWT(t, "admin-1", "tenant-1", []string{string(auth.RoleAdmin)})

	res := performAuthorizedRequest(t, router, http.MethodGet, "/api/admin/outbox/dead-letter", token)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET dead-letter: expected 503 (nil repo), got %d", res.Code)
	}

	res = performAuthorizedRequest(t, router, http.MethodPost, "/api/admin/outbox/some-id/requeue", token)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST requeue: expected 503 (nil repo), got %d", res.Code)
	}
}
