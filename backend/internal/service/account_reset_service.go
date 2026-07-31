package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type AccountResetRequest struct {
	AccountID                int64
	IdempotencyKey           string
	ActorID                  int64
	RestoreSubscriptionUsage bool
}

type accountQuotaResetter interface {
	GetByID(ctx context.Context, accountID int64) (*Account, error)
}

type AccountResetService struct {
	repo                AccountResetRepository
	accountRepo         accountQuotaResetter
	billingCache        *BillingCacheService
	subscriptionService *SubscriptionService
	notifier            *FeishuNotificationService
	workerID            string
	cancel              context.CancelFunc
	wg                  sync.WaitGroup
	mu                  sync.Mutex
}

func NewAccountResetService(repo AccountResetRepository, accountRepo AccountRepository, billingCache *BillingCacheService, subscriptionService *SubscriptionService) *AccountResetService {
	return &AccountResetService{repo: repo, accountRepo: accountRepo, billingCache: billingCache, subscriptionService: subscriptionService}
}

func (s *AccountResetService) ExecuteLocalQuotaReset(ctx context.Context, req AccountResetRequest) (*AccountResetResult, error) {
	if s == nil || s.accountRepo == nil {
		return nil, fmt.Errorf("account reset service is unavailable")
	}
	account, err := s.accountRepo.GetByID(ctx, req.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, infraerrors.NotFound("ACCOUNT_NOT_FOUND", "account not found")
	}
	if account.IsCredentialShadow() {
		return nil, infraerrors.New(http.StatusBadRequest, "SPARK_SHADOW_NO_QUOTA_RESET", "cannot reset quota for a spark shadow account; manage it on the parent account")
	}
	op, err := s.begin(ctx, req, AccountResetTypeLocalQuota, "")
	if err != nil {
		return nil, err
	}
	if op.Status == "failed" {
		return nil, infraerrors.Conflict("ACCOUNT_RESET_FAILED", "the reset operation has failed; use a new idempotency key after reviewing the cause")
	}
	if op.Status == "pending" {
		if err := s.repo.ResetLocalQuotaAndMarkSucceeded(ctx, op.ID, req.AccountID); err != nil {
			return nil, err
		}
	}
	return s.finishLocal(ctx, op)
}

func (s *AccountResetService) ExecuteOpenAICreditReset(ctx context.Context, req AccountResetRequest, reset func(context.Context, string) (*OpenAIQuotaResetResult, error)) (*OpenAIQuotaResetResult, *AccountResetResult, error) {
	redeemID, err := generateRedeemRequestID()
	if err != nil {
		return nil, nil, err
	}
	op, err := s.begin(ctx, req, AccountResetTypeOpenAICredit, redeemID)
	if err != nil {
		return nil, nil, err
	}
	if op.Status == "failed" {
		return nil, nil, infraerrors.Conflict("ACCOUNT_RESET_FAILED", "the reset operation has failed; use a new idempotency key after reviewing the cause")
	}
	var upstream *OpenAIQuotaResetResult
	if op.UpstreamResult != nil {
		persisted := *op.UpstreamResult
		persisted.ResetOperation = nil
		upstream = &persisted
	}
	if op.Status == "pending" {
		upstream, err = reset(ctx, op.UpstreamRedeemRequestID)
		if err != nil {
			// Keep the operation pending. A retry with the same key reuses the same
			// upstream redeem ID and cannot consume a second credit.
			return nil, nil, err
		}
		if err := s.repo.MarkUpstreamSucceeded(ctx, op.ID, upstream); err != nil {
			return upstream, nil, infraerrors.ServiceUnavailable("ACCOUNT_RESET_STATE_PERSIST_FAILED", "upstream reset may have succeeded; retry with the same idempotency key").WithCause(err)
		}
		op.UpstreamResult = upstream
	}
	if upstream == nil {
		// Operations created before upstream_result persistence can still be replayed
		// safely, but the historical provider payload cannot be reconstructed.
		upstream = &OpenAIQuotaResetResult{Code: "already_completed"}
	}
	local, err := s.finishLocal(ctx, op)
	return upstream, local, err
}

