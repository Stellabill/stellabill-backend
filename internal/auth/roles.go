package auth

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleUser     Role = "user"
	RoleMerchant Role = "merchant"
	RoleCustomer Role = "customer"
)

type Permission string

const (
	PermReadPlans           Permission = "read:plans"
	PermReadSubscriptions   Permission = "read:subscriptions"
	PermManagePlans         Permission = "manage:plans"
	PermManageSubscriptions Permission = "manage:subscriptions"
	PermManageReconciliation Permission = "manage:reconciliation"
)

var rolePermissions = map[Role][]Permission{
	RoleAdmin: {
		PermReadPlans,
		PermReadSubscriptions,
		PermManagePlans,
		PermManageSubscriptions,
		PermManageReconciliation,
	},
	RoleMerchant: {
		PermReadPlans,
		PermReadSubscriptions,
		PermManageReconciliation,
	},
	RoleUser: {
		PermReadPlans,
		PermReadSubscriptions,
	},
}

func HasPermission(role Role, perm Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false // default deny
	}
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}
