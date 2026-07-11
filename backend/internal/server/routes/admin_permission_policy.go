package routes

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func resolveAdminAccessRule(method, fullPath string) middleware.AdminAccessRule {
	path := adminSubPath(fullPath)
	method = strings.ToUpper(strings.TrimSpace(method))

	if isSuperOnlyAdminPath(method, path) {
		return superOnlyRule()
	}

	action := "write"
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions || isReadOnlyAdminPost(path) {
		action = "read"
	}

	switch {
	case path == "/permissions" || path == "/compliance" || path == "/compliance/accept":
		return delegatedAdminRule()
	case path == "" || path == "/" || strings.HasPrefix(path, "/dashboard"):
		return moduleRule(service.AdminPermissionDashboardRead, service.AdminPermissionDashboardWrite, action)
	case strings.HasPrefix(path, "/ops"):
		return moduleRule(service.AdminPermissionOpsRead, service.AdminPermissionOpsWrite, action)
	case isNestedAdminPath(path, "/accounts", "scheduled-test-plans"):
		return moduleRule(service.AdminPermissionScheduledTestsRead, service.AdminPermissionScheduledTestsWrite, action)
	case isNestedAdminPath(path, "/users", "subscriptions"):
		return moduleRule(service.AdminPermissionSubscriptionsRead, service.AdminPermissionSubscriptionsWrite, action)
	case strings.HasPrefix(path, "/users"):
		return moduleRule(service.AdminPermissionUsersRead, service.AdminPermissionUsersWrite, action)
	case strings.HasPrefix(path, "/api-keys"):
		return moduleRule(service.AdminPermissionUsersRead, service.AdminPermissionUsersWrite, action)
	case strings.HasPrefix(path, "/groups"):
		return moduleRule(service.AdminPermissionGroupsRead, service.AdminPermissionGroupsWrite, action)
	case strings.HasPrefix(path, "/accounts"):
		return moduleRule(service.AdminPermissionAccountsRead, service.AdminPermissionAccountsWrite, action)
	case strings.HasPrefix(path, "/quota-status"):
		return moduleRule(service.AdminPermissionAccountsRead, service.AdminPermissionAccountsWrite, action)
	case strings.HasPrefix(path, "/openai"):
		return moduleRule(service.AdminPermissionAccountsRead, service.AdminPermissionAccountsWrite, action)
	case strings.HasPrefix(path, "/gemini"):
		return moduleRule(service.AdminPermissionAccountsRead, service.AdminPermissionAccountsWrite, action)
	case strings.HasPrefix(path, "/antigravity"):
		return moduleRule(service.AdminPermissionAccountsRead, service.AdminPermissionAccountsWrite, action)
	case strings.HasPrefix(path, "/channels"):
		return moduleRule(service.AdminPermissionChannelsRead, service.AdminPermissionChannelsWrite, action)
	case strings.HasPrefix(path, "/channel-monitors"), strings.HasPrefix(path, "/channel-monitor-templates"):
		return moduleRule(service.AdminPermissionChannelMonitorsRead, service.AdminPermissionChannelMonitorsWrite, action)
	case strings.HasPrefix(path, "/subscriptions"):
		return moduleRule(service.AdminPermissionSubscriptionsRead, service.AdminPermissionSubscriptionsWrite, action)
	case strings.HasPrefix(path, "/announcements"):
		return moduleRule(service.AdminPermissionAnnouncementsRead, service.AdminPermissionAnnouncementsWrite, action)
	case strings.HasPrefix(path, "/proxies"):
		return moduleRule(service.AdminPermissionProxiesRead, service.AdminPermissionProxiesWrite, action)
	case strings.HasPrefix(path, "/risk-control"):
		return moduleRule(service.AdminPermissionRiskControlRead, service.AdminPermissionRiskControlWrite, action)
	case strings.HasPrefix(path, "/redeem-codes"):
		return moduleRule(service.AdminPermissionRedeemCodesRead, service.AdminPermissionRedeemCodesWrite, action)
	case strings.HasPrefix(path, "/promo-codes"):
		return moduleRule(service.AdminPermissionPromoCodesRead, service.AdminPermissionPromoCodesWrite, action)
	case strings.HasPrefix(path, "/settings"):
		return moduleRule(service.AdminPermissionSettingsRead, service.AdminPermissionSettingsWrite, action)
	case strings.HasPrefix(path, "/data-management"):
		return moduleRule(service.AdminPermissionDataManagementRead, service.AdminPermissionDataManagementWrite, action)
	case strings.HasPrefix(path, "/backups"):
		return moduleRule(service.AdminPermissionBackupRead, service.AdminPermissionBackupWrite, action)
	case strings.HasPrefix(path, "/system"):
		return moduleRule(service.AdminPermissionSystemRead, service.AdminPermissionSystemWrite, action)
	case strings.HasPrefix(path, "/usage"):
		return moduleRule(service.AdminPermissionUsageRead, service.AdminPermissionUsageWrite, action)
	case strings.HasPrefix(path, "/user-attributes"):
		return moduleRule(service.AdminPermissionUserAttributesRead, service.AdminPermissionUserAttributesWrite, action)
	case strings.HasPrefix(path, "/error-passthrough-rules"):
		return moduleRule(service.AdminPermissionErrorPassthroughRead, service.AdminPermissionErrorPassthroughWrite, action)
	case strings.HasPrefix(path, "/tls-fingerprint-profiles"):
		return moduleRule(service.AdminPermissionTLSFingerprintProfilesRead, service.AdminPermissionTLSFingerprintProfilesWrite, action)
	case strings.HasPrefix(path, "/scheduled-test-plans"):
		return moduleRule(service.AdminPermissionScheduledTestsRead, service.AdminPermissionScheduledTestsWrite, action)
	case strings.HasPrefix(path, "/affiliates"):
		return moduleRule(service.AdminPermissionAffiliatesRead, service.AdminPermissionAffiliatesWrite, action)
	case strings.HasPrefix(path, "/payment"):
		return moduleRule(service.AdminPermissionPaymentRead, service.AdminPermissionPaymentWrite, action)
	}

	return superOnlyRule()
}

