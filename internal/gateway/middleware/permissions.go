package middleware

// Permission is a single capability a route can require. Which roles hold which
// permissions is data (the `roles` table), not code: grant or revoke a
// permission with a migration. The constants below are the only place a
// permission string may be spelled; routes must reference these, never literals.
type Permission string

const (
	// Producer capabilities.
	PermSpecUpload     Permission = "spec:upload"
	PermAnalyticsView  Permission = "analytics:view"
	PermOrdersProducer Permission = "orders:producer"

	// Administration.
	PermAdminUsersRead     Permission = "admin:users:read"
	PermAdminUsersWrite    Permission = "admin:users:write"
	PermAdminRolesRead     Permission = "admin:roles:read"
	PermAdminRolesAssign   Permission = "admin:roles:assign"
	PermAdminSpecsRead     Permission = "admin:specs:read"
	PermAdminSpecsWrite    Permission = "admin:specs:write"
	PermAdminOrdersRead    Permission = "admin:orders:read"
	PermAdminOrdersRefund  Permission = "admin:orders:refund"
	PermAdminLicensesRead  Permission = "admin:licenses:read"
	PermAdminAnalyticsRead Permission = "admin:analytics:read"
	PermAdminAuditRead     Permission = "admin:audit:read"
	PermAdminFXRead        Permission = "admin:fx:read"
	PermAdminFXWrite       Permission = "admin:fx:write"
)

// AllPermissions lists every permission the code knows about. A test keeps it
// in sync with the permission strings seeded by the migrations, so a typo on
// either side fails CI instead of silently granting nothing.
var AllPermissions = []Permission{
	PermSpecUpload,
	PermAnalyticsView,
	PermOrdersProducer,
	PermAdminUsersRead,
	PermAdminUsersWrite,
	PermAdminRolesRead,
	PermAdminRolesAssign,
	PermAdminSpecsRead,
	PermAdminSpecsWrite,
	PermAdminOrdersRead,
	PermAdminOrdersRefund,
	PermAdminLicensesRead,
	PermAdminAnalyticsRead,
	PermAdminAuditRead,
	PermAdminFXRead,
	PermAdminFXWrite,
}

func isKnownPermission(p Permission) bool {
	for _, known := range AllPermissions {
		if known == p {
			return true
		}
	}
	return false
}
