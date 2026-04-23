package routes

// RouteAuth represents the authorization requirements for a route
type RouteAuth struct {
	Method        string
	Path          string
	Public        bool     // true if no authentication required
	RequiredRoles []string // empty if any authenticated user allowed, specific roles if restricted
	Description   string
}

// AuthMatrix defines all route authorization requirements
// This serves as the source of truth for endpoint access control
var AuthMatrix = map[string]RouteAuth{
	// Health check - public, no auth required
	"GET:/api/health": {
		Method:        "GET",
		Path:          "/api/health",
		Public:        true,
		RequiredRoles: []string{},
		Description:   "Service health check - accessible without authentication",
	},

	// Plans endpoints - readable by all authenticated users
	"GET:/api/plans": {
		Method:        "GET",
		Path:          "/api/plans",
		Public:        false,
		RequiredRoles: []string{"admin", "merchant", "customer"},
		Description:   "List plans - requires authentication, accessible to all roles",
	},

	// Subscriptions endpoints - accessible to merchants and admins (with filtering)
	"GET:/api/subscriptions": {
		Method:        "GET",
		Path:          "/api/subscriptions",
		Public:        false,
		RequiredRoles: []string{"admin", "merchant"},
		Description:   "List subscriptions - requires admin or merchant role, filtered by merchant",
	},

	"GET:/api/subscriptions/:id": {
		Method:        "GET",
		Path:          "/api/subscriptions/:id",
		Public:        false,
		RequiredRoles: []string{"admin", "merchant"},
		Description:   "Get subscription by ID - requires admin or merchant role, checks ownership",
	},

	"GET:/api/admin/diagnostics": {
		Method:        "GET",
		Path:          "/api/admin/diagnostics",
		Public:        false,
		RequiredRoles: []string{"admin"},
		Description:   "Run diagnostics - admin only",
	},

	"POST:/api/admin/reconcile": {
		Method:        "POST",
		Path:          "/api/admin/reconcile",
		Public:        false,
		RequiredRoles: []string{"admin"},
		Description:   "Run reconciliation - admin only",
	},

	"GET:/api/admin/reports": {
		Method:        "GET",
		Path:          "/api/admin/reports",
		Public:        false,
		RequiredRoles: []string{"admin"},
		Description:   "List reconciliation reports - admin only",
	},

	// Future protected routes follow this pattern:
	// POST:/api/subscriptions - create subscription (admin, merchant)
	// PUT:/api/subscriptions/:id - update subscription (admin, merchant)
	// DELETE:/api/subscriptions/:id - delete subscription (admin)
}

// GetRouteAuth returns authorization config for a route
func GetRouteAuth(method, path string) *RouteAuth {
	key := method + ":" + path
	if auth, exists := AuthMatrix[key]; exists {
		return &auth
	}
	return nil
}

// GetAuthMatrixSummary returns a formatted summary of all route authorizations
func GetAuthMatrixSummary() []RouteAuth {
	result := make([]RouteAuth, 0, len(AuthMatrix))
	for _, auth := range AuthMatrix {
		result = append(result, auth)
	}
	return result
}
