package usagestats

import "time"

// AccountStats 账号使用统计
//
// cost: 账号口径费用（使用 total_cost * account_rate_multiplier）
// standard_cost: 标准费用（使用 total_cost，不含倍率）
// user_cost: 用户/API Key 口径费用（使用 actual_cost，受分组倍率影响）
type AccountStats struct {
	Requests     int64   `json:"requests"`
	Tokens       int64   `json:"tokens"`
	Cost         float64 `json:"cost"`
	StandardCost float64 `json:"standard_cost"`
	UserCost     float64 `json:"user_cost"`
}

// AccountPeriodStats is a local, gateway-observed account usage summary.
// It never claims to represent an upstream provider's global quota usage.
type AccountPeriodStats struct {
	Today      AccountStats `json:"today"`
	Last7Days  AccountStats `json:"last_7_days"`
	Last30Days AccountStats `json:"last_30_days"`
	LastUsedAt *time.Time   `json:"last_used_at,omitempty"`
}
