package repository

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	apiKeyActiveIPKeyPrefix    = "apikey:active_ips:"
	apiKeyConcurrencyKeyPrefix = "apikey:concurrency:"
)

var bindActiveIPScript = redis.NewScript(`
	local key = KEYS[1]
	local ip = ARGV[1]
	local maxActiveIPs = tonumber(ARGV[2])
	local ttl = tonumber(ARGV[3])

	local timeResult = redis.call('TIME')
	local now = tonumber(timeResult[1])
	local expireBefore = now - ttl

	redis.call('ZREMRANGEBYSCORE', key, '-inf', expireBefore)

	local exists = redis.call('ZSCORE', key, ip)
	if exists ~= false then
		redis.call('ZADD', key, now, ip)
		redis.call('EXPIRE', key, ttl)
		return 1
	end

	local count = redis.call('ZCARD', key)
	if count < maxActiveIPs then
		redis.call('ZADD', key, now, ip)
		redis.call('EXPIRE', key, ttl)
		return 1
	end

	return 0
`)

var acquireAPIKeySlotScript = redis.NewScript(`
	redis.replicate_commands()
	local key = KEYS[1]
	local maxConcurrency = tonumber(ARGV[1])
	local ttl = tonumber(ARGV[2])
	local requestID = ARGV[3]

	local timeResult = redis.call('TIME')
	local now = tonumber(timeResult[1])
	local expireBefore = now - ttl

	redis.call('ZREMRANGEBYSCORE', key, '-inf', expireBefore)

	local exists = redis.call('ZSCORE', key, requestID)
	if exists ~= false then
		redis.call('ZADD', key, now, requestID)
		redis.call('EXPIRE', key, ttl)
		return 1
	end

	if redis.call('ZCARD', key) < maxConcurrency then
		redis.call('ZADD', key, now, requestID)
		redis.call('EXPIRE', key, ttl)
		return 1
	end

	return 0
`)

var getAPIKeyConcurrencyScript = redis.NewScript(`
	redis.replicate_commands()
	local key = KEYS[1]
	local ttl = tonumber(ARGV[1])

	local timeResult = redis.call('TIME')
	local now = tonumber(timeResult[1])
	local expireBefore = now - ttl

	redis.call('ZREMRANGEBYSCORE', key, '-inf', expireBefore)
	return redis.call('ZCARD', key)
`)

type apiKeyRuntimeCache struct {
	rdb            *redis.Client
	slotTTLSeconds int
}

func NewAPIKeyRuntimeCache(rdb *redis.Client, cfg *config.Config) service.APIKeyRuntimeCache {
	if rdb == nil {
		return nil
	}
	slotTTLMinutes := defaultSlotTTLMinutes
	if cfg != nil && cfg.Gateway.ConcurrencySlotTTLMinutes > 0 {
		slotTTLMinutes = cfg.Gateway.ConcurrencySlotTTLMinutes
	}
	return &apiKeyRuntimeCache{
		rdb:            rdb,
		slotTTLSeconds: slotTTLMinutes * 60,
	}
}

func apiKeyActiveIPKey(keyID int64) string {
	return fmt.Sprintf("%s%d", apiKeyActiveIPKeyPrefix, keyID)
}

func apiKeyConcurrencyKey(keyID int64) string {
	return fmt.Sprintf("%s%d", apiKeyConcurrencyKeyPrefix, keyID)
}

func (c *apiKeyRuntimeCache) BindActiveIP(ctx context.Context, keyID int64, clientIP string, maxActiveIPs int, idleTimeout time.Duration) (bool, error) {
	result, err := bindActiveIPScript.Run(ctx, c.rdb, []string{apiKeyActiveIPKey(keyID)}, clientIP, maxActiveIPs, int(idleTimeout.Seconds())).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (c *apiKeyRuntimeCache) RemoveActiveIP(ctx context.Context, keyID int64, clientIP string) error {
	return c.rdb.ZRem(ctx, apiKeyActiveIPKey(keyID), clientIP).Err()
}

func (c *apiKeyRuntimeCache) ClearActiveIPs(ctx context.Context, keyID int64) error {
	return c.rdb.Del(ctx, apiKeyActiveIPKey(keyID)).Err()
}

func (c *apiKeyRuntimeCache) GetActiveIPs(ctx context.Context, keyID int64, idleTimeout time.Duration) ([]service.APIKeyActiveIP, error) {
	now, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("redis TIME: %w", err)
	}
	ttlSeconds := int64(idleTimeout.Seconds())
	key := apiKeyActiveIPKey(keyID)
	cutoff := now.Unix() - ttlSeconds
	if err := c.rdb.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(cutoff, 10)).Err(); err != nil {
		return nil, err
	}
	members, err := c.rdb.ZRangeWithScores(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		_ = c.rdb.Del(ctx, key).Err()
		return []service.APIKeyActiveIP{}, nil
	}
	_ = c.rdb.Expire(ctx, key, idleTimeout).Err()
	out := make([]service.APIKeyActiveIP, 0, len(members))
	for _, member := range members {
		ip, ok := member.Member.(string)
		if !ok || ip == "" {
			continue
		}
		lastSeen := time.Unix(int64(member.Score), 0)
		expiresAt := lastSeen.Add(idleTimeout)
		remaining := int(expiresAt.Sub(now).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		out = append(out, service.APIKeyActiveIP{
			IP:                   ip,
			LastSeenAt:           lastSeen,
			ExpiresAt:            expiresAt,
			RemainingIdleSeconds: remaining,
		})
	}
	return out, nil
}

func (c *apiKeyRuntimeCache) AcquireAPIKeySlot(ctx context.Context, keyID int64, maxConcurrency int, requestID string) (bool, error) {
	result, err := acquireAPIKeySlotScript.Run(ctx, c.rdb, []string{apiKeyConcurrencyKey(keyID)}, maxConcurrency, c.slotTTLSeconds, requestID).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (c *apiKeyRuntimeCache) ReleaseAPIKeySlot(ctx context.Context, keyID int64, requestID string) error {
	return c.rdb.ZRem(ctx, apiKeyConcurrencyKey(keyID), requestID).Err()
}

func (c *apiKeyRuntimeCache) GetAPIKeyConcurrency(ctx context.Context, keyID int64) (int, error) {
	result, err := getAPIKeyConcurrencyScript.Run(ctx, c.rdb, []string{apiKeyConcurrencyKey(keyID)}, c.slotTTLSeconds).Int()
	if err != nil {
		return 0, err
	}
	return result, nil
}
