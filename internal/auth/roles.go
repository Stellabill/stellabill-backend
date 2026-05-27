package auth

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleMerchant Role = "merchant"
	RoleCustomer Role = "customer"
	RoleUser     Role = "user"
)

type Permission string

const (
	PermReadPlans          Permission = "read:plans"
	PermReadSubscriptions  Permission = "read:subscriptions"
	PermManagePlans        Permission = "manage:plans"
	PermManageSubscriptions Permission = "manage:subscriptions"
	PermReadStatements     Permission = "read:statements"
	PermManageStatements       Permission = "manage:statements"
	PermManageReconciliation   Permission = "manage:reconciliation"
	PermReadReconciliation     Permission = "read:reconciliation"
)

var rolePermissions = map[Role][]Permission{
	RoleAdmin: {
		PermReadPlans,
		PermReadSubscriptions,
		PermManagePlans,
		PermManageSubscriptions,
		PermReadStatements,
		PermManageStatements,
		PermManageReconciliation,
		PermReadReconciliation,
	},
	RoleMerchant: {
		PermReadPlans,
		PermReadSubscriptions,
		PermManagePlans,
		PermManageSubscriptions,
		PermReadStatements,
		PermManageStatements,
		PermReadReconciliation,
	},
	RoleCustomer: {
		PermReadPlans,
		PermReadSubscriptions,
		PermReadStatements,
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
