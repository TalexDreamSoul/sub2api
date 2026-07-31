package service

import (
	"context"
	"errors"
	"sort"
	"time"
)

const (
	ClientSubscriptionLimitsProvider = "openai-codex"
	ClientSubscriptionLimitsScope    = "account-pool"
)

var ErrClientSubscriptionLimitsUnavailable = errors.New("client subscription limits unavailable")

type ClientSubscriptionLimitWindow struct {
	ID          string  `json:"id"`
	UsedPercent float64 `json:"used_percent"`
	ResetAt     string  `json:"reset_at,omitempty"`
}

type ClientSubscriptionLimitsResponse struct {
	Provider string                          `json:"provider"`
	Scope    string                          `json:"scope"`
	AsOf     time.Time                       `json:"as_of"`
	Windows  []ClientSubscriptionLimitWindow `json:"windows"`
}

type clientSubscriptionLimitAggregate struct {
	totalUsedPercent float64
	count            int
	earliestResetAt  int64
}

type clientSubscriptionQuotaQuerier interface {
	QueryUsage(ctx context.Context, accountID int64) (*OpenAIQuotaUsage, error)
}

type ClientSubscriptionLimitsService struct {
	accountRepo  AccountRepository
	quotaService clientSubscriptionQuotaQuerier
}

func NewClientSubscriptionLimitsService(accountRepo AccountRepository, quotaService *OpenAIQuotaService) *ClientSubscriptionLimitsService {
	return &ClientSubscriptionLimitsService{accountRepo: accountRepo, quotaService: quotaService}
}

func (s *ClientSubscriptionLimitsService) Get(ctx context.Context, group *Group) (*ClientSubscriptionLimitsResponse, error) {
	if s == nil || s.accountRepo == nil || s.quotaService == nil || group == nil || group.ID <= 0 || group.Platform != PlatformOpenAI {
		return nil, ErrClientSubscriptionLimitsUnavailable
	}

	accounts, err := s.accountRepo.ListSchedulableByGroupIDAndPlatform(ctx, group.ID, PlatformOpenAI)
	if err != nil {
		return nil, err
	}

	aggregates := make(map[string]*clientSubscriptionLimitAggregate)
	seenCredentialAccounts := make(map[int64]struct{}, len(accounts))

	for i := range accounts {
		account := &accounts[i]
		if !account.IsOAuth() {
			continue
		}

		credentialAccountID := account.ID
		if account.ParentAccountID != nil {
			credentialAccountID = *account.ParentAccountID
		}
		if _, seen := seenCredentialAccounts[credentialAccountID]; seen {
			continue
		}
		seenCredentialAccounts[credentialAccountID] = struct{}{}

		usage, usageErr := s.quotaService.QueryUsage(ctx, credentialAccountID)
		if usageErr != nil {
			return nil, usageErr
		}
		addClientSubscriptionRateLimit(aggregates, usage.RateLimit)
	}

	windows := make([]ClientSubscriptionLimitWindow, 0, len(aggregates))
	for id, value := range aggregates {
		if value.count == 0 {
			continue
		}
		window := ClientSubscriptionLimitWindow{
			ID:          id,
			UsedPercent: value.totalUsedPercent / float64(value.count),
		}
		if value.earliestResetAt > 0 {
			window.ResetAt = time.Unix(value.earliestResetAt, 0).UTC().Format(time.RFC3339)
		}
		windows = append(windows, window)
	}
	if len(windows) == 0 {
		return nil, ErrClientSubscriptionLimitsUnavailable
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].ID < windows[j].ID })

	return &ClientSubscriptionLimitsResponse{
		Provider: ClientSubscriptionLimitsProvider,
		Scope:    ClientSubscriptionLimitsScope,
		AsOf:     time.Now().UTC(),
		Windows:  windows,
	}, nil
}

func addClientSubscriptionRateLimit(aggregates map[string]*clientSubscriptionLimitAggregate, rateLimit *OpenAIRateLimit) {
	if rateLimit == nil {
		return
	}
	for _, window := range []*OpenAIRateLimitWindow{rateLimit.PrimaryWindow, rateLimit.SecondaryWindow} {
		if window == nil {
			continue
		}
		id := clientSubscriptionLimitWindowID(window.LimitWindowSeconds)
		if id == "" {
			continue
		}
		value := aggregates[id]
		if value == nil {
			value = &clientSubscriptionLimitAggregate{}
			aggregates[id] = value
		}
		value.totalUsedPercent += window.UsedPercent
		value.count++
		if window.ResetAt > 0 && (value.earliestResetAt == 0 || window.ResetAt < value.earliestResetAt) {
			value.earliestResetAt = window.ResetAt
		}
	}
}

func clientSubscriptionLimitWindowID(seconds int64) string {
	switch time.Duration(seconds) * time.Second {
	case 5 * time.Hour:
		return "5h"
	case 24 * time.Hour:
		return "1d"
	case 7 * 24 * time.Hour:
		return "1w"
	default:
		return ""
	}
}