func adminSubPath(fullPath string) string {
	fullPath = strings.TrimSpace(fullPath)
	idx := strings.Index(fullPath, "/admin")
	if idx < 0 {
		return fullPath
	}
	out := strings.TrimPrefix(fullPath[idx:], "/admin")
	if out == "" {
		return "/"
	}
	return strings.TrimRight(out, "/")
}

func isNestedAdminPath(path, prefix, segment string) bool {
	if !strings.HasPrefix(path, prefix+"/") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == segment {
			return true
		}
	}
	return false
}

func moduleRule(readPermission, writePermission, action string) middleware.AdminAccessRule {
	if action == "read" {
		return middleware.AdminAccessRule{Permission: readPermission}
	}
	return middleware.AdminAccessRule{Permission: writePermission}
}

func superOnlyRule() middleware.AdminAccessRule {
	return middleware.AdminAccessRule{SuperOnly: true}
}

func isReadOnlyAdminPost(path string) bool {
	for _, p := range []string{
		"/dashboard/users-usage",
		"/dashboard/api-keys-usage",
		"/accounts/check-mixed-channel",
		"/accounts/sync/crs/preview",
		"/accounts/today-stats/batch",
		"/accounts/models/sync-upstream-preview",
		"/user-attributes/batch",
	} {
		if path == p {
			return true
		}
	}
	return false
}

func isSuperOnlyAdminPath(method, path string) bool {
	if strings.Contains(path, "/admin-api-key") {
		return true
	}
	if strings.HasPrefix(path, "/system/") && method != http.MethodGet {
		return true
	}
	if strings.HasPrefix(path, "/backups/") && (strings.HasSuffix(path, "/download-url") || strings.HasSuffix(path, "/restore")) {
		return true
	}
	if strings.HasPrefix(path, "/accounts/") && (strings.Contains(path, "/apply-oauth-credentials") || strings.Contains(path, "/models/sync-upstream")) {
		return true
	}
	for _, p := range []string{
		"/accounts/data",
		"/accounts/batch-update-credentials",
		"/proxies/data",
		"/redeem-codes/export",
	} {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	if strings.HasPrefix(path, "/payment/providers") && method != http.MethodGet {
		return true
	}
	if strings.HasPrefix(path, "/payment/orders/") &&
		(strings.HasSuffix(path, "/refund") || strings.HasSuffix(path, "/refund/query")) {
		return true
	}
	return false
}

func delegatedAdminRule() middleware.AdminAccessRule {
	return middleware.AdminAccessRule{AllowDelegated: true}
}
