package admin

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

var accountPeriodStatsBatchCache = newSnapshotCache(30 * time.Second)

func buildAccountPeriodStatsBatchCacheKey(accountIDs []int64) string {
	if len(accountIDs) == 0 {
		return "accounts_period_stats_empty"
	}
	ids := append([]int64(nil), accountIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return "accounts_period_stats:" + strings.Join(parts, ",")
}
