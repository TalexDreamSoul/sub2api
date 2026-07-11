//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateOpenAIQuotaNotifyConfigValidRules(t *testing.T) {
	extra := map[string]any{
		openAIQuotaNotifyEnabledKey: true,
		openAIQuotaNotifyRulesKey: []any{
			map[string]any{"window": "7d", "remaining_percent": 10},
			map[string]any{"window": "5h", "remaining_percent": 20},
		},
	}

	require.NoError(t, ValidateOpenAIQuotaNotifyConfig(PlatformOpenAI, AccountTypeOAuth, extra))
	rules, ok := extra[openAIQuotaNotifyRulesKey].([]OpenAIQuotaNotifyRule)
	require.True(t, ok)
	require.Equal(t, []OpenAIQuotaNotifyRule{
		{Window: "5h", RemainingPercent: 20},
		{Window: "7d", RemainingPercent: 10},
	}, rules)
}

func TestValidateOpenAIQuotaNotifyConfigRejectsDuplicates(t *testing.T) {
	extra := map[string]any{
		openAIQuotaNotifyEnabledKey: true,
		openAIQuotaNotifyRulesKey: []any{
			map[string]any{"window": "5h", "remaining_percent": 20},
			map[string]any{"window": "5h", "remaining_percent": 20},
		},
	}

	err := ValidateOpenAIQuotaNotifyConfig(PlatformOpenAI, AccountTypeOAuth, extra)
	require.ErrorContains(t, err, "duplicate")
}

func TestValidateOpenAIQuotaNotifyConfigRejectsNonOAuthAccount(t *testing.T) {
	extra := map[string]any{
		openAIQuotaNotifyEnabledKey: true,
		openAIQuotaNotifyRulesKey: []any{
			map[string]any{"window": "5h", "remaining_percent": 20},
		},
	}

	err := ValidateOpenAIQuotaNotifyConfig(PlatformOpenAI, AccountTypeAPIKey, extra)
	require.ErrorContains(t, err, "only supported")
}

func TestOpenAIQuotaWindowSnapshot(t *testing.T) {
	resetAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	used, cycle, ok := openAIQuotaWindowSnapshot(map[string]any{
		"codex_5h_used_percent": 82.5,
		"codex_5h_reset_at":     resetAt,
	}, "5h")

	require.True(t, ok)
	require.Equal(t, 82.5, used)
	parsedResetAt, err := time.Parse(time.RFC3339, resetAt)
	require.NoError(t, err)
	require.Equal(t, parsedResetAt.Truncate(time.Minute).Format(time.RFC3339), cycle)
}

func TestOpenAIQuotaWindowSnapshotRejectsExpiredCycle(t *testing.T) {
	_, _, ok := openAIQuotaWindowSnapshot(map[string]any{
		"codex_7d_used_percent": 95.0,
		"codex_7d_reset_at":     time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
	}, "7d")

	require.False(t, ok)
}
