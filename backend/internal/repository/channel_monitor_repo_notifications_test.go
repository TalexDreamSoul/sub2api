package repository

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdvanceChannelMonitorIncidentStateDebouncesAndRecovers(t *testing.T) {
	streak, open, version := 0, false, int64(0)
	for attempt := 1; attempt <= 2; attempt++ {
		var event string
		streak, open, version, event = advanceChannelMonitorIncidentState(streak, open, version, true, 3)
		require.Equal(t, attempt, streak)
		require.False(t, open)
		require.Empty(t, event)
	}

	streak, open, version, event := advanceChannelMonitorIncidentState(streak, open, version, true, 3)
	require.Equal(t, 3, streak)
	require.True(t, open)
	require.EqualValues(t, 1, version)
	require.Equal(t, "incident", event)

	streak, open, version, event = advanceChannelMonitorIncidentState(streak, open, version, true, 3)
	require.Equal(t, 4, streak)
	require.True(t, open)
	require.Empty(t, event)

	streak, open, version, event = advanceChannelMonitorIncidentState(streak, open, version, false, 3)
	require.Zero(t, streak)
	require.False(t, open)
	require.EqualValues(t, 1, version)
	require.Equal(t, "recovery", event)
}
