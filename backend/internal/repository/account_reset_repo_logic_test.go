package repository

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountResetWindowRefundDoesNotConsumeOtherAccountUsage(t *testing.T) {
	refunded := accountResetWindowRefund(10, 13)
	require.Equal(t, 10.0, refunded)

	refunded = accountResetWindowRefund(0, 3)
	require.Zero(t, refunded)
}

func TestAccountResetWindowRefundCapsAtCurrentUsage(t *testing.T) {
	refunded := accountResetWindowRefund(10, 4)
	require.Equal(t, 4.0, refunded)

	refunded = accountResetWindowRefund(-1, 6)
	require.Zero(t, refunded)
}