func (s *AccountResetService) begin(ctx context.Context, req AccountResetRequest, resetType, redeemID string) (*AccountResetOperation, error) {
	if s == nil || s.repo == nil || s.accountRepo == nil {
		return nil, fmt.Errorf("account reset service is unavailable")
	}
	if req.AccountID <= 0 || req.ActorID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_ACCOUNT_RESET", "account and administrator are required")
	}
	if req.RestoreSubscriptionUsage {
		return nil, infraerrors.BadRequest("SUBSCRIPTION_USAGE_REFUND_DISABLED", "subscription usage refunds are temporarily unavailable")
	}
	key := strings.TrimSpace(req.IdempotencyKey)
	if key == "" {
		return nil, infraerrors.BadRequest("IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key is required")
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("admin:%d|account:%d|type:%s|key:%s", req.ActorID, req.AccountID, resetType, key)))
	operationKey := hex.EncodeToString(hash[:])
	actor := req.ActorID
	op, _, err := s.repo.Begin(ctx, operationKey, req.AccountID, resetType, req.RestoreSubscriptionUsage, redeemID, &actor)
	return op, err
}

func (s *AccountResetService) finishLocal(ctx context.Context, op *AccountResetOperation) (*AccountResetResult, error) {
	if op == nil {
		return nil, fmt.Errorf("account reset operation is nil")
	}
	refund, err := s.repo.ApplySubscriptionRefund(ctx, op.ID)
	if err != nil {
		_ = s.repo.MarkLocalPending(ctx, op.ID, "subscription_refund_pending")
		slog.Error("account reset subscription refund deferred", "operation_id", op.ID, "account_id", op.AccountID, "error", err)
		return &AccountResetResult{OperationID: op.ID, Status: "local_pending", RestoreSubscriptionUsage: op.RestoreSubscriptionUsage}, nil
	}
	s.invalidateRefunds(ctx, refund)
	s.notifyRefunds(ctx, op.ID, refund)
	return &AccountResetResult{OperationID: op.ID, Status: "completed", RestoreSubscriptionUsage: op.RestoreSubscriptionUsage, Refund: *refund}, nil
}

func (s *AccountResetService) invalidateRefunds(ctx context.Context, refund *AccountResetRefundResult) {
	if refund == nil {
		return
	}
	seen := make(map[[2]int64]struct{})
	for _, item := range refund.Adjustments {
		key := [2]int64{item.UserID, item.GroupID}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if s.subscriptionService != nil {
			if err := s.subscriptionService.InvalidateSubscriptionCaches(item.UserID, item.GroupID); err != nil {
				slog.Warn("account reset subscription cache invalidation failed", "user_id", item.UserID, "group_id", item.GroupID, "error", err)
			}
			continue
		}
		if s.billingCache != nil {
			if err := s.billingCache.InvalidateSubscription(ctx, item.UserID, item.GroupID); err != nil {
				slog.Warn("account reset subscription cache invalidation failed", "user_id", item.UserID, "group_id", item.GroupID, "error", err)
			}
		}
	}
}

func (s *AccountResetService) notifyRefunds(ctx context.Context, operationID int64, refund *AccountResetRefundResult) {
	if s.notifier == nil || refund == nil {
		return
	}
	if err := s.notifier.QueueAccountResetRefund(ctx, operationID, refund); err != nil {
		slog.Warn("account reset refund notification enqueue failed", "operation_id", operationID, "error", err)
	}
}

func (s *AccountResetService) Start() {
	if s == nil || s.repo == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.workerID = fmt.Sprintf("account-reset-%d-%d", os.Getpid(), time.Now().UnixNano())
	s.wg.Add(1)
	go s.run(ctx)
}
func (s *AccountResetService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		s.wg.Wait()
	}
}
func (s *AccountResetService) run(ctx context.Context) {
	defer s.wg.Done()
	s.retryLocalPending(ctx)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.retryLocalPending(ctx)
		}
	}
}
func (s *AccountResetService) retryLocalPending(ctx context.Context) {
	items, err := s.repo.ClaimLocalPending(ctx, s.workerID, 20, time.Minute)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("account reset retry claim failed", "error", err)
		}
		return
	}
	for i := range items {
		op := &items[i]
		refund, applyErr := s.repo.ApplySubscriptionRefund(ctx, op.ID)
		if applyErr != nil {
			_ = s.repo.MarkLocalPending(ctx, op.ID, "subscription_refund_pending")
			continue
		}
		s.invalidateRefunds(ctx, refund)
		s.notifyRefunds(ctx, op.ID, refund)
	}
}
