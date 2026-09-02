//go:build integration

package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type APIKeyRuntimeCacheSuite struct {
	IntegrationRedisSuite
	cache service.APIKeyRuntimeCache
}

func (s *APIKeyRuntimeCacheSuite) SetupTest() {
	s.IntegrationRedisSuite.SetupTest()
	s.cache = NewAPIKeyRuntimeCache(s.rdb, &config.Config{
		Gateway: config.GatewayConfig{
			ConcurrencySlotTTLMinutes: 1,
		},
	})
}

func (s *APIKeyRuntimeCacheSuite) TestAPIKeySlot_AcquireCountRejectReleaseAndReacquire() {
	apiKeyID := int64(88)
	firstRequestID := "first-request"
	secondRequestID := "second-request"

	acquired, err := s.cache.AcquireAPIKeySlot(s.ctx, apiKeyID, 1, firstRequestID)
	require.NoError(s.T(), err, "first API key slot acquire must execute the single-key Lua script")
	require.True(s.T(), acquired, "the first request must acquire the only available slot")

	count, err := s.cache.GetAPIKeyConcurrency(s.ctx, apiKeyID)
	require.NoError(s.T(), err, "API key concurrency count must execute without a missing Lua key")
	require.Equal(s.T(), 1, count, "the acquired request must be counted")

	acquired, err = s.cache.AcquireAPIKeySlot(s.ctx, apiKeyID, 1, secondRequestID)
	require.NoError(s.T(), err, "a limit rejection must be a successful Lua evaluation")
	require.False(s.T(), acquired, "a second request must not exceed maxConcurrency=1")

	require.NoError(s.T(), s.cache.ReleaseAPIKeySlot(s.ctx, apiKeyID, firstRequestID))
	count, err = s.cache.GetAPIKeyConcurrency(s.ctx, apiKeyID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), 0, count, "releasing the only slot must return the count to zero")

	acquired, err = s.cache.AcquireAPIKeySlot(s.ctx, apiKeyID, 1, secondRequestID)
	require.NoError(s.T(), err)
	require.True(s.T(), acquired, "released capacity must become immediately available")

	count, err = s.cache.GetAPIKeyConcurrency(s.ctx, apiKeyID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), 1, count, "the replacement request must be counted")
}

func TestAPIKeyRuntimeCacheSuite(t *testing.T) {
	suite.Run(t, new(APIKeyRuntimeCacheSuite))
}
