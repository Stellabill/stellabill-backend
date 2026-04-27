package auth

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleMerchant Role = "merchant"
	RoleCustomer Role = "customer"
)

type Permission string

const (
	PermReadPlans           Permission = "read:plans"
	PermReadSubscriptions   Permission = "read:subscriptions"
	PermManagePlans         Permission = "manage:plans"
	PermManageSubscriptions Permission = "manage:subscriptions"
)

var rolePermissions = map[Role][]Permission{
	RoleAdmin: {
		PermReadPlans,
		PermReadSubscriptions,
		PermManagePlans,
		PermManageSubscriptions,
	},
	RoleMerchant: {
		PermReadPlans,
		PermReadSubscriptions,
	},
	RoleCustomer: {
		PermReadPlans,
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

