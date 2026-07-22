package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

const (
	ContentModerationModeOff      = "off"
	ContentModerationModeObserve  = "observe"
	ContentModerationModePreBlock = "pre_block"

	contentModerationAPIKeysModeAppend  = "append"
	contentModerationAPIKeysModeReplace = "replace"

	ContentModerationActionAllow                     = "allow"
	ContentModerationActionBlock                     = "block"
	ContentModerationActionHashBlock                 = "hash_block"
	ContentModerationActionKeywordBlock              = "keyword_block"
	ContentModerationActionError                     = "error"
	ContentModerationActionBan                       = "ban"
	ContentModerationActionRecordAudit               = "record_and_audit"
	ContentModerationActionIgnore                    = "ignore"
	ContentModerationActionCyberPolicy               = "cyber_policy"
	ContentModerationRiskEventFlagged                = "flagged"
	ContentModerationRiskEventBan                    = "ban"
	ContentModerationRiskEventUnban                  = "unban"
	ContentModerationRiskEventManualSuspicious       = "manual_suspicious"
	ContentModerationRiskEventManualSuspiciousClear  = "manual_suspicious_clear"
	ContentModerationRiskEventSourceSync             = "sync_moderation"
	ContentModerationRiskEventSourceAsync            = "async_worker"
	ContentModerationRiskEventSourceBackgroundReview = "background_review"
	ContentModerationRiskEventSourceManualAdmin      = "manual_admin"
	ContentModerationRiskEventSourceSelfUnban        = "self_unban"
	ContentModerationRiskEventSourceHashBlock        = "hash_block"

	ContentModerationReviewStageRealtime   = "realtime"
	ContentModerationReviewStageAsync      = "async"
	ContentModerationReviewStageBackground = "background_review"

	ContentModerationContextStatusPending    = "pending"
	ContentModerationContextStatusProcessing = "processing"
	ContentModerationContextStatusReviewed   = "reviewed"
	ContentModerationContextStatusFailed     = "failed"
	ContentModerationContextStatusSkipped    = "skipped"

	contentModerationKeywordCategory    = "keyword"
	contentModerationModelAuditCategory = "model_audit"

	ContentModerationDecisionRuleAny             = "any"
	ContentModerationDecisionRuleAll             = "all"
	ContentModerationDecisionRuleNOfM            = "n_of_m"
	ContentModerationDecisionRuleWeightThreshold = "weight_threshold"

	ContentModerationKeywordMatchContains = "contains"
	ContentModerationKeywordMatchRegex    = "regex"

	ContentModerationKeywordModeKeywordOnly   = "keyword_only"
	ContentModerationKeywordModeKeywordAndAPI = "keyword_and_api"
	ContentModerationKeywordModeAPIOnly       = "api_only"

	contentModerationClientDelayedTriggerCategory = "延后触发"

	ContentModerationModelFilterAll     = "all"
	ContentModerationModelFilterInclude = "include"
	ContentModerationModelFilterExclude = "exclude"

	ContentModerationProtocolAnthropicMessages = "anthropic_messages"
	ContentModerationProtocolOpenAIResponses   = "openai_responses"
	ContentModerationProtocolOpenAIChat        = "openai_chat_completions"
	ContentModerationProtocolGemini            = "gemini"
	ContentModerationProtocolOpenAIImages      = "openai_images"

	ContentModerationAuditProtocolOpenAICompatible = "openai_compatible"
	ContentModerationAuditProtocolInternalGroup    = "internal_group"

	contentModerationAuditModelStatusUnknown = "unknown"
	contentModerationAuditModelStatusOK      = "ok"
	contentModerationAuditModelStatusError   = "error"

	contentModerationInternalAuditUserEmail = "content-moderation-audit@internal.local"
	contentModerationInternalAuditUserName  = "Content Moderation Audit"
	contentModerationInternalAuditKeyPrefix = "content-moderation-audit"
	contentModerationInternalAuditUserNotes = "internal content moderation audit service account"
	contentModerationInternalAuditBalance   = 1000000.0

	defaultContentModerationBaseURL   = "https://api.openai.com"
	defaultContentModerationModel     = "omni-moderation-latest"
	defaultContentModerationTimeoutMS = 3000
	maxContentModerationTimeoutMS     = 30000
	maxModerationInputRunes           = 12000
	maxModerationUserMessageWindow    = 3
	maxModerationExcerptRunes         = 240

	defaultContentModerationWorkerCount          = 4
	maxContentModerationWorkerCount              = 32
	defaultContentModerationQueueSize            = 32768
	maxContentModerationQueueSize                = 100000
	defaultContentModerationBanThreshold         = 10
	defaultContentModerationViolationWindowHours = 720
	defaultContentModerationBlockHTTPStatus      = http.StatusForbidden
	defaultContentModerationBlockMessage         = "内容审计命中风险规则，请调整输入后重试"
	defaultContentModerationCyberuseErrorCode    = "cyber_policy"
	defaultContentModerationCyberuseMessage      = "Your request was blocked by local content moderation policy."
	defaultContentModerationRetryCount           = 2
	maxContentModerationRetryCount               = 5
	defaultContentModerationHitRetentionDays     = 180
	defaultContentModerationNonHitRetentionDays  = 3
	defaultContentModerationContextRetentionDays = 3
	defaultContentModerationBanDurationMinutes   = 60
	defaultContentModerationUnbanWindowMinutes   = 5 * 60
	defaultContentModerationSecondUnbanWaitMins  = 15
	maxContentModerationRetentionDays            = 3650
	maxContentModerationNonHitRetentionDays      = 3
	maxContentModerationContextRetentionDays     = 3
	contentModerationKeyRateLimitFreezeDuration  = time.Minute
	contentModerationKeyAuthFreezeDuration       = 10 * time.Minute
	contentModerationKeyHTTPErrorFreezeDuration  = 10 * time.Second
	maxContentModerationInputImages              = 1
	maxContentModerationTestImages               = maxContentModerationInputImages
	maxContentModerationTestImageBytes           = 8 * 1024 * 1024
	maxContentModerationTestImageDataURLBytes    = 12 * 1024 * 1024
	maxContentModerationBlockedKeywords          = 10000
	maxContentModerationBlockedKeywordRunes      = 200
	maxContentModerationModelFilterModels        = 1000
	maxContentModerationModelFilterRunes         = 200
	defaultContentModerationFlaggedWeight        = 10
	defaultContentModerationBanWeight            = 40
	defaultContentModerationManualWeight         = 60
	defaultContentModerationDecayHalfLifeDays    = 90
	defaultContentModerationMaxSampleRate        = 100
	defaultContentModerationBanThresholdStep     = 30
	defaultContentModerationMinBanThreshold      = 1
	defaultContentModerationReviewBatchSize      = 5
	defaultContentModerationReviewMaxAttempts    = 3
	defaultContentModerationReviewBackoffSeconds = 300
	defaultContentModerationContextMaxBytes      = 256 * 1024
	maxContentModerationReviewBatchSize          = 100
	maxContentModerationContextMaxBytes          = 2 * 1024 * 1024

	contentModerationCleanupInterval = 24 * time.Hour
	contentModerationCleanupTimeout  = 30 * time.Minute
	contentModerationCleanupDelay    = 5 * time.Minute

	contentModerationRuntimeCacheTTL       = time.Second
	contentModerationRuntimeRefreshTimeout = 5 * time.Second
)

var contentModerationCategoryOrder = []string{
	"harassment",
	"harassment/threatening",
	"hate",
	"hate/threatening",
	"illicit",
	"illicit/violent",
	"self-harm",
	"self-harm/intent",
	"self-harm/instructions",
	"sexual",
	"sexual/minors",
	"violence",
	"violence/graphic",
}

func ContentModerationDefaultThresholds() map[string]float64 {
	return map[string]float64{
		"harassment":             0.98,
		"harassment/threatening": 0.90,
		"hate":                   0.65,
		"hate/threatening":       0.65,
		"illicit":                0.95,
		"illicit/violent":        0.95,
		"self-harm":              0.65,
		"self-harm/intent":       0.85,
		"self-harm/instructions": 0.65,
		"sexual":                 0.65,
		"sexual/minors":          0.65,
		"violence":               0.95,
		"violence/graphic":       0.95,
	}
}

func ContentModerationCategories() []string {
	out := make([]string, len(contentModerationCategoryOrder))
	copy(out, contentModerationCategoryOrder)
	return out
}

type ContentModerationConfig struct {
	Enabled                             bool                                `json:"enabled"`
	Mode                                string                              `json:"mode"`
	BaseURL                             string                              `json:"base_url"`
	Model                               string                              `json:"model"`
	APIKey                              string                              `json:"api_key,omitempty"`
	APIKeys                             []string                            `json:"api_keys,omitempty"`
	TimeoutMS                           int                                 `json:"timeout_ms"`
	SampleRate                          int                                 `json:"sample_rate"`
	AllGroups                           bool                                `json:"all_groups"`
	GroupIDs                            []int64                             `json:"group_ids"`
	RecordNonHits                       bool                                `json:"record_non_hits"`
	Thresholds                          map[string]float64                  `json:"thresholds"`
	WorkerCount                         int                                 `json:"worker_count"`
	QueueSize                           int                                 `json:"queue_size"`
	BlockStatus                         int                                 `json:"block_status"`
	BlockMessage                        string                              `json:"block_message"`
	EmailOnHit                          bool                                `json:"email_on_hit"`
	AutoBanEnabled                      bool                                `json:"auto_ban_enabled"`
	BanThreshold                        int                                 `json:"ban_threshold"`
	BanDurationMinutes                  int                                 `json:"ban_duration_minutes"`
	ViolationWindowHours                int                                 `json:"violation_window_hours"`
	RetryCount                          int                                 `json:"retry_count"`
	HitRetentionDays                    int                                 `json:"hit_retention_days"`
	NonHitRetentionDays                 int                                 `json:"non_hit_retention_days"`
	ContextRetentionDays                int                                 `json:"context_retention_days"`
	PreHashCheckEnabled                 bool                                `json:"pre_hash_check_enabled"`
	BlockedKeywords                     []string                            `json:"blocked_keywords"`
	KeywordBlockingMode                 string                              `json:"keyword_blocking_mode"`
	KeywordRules                        []ContentModerationKeywordRule      `json:"keyword_rules"`
	ModelFilter                         ContentModerationModelFilter        `json:"model_filter"`
	AuditModels                         []ContentModerationAuditModelConfig `json:"audit_models"`
	DecisionRule                        ContentModerationDecisionRule       `json:"decision_rule"`
	SelfUnban                           ContentModerationSelfUnbanConfig    `json:"self_unban"`
	RiskWeightEnabled                   bool                                `json:"risk_weight_enabled"`
	FlaggedWeight                       float64                             `json:"flagged_weight"`
	BanWeight                           float64                             `json:"ban_weight"`
	ManualSuspiciousWeight              float64                             `json:"manual_suspicious_weight"`
	DecayHalfLifeDays                   int                                 `json:"decay_half_life_days"`
	MaxSampleRate                       int                                 `json:"max_sample_rate"`
	BanThresholdWeightStep              int                                 `json:"ban_threshold_weight_step"`
	MinEffectiveBanThreshold            int                                 `json:"min_effective_ban_threshold"`
	BackgroundReviewEnabled             bool                                `json:"background_review_enabled"`
	BackgroundReviewBatchSize           int                                 `json:"background_review_batch_size"`
	BackgroundReviewMaxAttempts         int                                 `json:"background_review_max_attempts"`
	BackgroundReviewRetryBackoffSeconds int                                 `json:"background_review_retry_backoff_seconds"`
	ContextCaptureEnabled               bool                                `json:"context_capture_enabled"`
	ContextMaxBytes                     int                                 `json:"context_max_bytes"`
	CyberuseResponse                    ContentModerationCyberuseConfig     `json:"cyberuse_response"`
	CyberPolicyExcludeFromBanCount      bool                                `json:"cyber_policy_exclude_from_ban_count"`
}

type ContentModerationConfigView struct {
	Enabled                             bool                                `json:"enabled"`
	Mode                                string                              `json:"mode"`
	BaseURL                             string                              `json:"base_url"`
	Model                               string                              `json:"model"`
	APIKeyConfigured                    bool                                `json:"api_key_configured"`
	APIKeyMasked                        string                              `json:"api_key_masked"`
	APIKeyCount                         int                                 `json:"api_key_count"`
	APIKeyMasks                         []string                            `json:"api_key_masks"`
	APIKeyStatuses                      []ContentModerationAPIKeyStatus     `json:"api_key_statuses"`
	TimeoutMS                           int                                 `json:"timeout_ms"`
	SampleRate                          int                                 `json:"sample_rate"`
	AllGroups                           bool                                `json:"all_groups"`
	GroupIDs                            []int64                             `json:"group_ids"`
	RecordNonHits                       bool                                `json:"record_non_hits"`
	Thresholds                          map[string]float64                  `json:"thresholds"`
	WorkerCount                         int                                 `json:"worker_count"`
	QueueSize                           int                                 `json:"queue_size"`
	BlockStatus                         int                                 `json:"block_status"`
	BlockMessage                        string                              `json:"block_message"`
	EmailOnHit                          bool                                `json:"email_on_hit"`
	AutoBanEnabled                      bool                                `json:"auto_ban_enabled"`
	BanThreshold                        int                                 `json:"ban_threshold"`
	BanDurationMinutes                  int                                 `json:"ban_duration_minutes"`
	ViolationWindowHours                int                                 `json:"violation_window_hours"`
	RetryCount                          int                                 `json:"retry_count"`
	HitRetentionDays                    int                                 `json:"hit_retention_days"`
	NonHitRetentionDays                 int                                 `json:"non_hit_retention_days"`
	ContextRetentionDays                int                                 `json:"context_retention_days"`
	PreHashCheckEnabled                 bool                                `json:"pre_hash_check_enabled"`
	BlockedKeywords                     []string                            `json:"blocked_keywords"`
	KeywordBlockingMode                 string                              `json:"keyword_blocking_mode"`
	KeywordRules                        []ContentModerationKeywordRule      `json:"keyword_rules"`
	ModelFilter                         ContentModerationModelFilter        `json:"model_filter"`
	AuditModels                         []ContentModerationAuditModelConfig `json:"audit_models"`
	DecisionRule                        ContentModerationDecisionRule       `json:"decision_rule"`
	SelfUnban                           ContentModerationSelfUnbanConfig    `json:"self_unban"`
	RiskWeightEnabled                   bool                                `json:"risk_weight_enabled"`
	FlaggedWeight                       float64                             `json:"flagged_weight"`
	BanWeight                           float64                             `json:"ban_weight"`
	ManualSuspiciousWeight              float64                             `json:"manual_suspicious_weight"`
	DecayHalfLifeDays                   int                                 `json:"decay_half_life_days"`
	MaxSampleRate                       int                                 `json:"max_sample_rate"`
	BanThresholdWeightStep              int                                 `json:"ban_threshold_weight_step"`
	MinEffectiveBanThreshold            int                                 `json:"min_effective_ban_threshold"`
	BackgroundReviewEnabled             bool                                `json:"background_review_enabled"`
	BackgroundReviewBatchSize           int                                 `json:"background_review_batch_size"`
	BackgroundReviewMaxAttempts         int                                 `json:"background_review_max_attempts"`
	BackgroundReviewRetryBackoffSeconds int                                 `json:"background_review_retry_backoff_seconds"`
	ContextCaptureEnabled               bool                                `json:"context_capture_enabled"`
	ContextMaxBytes                     int                                 `json:"context_max_bytes"`
	CyberuseResponse                    ContentModerationCyberuseConfig     `json:"cyberuse_response"`
	CyberPolicyExcludeFromBanCount      bool                                `json:"cyber_policy_exclude_from_ban_count"`
}

type ContentModerationAuditModelConfig struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Enabled          bool    `json:"enabled"`
	Protocol         string  `json:"protocol"`
	BaseURL          string  `json:"base_url"`
	APIKey           string  `json:"api_key,omitempty"`
	Model            string  `json:"model"`
	GroupID          *int64  `json:"group_id,omitempty"`
	GroupName        string  `json:"group_name,omitempty"`
	InternalAPIKeyID *int64  `json:"internal_api_key_id,omitempty"`
	Temperature      float64 `json:"temperature"`
	TimeoutMS        int     `json:"timeout_ms"`
	PromptTemplate   string  `json:"prompt_template"`
	Weight           float64 `json:"weight"`
}

type ContentModerationDecisionRule struct {
	Type            string  `json:"type"`
	RequiredCount   int     `json:"required_count"`
	WeightThreshold float64 `json:"weight_threshold"`
}

type ContentModerationSelfUnbanConfig struct {
	Enabled                  bool `json:"enabled"`
	WindowMinutes            int  `json:"window_minutes"`
	MaxAttempts              int  `json:"max_attempts"`
	SecondAttemptWaitMinutes int  `json:"second_attempt_wait_minutes"`
}

const (
	ContentModerationCyberuseUserScopeAll     = "all"
	ContentModerationCyberuseUserScopeInclude = "include"
	ContentModerationCyberuseUserScopeExclude = "exclude"
)

type ContentModerationCyberuseConfig struct {
	Enabled              bool                               `json:"enabled"`
	EmitToClient         bool                               `json:"emit_to_client"`
	ErrorCode            string                             `json:"error_code"`
	Message              string                             `json:"message"`
	IncludeRequestID     bool                               `json:"include_request_id"`
	AuditMetadataEnabled bool                               `json:"audit_metadata_enabled"`
	AnnouncementEnabled  bool                               `json:"announcement_enabled"`
	AnnouncementTitle    string                             `json:"announcement_title"`
	AnnouncementContent  string                             `json:"announcement_content"`
	UserScope            ContentModerationCyberuseUserScope `json:"user_scope"`
}

type ContentModerationCyberuseUserScope struct {
	Mode    string  `json:"mode"`
	UserIDs []int64 `json:"user_ids"`
}

type ContentModerationKeywordRule struct {
	ID         string   `json:"id"`
	Group      string   `json:"group"`
	MatchType  string   `json:"match_type"`
	Patterns   []string `json:"patterns"`
	Fields     []string `json:"fields"`
	Whitelist  bool     `json:"whitelist"`
	Priority   int      `json:"priority"`
	Actions    []string `json:"actions"`
	Enabled    bool     `json:"enabled"`
	IgnoreCase bool     `json:"ignore_case"`
}

type ContentModerationKeywordHit struct {
	RuleID      string `json:"rule_id"`
	Group       string `json:"group"`
	MatchType   string `json:"match_type"`
	Keyword     string `json:"keyword"`
	MatchedText string `json:"matched_text"`
	Field       string `json:"field"`
	Action      string `json:"action"`
	Whitelist   bool   `json:"whitelist"`
	Priority    int    `json:"priority"`
}

type ContentModerationAuditContext struct {
	Request     ContentModerationRequestContext     `json:"request"`
	Response    any                                 `json:"response,omitempty"`
	KeywordHits []ContentModerationKeywordHit       `json:"keyword_hits,omitempty"`
	ModelAudits []ContentModerationModelAuditDetail `json:"model_audits,omitempty"`
	Decision    *ContentModerationAggregateDecision `json:"decision,omitempty"`
	Metadata    *ContentModerationPolicyMetadata    `json:"metadata,omitempty"`
	FinalAction string                              `json:"final_action"`
}

type ContentModerationPolicyMetadata struct {
	Source              string `json:"source"`
	Origin              string `json:"origin"`
	PolicySignal        string `json:"policy_signal"`
	UpstreamPolicy      bool   `json:"upstream_policy"`
	RequestID           string `json:"request_id"`
	UserID              int64  `json:"user_id"`
	UserEmail           string `json:"user_email"`
	APIKeyID            int64  `json:"api_key_id"`
	APIKeyName          string `json:"api_key_name"`
	GroupID             *int64 `json:"group_id,omitempty"`
	GroupName           string `json:"group_name"`
	Endpoint            string `json:"endpoint"`
	Provider            string `json:"provider"`
	Model               string `json:"model"`
	Protocol            string `json:"protocol"`
	InputHash           string `json:"input_hash,omitempty"`
	ContextID           *int64 `json:"context_id,omitempty"`
	Action              string `json:"action"`
	HighestCategory     string `json:"highest_category"`
	ClientErrorCode     string `json:"client_error_code,omitempty"`
	ClientMessage       string `json:"client_message,omitempty"`
	AnnouncementEnabled bool   `json:"announcement_enabled"`
	AnnouncementTitle   string `json:"announcement_title,omitempty"`
	AnnouncementContent string `json:"announcement_content,omitempty"`
}

type ContentModerationRequestContext struct {
	RequestID  string `json:"request_id"`
	UserID     int64  `json:"user_id"`
	UserEmail  string `json:"user_email"`
	APIKeyID   int64  `json:"api_key_id"`
	APIKeyName string `json:"api_key_name"`
	GroupID    *int64 `json:"group_id,omitempty"`
	GroupName  string `json:"group_name"`
	Endpoint   string `json:"endpoint"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Protocol   string `json:"protocol"`
	Input      string `json:"input"`
}

type ContentModerationModelAuditDetail struct {
	ModelID     string                       `json:"model_id"`
	Model       string                       `json:"model"`
	Prompt      string                       `json:"prompt"`
	RawResponse string                       `json:"raw_response"`
	Result      ContentModerationModelResult `json:"result"`
	LatencyMS   int                          `json:"latency_ms"`
	Error       string                       `json:"error,omitempty"`
}

type ContentModerationModelResult struct {
	Violation  bool     `json:"violation"`
	RiskScore  float64  `json:"risk_score"`
	Reason     string   `json:"reason"`
	Categories []string `json:"categories"`
}

type ContentModerationAggregateDecision struct {
	Flagged         bool    `json:"flagged"`
	RuleType        string  `json:"rule_type"`
	ViolationCount  int     `json:"violation_count"`
	TotalCount      int     `json:"total_count"`
	ViolationWeight float64 `json:"violation_weight"`
	TotalWeight     float64 `json:"total_weight"`
	Reason          string  `json:"reason"`
}

type ContentModerationBanStatus struct {
	UserID                 int64      `json:"user_id"`
	Banned                 bool       `json:"banned"`
	Reason                 string     `json:"reason"`
	TriggeredAt            *time.Time `json:"triggered_at,omitempty"`
	BannedUntil            *time.Time `json:"banned_until,omitempty"`
	RemainingSeconds       int64      `json:"remaining_seconds"`
	SelfUnbanAvailable     bool       `json:"self_unban_available"`
	SelfUnbanAttemptsUsed  int        `json:"self_unban_attempts_used"`
	SelfUnbanMaxAttempts   int        `json:"self_unban_max_attempts"`
	SelfUnbanWaitSeconds   int64      `json:"self_unban_wait_seconds"`
	SelfUnbanWindowResetAt *time.Time `json:"self_unban_window_reset_at,omitempty"`
}

type ContentModerationSelfUnbanResult struct {
	UserID        int64      `json:"user_id"`
	Unbanned      bool       `json:"unbanned"`
	Status        string     `json:"status"`
	AttemptsUsed  int        `json:"attempts_used"`
	MaxAttempts   int        `json:"max_attempts"`
	WaitSeconds   int64      `json:"wait_seconds"`
	WindowResetAt *time.Time `json:"window_reset_at,omitempty"`
	Message       string     `json:"message"`
}

type ContentModerationAPIKeyStatus struct {
	Index          int        `json:"index"`
	KeyHash        string     `json:"key_hash"`
	Masked         string     `json:"masked"`
	Status         string     `json:"status"`
	FailureCount   int        `json:"failure_count"`
	SuccessCount   int64      `json:"success_count"`
	LastError      string     `json:"last_error"`
	LastCheckedAt  *time.Time `json:"last_checked_at,omitempty"`
	FrozenUntil    *time.Time `json:"frozen_until,omitempty"`
	LastLatencyMS  int        `json:"last_latency_ms"`
	LastHTTPStatus int        `json:"last_http_status"`
	LastTested     bool       `json:"last_tested"`
	Configured     bool       `json:"configured"`
}

type ContentModerationAPIKeyLoad struct {
	Index          int    `json:"index"`
	KeyHash        string `json:"key_hash"`
	Masked         string `json:"masked"`
	Status         string `json:"status"`
	Active         int64  `json:"active"`
	Total          int64  `json:"total"`
	Success        int64  `json:"success"`
	Errors         int64  `json:"errors"`
	AvgLatencyMS   int64  `json:"avg_latency_ms"`
	LastLatencyMS  int    `json:"last_latency_ms"`
	LastHTTPStatus int    `json:"last_http_status"`
}

type TestContentModerationAPIKeysInput struct {
	APIKeys   []string `json:"api_keys"`
	BaseURL   string   `json:"base_url"`
	Model     string   `json:"model"`
	TimeoutMS int      `json:"timeout_ms"`
	Prompt    string   `json:"prompt"`
	Images    []string `json:"images"`
}

type TestContentModerationAPIKeysResult struct {
	Items       []ContentModerationAPIKeyStatus   `json:"items"`
	AuditResult *ContentModerationTestAuditResult `json:"audit_result,omitempty"`
	ImageCount  int                               `json:"image_count"`
}

type ContentModerationTestAuditResult struct {
	Flagged         bool               `json:"flagged"`
	HighestCategory string             `json:"highest_category"`
	HighestScore    float64            `json:"highest_score"`
	CompositeScore  float64            `json:"composite_score"`
	CategoryScores  map[string]float64 `json:"category_scores"`
	Thresholds      map[string]float64 `json:"thresholds"`
}

type UpdateContentModerationConfigInput struct {
	Enabled                             *bool                                `json:"enabled"`
	Mode                                *string                              `json:"mode"`
	BaseURL                             *string                              `json:"base_url"`
	Model                               *string                              `json:"model"`
	APIKey                              *string                              `json:"api_key"`
	APIKeys                             *[]string                            `json:"api_keys"`
	APIKeysMode                         string                               `json:"api_keys_mode"`
	DeleteAPIKeyHashes                  *[]string                            `json:"delete_api_key_hashes"`
	ClearAPIKey                         bool                                 `json:"clear_api_key"`
	TimeoutMS                           *int                                 `json:"timeout_ms"`
	SampleRate                          *int                                 `json:"sample_rate"`
	AllGroups                           *bool                                `json:"all_groups"`
	GroupIDs                            *[]int64                             `json:"group_ids"`
	RecordNonHits                       *bool                                `json:"record_non_hits"`
	Thresholds                          *map[string]float64                  `json:"thresholds"`
	WorkerCount                         *int                                 `json:"worker_count"`
	QueueSize                           *int                                 `json:"queue_size"`
	BlockStatus                         *int                                 `json:"block_status"`
	BlockMessage                        *string                              `json:"block_message"`
	EmailOnHit                          *bool                                `json:"email_on_hit"`
	AutoBanEnabled                      *bool                                `json:"auto_ban_enabled"`
	BanThreshold                        *int                                 `json:"ban_threshold"`
	BanDurationMinutes                  *int                                 `json:"ban_duration_minutes"`
	ViolationWindowHours                *int                                 `json:"violation_window_hours"`
	RetryCount                          *int                                 `json:"retry_count"`
	HitRetentionDays                    *int                                 `json:"hit_retention_days"`
	NonHitRetentionDays                 *int                                 `json:"non_hit_retention_days"`
	ContextRetentionDays                *int                                 `json:"context_retention_days"`
	PreHashCheckEnabled                 *bool                                `json:"pre_hash_check_enabled"`
	BlockedKeywords                     *[]string                            `json:"blocked_keywords"`
	KeywordBlockingMode                 *string                              `json:"keyword_blocking_mode"`
	KeywordRules                        *[]ContentModerationKeywordRule      `json:"keyword_rules"`
	ModelFilter                         *ContentModerationModelFilter        `json:"model_filter"`
	AuditModels                         *[]ContentModerationAuditModelConfig `json:"audit_models"`
	DecisionRule                        *ContentModerationDecisionRule       `json:"decision_rule"`
	SelfUnban                           *ContentModerationSelfUnbanConfig    `json:"self_unban"`
	RiskWeightEnabled                   *bool                                `json:"risk_weight_enabled"`
	FlaggedWeight                       *float64                             `json:"flagged_weight"`
	BanWeight                           *float64                             `json:"ban_weight"`
	ManualSuspiciousWeight              *float64                             `json:"manual_suspicious_weight"`
	DecayHalfLifeDays                   *int                                 `json:"decay_half_life_days"`
	MaxSampleRate                       *int                                 `json:"max_sample_rate"`
	BanThresholdWeightStep              *int                                 `json:"ban_threshold_weight_step"`
	MinEffectiveBanThreshold            *int                                 `json:"min_effective_ban_threshold"`
	BackgroundReviewEnabled             *bool                                `json:"background_review_enabled"`
	BackgroundReviewBatchSize           *int                                 `json:"background_review_batch_size"`
	BackgroundReviewMaxAttempts         *int                                 `json:"background_review_max_attempts"`
	BackgroundReviewRetryBackoffSeconds *int                                 `json:"background_review_retry_backoff_seconds"`
	ContextCaptureEnabled               *bool                                `json:"context_capture_enabled"`
	ContextMaxBytes                     *int                                 `json:"context_max_bytes"`
	CyberuseResponse                    *ContentModerationCyberuseConfig     `json:"cyberuse_response"`
	CyberPolicyExcludeFromBanCount      *bool                                `json:"cyber_policy_exclude_from_ban_count"`
}

type ContentModerationModelFilter struct {
	Type   string   `json:"type"`
	Models []string `json:"models"`
}

type ContentModerationCheckInput struct {
	RequestID  string
	UserID     int64
	UserEmail  string
	APIKeyID   int64
	APIKeyName string
	GroupID    *int64
	GroupName  string
	Endpoint   string
	Provider   string
	Model      string
	Protocol   string
	Body       []byte
	Response   any
}

type ContentModerationRiskSnapshot struct {
	Weight                float64 `json:"weight"`
	EffectiveSampleRate   int     `json:"effective_sample_rate"`
	EffectiveBanThreshold int     `json:"effective_ban_threshold"`
}

type ContentModerationInput struct {
	Text   string
	Images []string
}

func (in *ContentModerationInput) Normalize() {
	if in == nil {
		return
	}
	in.Text = trimRunesFromEnd(normalizeContentModerationText(in.Text), maxModerationInputRunes)
	in.Images = normalizeModerationImages(in.Images)
}

func (in ContentModerationInput) IsEmpty() bool {
	return strings.TrimSpace(in.Text) == "" && len(in.Images) == 0
}

func (in ContentModerationInput) ModerationInput() any {
	images := limitContentModerationImages(in.Images)
	if len(images) == 0 {
		return in.Text
	}
	parts := make([]moderationAPIInputPart, 0, len(images)+1)
	if strings.TrimSpace(in.Text) != "" {
		parts = append(parts, moderationAPIInputPart{Type: "text", Text: in.Text})
	}
	for _, image := range images {
		parts = append(parts, moderationAPIInputPart{
			Type:     "image_url",
			ImageURL: &moderationAPIImageURLRef{URL: image},
		})
	}
	return parts
}

func (in ContentModerationInput) ExcerptText() string {
	return in.Text
}

func (in ContentModerationInput) Hash() string {
	h := sha256.New()
	_, _ = h.Write([]byte("text:"))
	_, _ = h.Write([]byte(in.Text))
	for _, image := range in.Images {
		imageHash := sha256.Sum256([]byte(image))
		_, _ = h.Write([]byte("\nimage:"))
		_, _ = h.Write([]byte(hex.EncodeToString(imageHash[:])))
	}
	return hex.EncodeToString(h.Sum(nil))
}

type ContentModerationDecision struct {
	Allowed         bool                           `json:"allowed"`
	Blocked         bool                           `json:"blocked"`
	Flagged         bool                           `json:"flagged"`
	Message         string                         `json:"message"`
	ErrorCode       string                         `json:"error_code,omitempty"`
	StatusCode      int                            `json:"status_code"`
	InputHash       string                         `json:"input_hash,omitempty"`
	HighestCategory string                         `json:"highest_category"`
	HighestScore    float64                        `json:"highest_score"`
	CategoryScores  map[string]float64             `json:"category_scores"`
	Action          string                         `json:"action"`
	BanStatus       *ContentModerationBanStatus    `json:"ban_status,omitempty"`
	KeywordHits     []ContentModerationKeywordHit  `json:"keyword_hits,omitempty"`
	ContextID       *int64                         `json:"context_id,omitempty"`
	LogID           int64                          `json:"log_id,omitempty"`
	RiskSnapshot    *ContentModerationRiskSnapshot `json:"risk_snapshot,omitempty"`
}

type ContentModerationLog struct {
	ID                    int64                          `json:"id"`
	RequestID             string                         `json:"request_id"`
	UserID                *int64                         `json:"user_id,omitempty"`
	UserEmail             string                         `json:"user_email"`
	APIKeyID              *int64                         `json:"api_key_id,omitempty"`
	APIKeyName            string                         `json:"api_key_name"`
	GroupID               *int64                         `json:"group_id,omitempty"`
	GroupName             string                         `json:"group_name"`
	Endpoint              string                         `json:"endpoint"`
	Provider              string                         `json:"provider"`
	Model                 string                         `json:"model"`
	Protocol              string                         `json:"protocol,omitempty"`
	Mode                  string                         `json:"mode"`
	Action                string                         `json:"action"`
	Flagged               bool                           `json:"flagged"`
	HighestCategory       string                         `json:"highest_category"`
	HighestScore          float64                        `json:"highest_score"`
	MatchedKeyword        string                         `json:"matched_keyword"`
	CategoryScores        map[string]float64             `json:"category_scores"`
	ThresholdSnapshot     map[string]float64             `json:"threshold_snapshot"`
	InputExcerpt          string                         `json:"input_excerpt"`
	KeywordHits           []ContentModerationKeywordHit  `json:"keyword_hits,omitempty"`
	AuditContext          *ContentModerationAuditContext `json:"audit_context,omitempty"`
	ContextID             *int64                         `json:"context_id,omitempty"`
	UpstreamLatencyMS     *int                           `json:"upstream_latency_ms,omitempty"`
	Error                 string                         `json:"error"`
	ViolationCount        int                            `json:"violation_count"`
	AutoBanned            bool                           `json:"auto_banned"`
	EmailSent             bool                           `json:"email_sent"`
	RiskWeightSnapshot    float64                        `json:"risk_weight_snapshot"`
	EffectiveSampleRate   int                            `json:"effective_sample_rate"`
	EffectiveBanThreshold int                            `json:"effective_ban_threshold"`
	RiskEventSource       string                         `json:"risk_event_source"`
	ReviewStage           string                         `json:"review_stage"`
	UserStatus            string                         `json:"user_status"`
	QueueDelayMS          *int                           `json:"queue_delay_ms,omitempty"`
	CreatedAt             time.Time                      `json:"created_at"`
}

type ContentModerationLogFilter struct {
	Pagination pagination.PaginationParams
	Result     string
	GroupID    *int64
	Endpoint   string
	Search     string
	From       *time.Time
	To         *time.Time
}

type ContentModerationCleanupResult struct {
	DeletedHit     int64     `json:"deleted_hit"`
	DeletedNonHit  int64     `json:"deleted_non_hit"`
	DeletedContext int64     `json:"deleted_context"`
	FinishedAt     time.Time `json:"finished_at"`
}

type ContentModerationRuntimeStatus struct {
	Enabled                      bool                                       `json:"enabled"`
	RiskControlEnabled           bool                                       `json:"risk_control_enabled"`
	Mode                         string                                     `json:"mode"`
	WorkerCount                  int                                        `json:"worker_count"`
	MaxWorkers                   int                                        `json:"max_workers"`
	ActiveWorkers                int                                        `json:"active_workers"`
	IdleWorkers                  int                                        `json:"idle_workers"`
	QueueSize                    int                                        `json:"queue_size"`
	QueueLength                  int                                        `json:"queue_length"`
	QueueUsagePercent            float64                                    `json:"queue_usage_percent"`
	Enqueued                     int64                                      `json:"enqueued"`
	Dropped                      int64                                      `json:"dropped"`
	Processed                    int64                                      `json:"processed"`
	Errors                       int64                                      `json:"errors"`
	PreBlockActive               int                                        `json:"pre_block_active"`
	PreBlockChecked              int64                                      `json:"pre_block_checked"`
	PreBlockAllowed              int64                                      `json:"pre_block_allowed"`
	PreBlockBlocked              int64                                      `json:"pre_block_blocked"`
	PreBlockErrors               int64                                      `json:"pre_block_errors"`
	PreBlockAvgLatencyMS         int64                                      `json:"pre_block_avg_latency_ms"`
	PreBlockAPIKeyActive         int64                                      `json:"pre_block_api_key_active"`
	PreBlockAPIKeyAvailableCount int64                                      `json:"pre_block_api_key_available_count"`
	PreBlockAPIKeyTotalCalls     int64                                      `json:"pre_block_api_key_total_calls"`
	PreBlockAPIKeyLoads          []ContentModerationAPIKeyLoad              `json:"pre_block_api_key_loads"`
	APIKeyStatuses               []ContentModerationAPIKeyStatus            `json:"api_key_statuses"`
	AuditModelStatuses           []ContentModerationAuditModelRuntimeStatus `json:"audit_model_statuses"`
	FlaggedHashCount             int64                                      `json:"flagged_hash_count"`
	PendingContextCount          int64                                      `json:"pending_context_count"`
	ProcessingContextCount       int64                                      `json:"processing_context_count"`
	FailedContextCount           int64                                      `json:"failed_context_count"`
	LastBackgroundReviewAt       *time.Time                                 `json:"last_background_review_at,omitempty"`
	ContextTotalCount            int64                                      `json:"context_total_count"`
	ContextTotalBytes            int64                                      `json:"context_total_bytes"`
	ContextAvgBytes              int64                                      `json:"context_avg_bytes"`
	ContextDropCount             int64                                      `json:"context_drop_count"`
	ContextCaptureError          string                                     `json:"context_capture_error"`
	LastContextCaptureErrorAt    *time.Time                                 `json:"last_context_capture_error_at,omitempty"`
	LastCleanupAt                *time.Time                                 `json:"last_cleanup_at,omitempty"`
	LastCleanupDeletedHit        int64                                      `json:"last_cleanup_deleted_hit"`
	LastCleanupDeletedNonHit     int64                                      `json:"last_cleanup_deleted_non_hit"`
	LastCleanupDeletedContext    int64                                      `json:"last_cleanup_deleted_context"`
}

type ContentModerationAuditModelRuntimeStatus struct {
	ModelID           string     `json:"model_id"`
	Name              string     `json:"name"`
	Model             string     `json:"model"`
	Status            string     `json:"status"`
	SuccessCount      int64      `json:"success_count"`
	FailureCount      int64      `json:"failure_count"`
	FlaggedCount      int64      `json:"flagged_count"`
	DisagreementCount int64      `json:"disagreement_count"`
	TotalCalls        int64      `json:"total_calls"`
	AvgLatencyMS      int64      `json:"avg_latency_ms"`
	LastLatencyMS     int        `json:"last_latency_ms"`
	LastHTTPStatus    int        `json:"last_http_status"`
	LastError         string     `json:"last_error"`
	LastCheckedAt     *time.Time `json:"last_checked_at,omitempty"`
}

type ContentModerationUnbanUserResult struct {
	UserID int64  `json:"user_id"`
	Status string `json:"status"`
}

type ContentModerationDeleteHashResult struct {
	InputHash string `json:"input_hash"`
	Deleted   bool   `json:"deleted"`
}

type ContentModerationClearHashesResult struct {
	Deleted int64 `json:"deleted"`
}

type ContentModerationUserRiskProfile struct {
	UserID                 int64      `json:"user_id"`
	CurrentWeight          float64    `json:"current_weight"`
	EffectiveWeight        float64    `json:"effective_weight"`
	ManualSuspicious       bool       `json:"manual_suspicious"`
	CumulativeFlaggedCount int        `json:"cumulative_flagged_count"`
	CumulativeBanCount     int        `json:"cumulative_ban_count"`
	LastEventAt            *time.Time `json:"last_event_at,omitempty"`
	LastDecayAt            *time.Time `json:"last_decay_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type ContentModerationUserRiskEvent struct {
	ID                    int64     `json:"id"`
	UserID                int64     `json:"user_id"`
	EventType             string    `json:"event_type"`
	Source                string    `json:"source"`
	ReviewStage           string    `json:"review_stage"`
	WeightDelta           float64   `json:"weight_delta"`
	EffectiveWeightBefore float64   `json:"effective_weight_before"`
	EffectiveWeightAfter  float64   `json:"effective_weight_after"`
	Reason                string    `json:"reason"`
	LogID                 *int64    `json:"log_id,omitempty"`
	ContextID             *int64    `json:"context_id,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
}

type ContentModerationContext struct {
	ID                  int64      `json:"id"`
	RequestID           string     `json:"request_id"`
	UserID              *int64     `json:"user_id,omitempty"`
	UserEmail           string     `json:"user_email"`
	APIKeyID            *int64     `json:"api_key_id,omitempty"`
	APIKeyName          string     `json:"api_key_name"`
	GroupID             *int64     `json:"group_id,omitempty"`
	GroupName           string     `json:"group_name"`
	Endpoint            string     `json:"endpoint"`
	Provider            string     `json:"provider"`
	Model               string     `json:"model"`
	Protocol            string     `json:"protocol"`
	InputHash           string     `json:"input_hash"`
	ContextHash         string     `json:"context_hash"`
	EncryptedContext    string     `json:"-"`
	PlainContext        string     `json:"plain_context,omitempty"`
	ContextSummary      string     `json:"context_summary"`
	ContextBytes        int        `json:"context_bytes"`
	Status              string     `json:"status"`
	ReviewStage         string     `json:"review_stage"`
	ReviewAttempts      int        `json:"review_attempts"`
	MaxReviewAttempts   int        `json:"max_review_attempts"`
	NextReviewAt        time.Time  `json:"next_review_at"`
	ProcessingStartedAt *time.Time `json:"processing_started_at,omitempty"`
	ReviewedAt          *time.Time `json:"reviewed_at,omitempty"`
	LastReviewLogID     *int64     `json:"last_review_log_id,omitempty"`
	LastReviewFlagged   bool       `json:"last_review_flagged"`
	LastReviewError     string     `json:"last_review_error"`
	LastCaptureError    string     `json:"last_capture_error"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type ContentModerationContextReviewUpdate struct {
	ID                int64
	Status            string
	ReviewAttempts    int
	NextReviewAt      *time.Time
	ReviewedAt        *time.Time
	LastReviewLogID   *int64
	LastReviewFlagged bool
	LastReviewError   string
}

type ContentModerationContextStatusCounts struct {
	Pending        int64
	Processing     int64
	Failed         int64
	Total          int64
	TotalBytes     int64
	AvgBytes       int64
	LastReviewedAt *time.Time
}

type ContentModerationUserRiskDetail struct {
	Profile               *ContentModerationUserRiskProfile `json:"profile"`
	Events                []ContentModerationUserRiskEvent  `json:"events"`
	BanStatus             *ContentModerationBanStatus       `json:"ban_status"`
	EffectiveSampleRate   int                               `json:"effective_sample_rate"`
	EffectiveBanThreshold int                               `json:"effective_ban_threshold"`
}

type ContentModerationRepository interface {
	CreateLog(ctx context.Context, log *ContentModerationLog) error
	ListLogs(ctx context.Context, filter ContentModerationLogFilter) ([]ContentModerationLog, *pagination.PaginationResult, error)
	CountFlaggedByUserSince(ctx context.Context, userID int64, since time.Time, excludeCyberPolicy bool) (int, error)
	CleanupExpiredLogs(ctx context.Context, hitBefore time.Time, nonHitBefore time.Time, contextBefore time.Time) (*ContentModerationCleanupResult, error)
	UpsertUserBan(ctx context.Context, ban *ContentModerationUserBan) error
	GetActiveUserBan(ctx context.Context, userID int64, now time.Time) (*ContentModerationUserBan, error)
	ClearUserBan(ctx context.Context, userID int64, now time.Time) error
	CountSelfUnbanAttempts(ctx context.Context, userID int64, since time.Time) (int, error)
	CreateSelfUnbanRecord(ctx context.Context, record *ContentModerationSelfUnbanRecord) error
	GetUserRiskProfile(ctx context.Context, userID int64) (*ContentModerationUserRiskProfile, error)
	UpsertUserRiskProfile(ctx context.Context, profile *ContentModerationUserRiskProfile) error
	CreateUserRiskEvent(ctx context.Context, event *ContentModerationUserRiskEvent) error
	ListUserRiskEvents(ctx context.Context, userID int64, limit int) ([]ContentModerationUserRiskEvent, error)
	CreateContext(ctx context.Context, item *ContentModerationContext) error
	ClaimPendingContexts(ctx context.Context, batchSize int) ([]ContentModerationContext, error)
	UpdateContextReview(ctx context.Context, update ContentModerationContextReviewUpdate) error
	CountContextsByStatus(ctx context.Context) (*ContentModerationContextStatusCounts, error)
	ListUserContexts(ctx context.Context, userID int64, limit int) ([]ContentModerationContext, error)
	GetContextByID(ctx context.Context, contextID int64) (*ContentModerationContext, error)
	CreateContextAccessLog(ctx context.Context, contextID int64, adminUserID int64, action string) error
	UpdateLogEmailSent(ctx context.Context, id int64, sent bool) error
}

type ContentModerationUserBan struct {
	UserID      int64
	Reason      string
	TriggeredAt time.Time
	BannedUntil time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ContentModerationSelfUnbanRecord struct {
	ID             int64
	UserID         int64
	BanTriggeredAt time.Time
	AttemptNo      int
	Allowed        bool
	Reason         string
	CreatedAt      time.Time
}

type ContentModerationHashCache interface {
	RecordFlaggedInputHash(ctx context.Context, inputHash string) error
	HasFlaggedInputHash(ctx context.Context, inputHash string) (bool, error)
	DeleteFlaggedInputHash(ctx context.Context, inputHash string) (bool, error)
	ClearFlaggedInputHashes(ctx context.Context) (int64, error)
	CountFlaggedInputHashes(ctx context.Context) (int64, error)
}

type ContentModerationService struct {
	settingRepo               SettingRepository
	repo                      ContentModerationRepository
	hashCache                 ContentModerationHashCache
	groupRepo                 GroupRepository
	userRepo                  UserRepository
	apiKeyRepo                APIKeyRepository
	authCacheInvalidator      APIKeyAuthCacheInvalidator
	emailService              *EmailService
	feishuNotificationService *FeishuNotificationService
	encryptor                 SecretEncryptor
	cfg                       *config.Config
	httpClient                *http.Client
	asyncQueue                chan contentModerationTask
	workerCount               int
	apiKeyCursor              atomic.Uint64
	asyncActive               atomic.Int64
	asyncEnqueued             atomic.Int64
	asyncDropped              atomic.Int64
	asyncProcessed            atomic.Int64
	asyncErrors               atomic.Int64
	preBlockActive            atomic.Int64
	preBlockChecked           atomic.Int64
	preBlockAllowed           atomic.Int64
	preBlockBlocked           atomic.Int64
	preBlockErrors            atomic.Int64
	preBlockLatencyTotalMS    atomic.Int64
	lastCleanupUnix           atomic.Int64
	lastCleanupDeletedHit     atomic.Int64
	lastCleanupDeletedNonHit  atomic.Int64
	lastCleanupDeletedContext atomic.Int64
	contextDrops              atomic.Int64
	lastBackgroundReviewUnix  atomic.Int64
	contextErrorMu            sync.Mutex
	contextCaptureError       string
	lastContextErrorAt        time.Time
	runtimeSnapshot           atomic.Pointer[contentModerationRuntimeSnapshot]
	runtimeRefreshMu          sync.Mutex
	runtimeCacheTTL           time.Duration
	runtimeRefreshRetryAt     atomic.Int64
	keyHealthMu               sync.Mutex
	keyHealth                 map[string]*contentModerationKeyHealth
	auditModelHealthMu        sync.Mutex
	auditModelHealth          map[string]*contentModerationAuditModelHealth
}

type contentModerationRuntimeSnapshot struct {
	riskControlEnabled bool
	config             *ContentModerationConfig
	keywordMatcher     *contentModerationKeywordMatcher
	configDigest       [sha256.Size]byte
	loadedAt           time.Time
}

type contentModerationTask struct {
	input            ContentModerationCheckInput
	content          ContentModerationInput
	inputHash        string
	keywordHits      []ContentModerationKeywordHit
	forceAudit       bool
	contextID        *int64
	riskSnapshot     *ContentModerationRiskSnapshot
	log              *ContentModerationLog
	config           *ContentModerationConfig
	recordHash       bool
	applySideEffects bool
	enqueuedAt       time.Time
}

type contentModerationKeyHealth struct {
	Hash           string
	Masked         string
	FailureCount   int
	SuccessCount   int64
	LastError      string
	LastCheckedAt  time.Time
	FrozenUntil    time.Time
	LastLatencyMS  int
	LastHTTPStatus int
	LastTested     bool
	SyncActive     int64
	SyncTotal      int64
	SyncSuccess    int64
	SyncErrors     int64
	SyncLatencyMS  int64
}

type contentModerationAuditModelHealth struct {
	ModelID           string
	Name              string
	Model             string
	SuccessCount      int64
	FailureCount      int64
	FlaggedCount      int64
	DisagreementCount int64
	TotalLatencyMS    int64
	LastLatencyMS     int
	LastHTTPStatus    int
	LastError         string
	LastCheckedAt     time.Time
}

func NewContentModerationService(
	settingRepo SettingRepository,
	repo ContentModerationRepository,
	hashCache ContentModerationHashCache,
	groupRepo GroupRepository,
	userRepo UserRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	emailService *EmailService,
	encryptors ...SecretEncryptor,
) *ContentModerationService {
	var encryptor SecretEncryptor
	if len(encryptors) > 0 {
		encryptor = encryptors[0]
	}
	svc := &ContentModerationService{
		settingRepo:          settingRepo,
		repo:                 repo,
		hashCache:            hashCache,
		groupRepo:            groupRepo,
		userRepo:             userRepo,
		authCacheInvalidator: authCacheInvalidator,
		emailService:         emailService,
		encryptor:            encryptor,
		httpClient:           servertiming.InstrumentClient(nil),
		workerCount:          maxContentModerationWorkerCount,
		asyncQueue:           make(chan contentModerationTask, maxContentModerationQueueSize),
		keyHealth:            make(map[string]*contentModerationKeyHealth),
		auditModelHealth:     make(map[string]*contentModerationAuditModelHealth),
	}
	if settingRepo != nil && repo != nil {
		for i := 0; i < svc.workerCount; i++ {
			go svc.worker(i)
		}
		go svc.cleanupWorker()
	}
	return svc
}

func (s *ContentModerationService) SetAPIKeyRepository(apiKeyRepo APIKeyRepository) {
	if s == nil {
		return
	}
	s.apiKeyRepo = apiKeyRepo
}

func (s *ContentModerationService) SetConfig(cfg *config.Config) {
	if s == nil {
		return
	}
	s.cfg = cfg
}

func (s *ContentModerationService) SetFeishuNotificationService(feishuNotificationService *FeishuNotificationService) {
	if s == nil {
		return
	}
	s.feishuNotificationService = feishuNotificationService
}

func (s *ContentModerationService) GetConfig(ctx context.Context) (*ContentModerationConfigView, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	return s.configView(cfg), nil
}

func (s *ContentModerationService) UpdateConfig(ctx context.Context, input UpdateContentModerationConfigInput) (*ContentModerationConfigView, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if input.Enabled != nil {
		cfg.Enabled = *input.Enabled
	}
	if input.Mode != nil {
		cfg.Mode = strings.TrimSpace(*input.Mode)
	}
	if input.BaseURL != nil {
		cfg.BaseURL = strings.TrimSpace(*input.BaseURL)
	}
	if input.Model != nil {
		cfg.Model = strings.TrimSpace(*input.Model)
	}
	if input.TimeoutMS != nil {
		cfg.TimeoutMS = *input.TimeoutMS
	}
	if input.SampleRate != nil {
		cfg.SampleRate = *input.SampleRate
	}
	if input.WorkerCount != nil {
		cfg.WorkerCount = *input.WorkerCount
	}
	if input.QueueSize != nil {
		cfg.QueueSize = *input.QueueSize
	}
	if input.BlockStatus != nil {
		cfg.BlockStatus = *input.BlockStatus
	}
	if input.BlockMessage != nil {
		cfg.BlockMessage = strings.TrimSpace(*input.BlockMessage)
	}
	if input.EmailOnHit != nil {
		cfg.EmailOnHit = *input.EmailOnHit
	}
	if input.AutoBanEnabled != nil {
		cfg.AutoBanEnabled = *input.AutoBanEnabled
	}
	if input.BanThreshold != nil {
		cfg.BanThreshold = *input.BanThreshold
	}
	if input.BanDurationMinutes != nil {
		cfg.BanDurationMinutes = *input.BanDurationMinutes
	}
	if input.ViolationWindowHours != nil {
		cfg.ViolationWindowHours = *input.ViolationWindowHours
	}
	if input.RetryCount != nil {
		cfg.RetryCount = *input.RetryCount
	}
	if input.HitRetentionDays != nil {
		cfg.HitRetentionDays = *input.HitRetentionDays
	}
	if input.NonHitRetentionDays != nil {
		cfg.NonHitRetentionDays = *input.NonHitRetentionDays
	}
	if input.ContextRetentionDays != nil {
		cfg.ContextRetentionDays = *input.ContextRetentionDays
	}
	if input.PreHashCheckEnabled != nil {
		cfg.PreHashCheckEnabled = *input.PreHashCheckEnabled
	}
	if input.BlockedKeywords != nil {
		cfg.BlockedKeywords = normalizeBlockedKeywords(*input.BlockedKeywords)
	}
	if input.KeywordBlockingMode != nil {
		cfg.KeywordBlockingMode = strings.TrimSpace(*input.KeywordBlockingMode)
	}
	if input.KeywordRules != nil {
		cfg.KeywordRules = filterGeneratedContentModerationKeywordRules(*input.KeywordRules)
	}
	if input.ModelFilter != nil {
		cfg.ModelFilter = *input.ModelFilter
	}
	if input.AuditModels != nil {
		cfg.AuditModels = mergeContentModerationAuditModelSecrets(cfg.AuditModels, *input.AuditModels)
	}
	if input.DecisionRule != nil {
		cfg.DecisionRule = *input.DecisionRule
	}
	if input.SelfUnban != nil {
		cfg.SelfUnban = *input.SelfUnban
	}
	if input.RiskWeightEnabled != nil {
		cfg.RiskWeightEnabled = *input.RiskWeightEnabled
	}
	if input.FlaggedWeight != nil {
		cfg.FlaggedWeight = *input.FlaggedWeight
	}
	if input.BanWeight != nil {
		cfg.BanWeight = *input.BanWeight
	}
	if input.ManualSuspiciousWeight != nil {
		cfg.ManualSuspiciousWeight = *input.ManualSuspiciousWeight
	}
	if input.DecayHalfLifeDays != nil {
		cfg.DecayHalfLifeDays = *input.DecayHalfLifeDays
	}
	if input.MaxSampleRate != nil {
		cfg.MaxSampleRate = *input.MaxSampleRate
	}
	if input.BanThresholdWeightStep != nil {
		cfg.BanThresholdWeightStep = *input.BanThresholdWeightStep
	}
	if input.MinEffectiveBanThreshold != nil {
		cfg.MinEffectiveBanThreshold = *input.MinEffectiveBanThreshold
	}
	if input.BackgroundReviewEnabled != nil {
		cfg.BackgroundReviewEnabled = *input.BackgroundReviewEnabled
	}
	if input.BackgroundReviewBatchSize != nil {
		cfg.BackgroundReviewBatchSize = *input.BackgroundReviewBatchSize
	}
	if input.BackgroundReviewMaxAttempts != nil {
		cfg.BackgroundReviewMaxAttempts = *input.BackgroundReviewMaxAttempts
	}
	if input.BackgroundReviewRetryBackoffSeconds != nil {
		cfg.BackgroundReviewRetryBackoffSeconds = *input.BackgroundReviewRetryBackoffSeconds
	}
	if input.ContextCaptureEnabled != nil {
		cfg.ContextCaptureEnabled = *input.ContextCaptureEnabled
	}
	if input.ContextMaxBytes != nil {
		cfg.ContextMaxBytes = *input.ContextMaxBytes
	}
	if input.CyberuseResponse != nil {
		cfg.CyberuseResponse = *input.CyberuseResponse
	}
	if input.AllGroups != nil {
		cfg.AllGroups = *input.AllGroups
	}
	if input.GroupIDs != nil {
		cfg.GroupIDs = normalizeInt64IDs(*input.GroupIDs)
	}
	if input.RecordNonHits != nil {
		cfg.RecordNonHits = *input.RecordNonHits
	}
	if input.CyberPolicyExcludeFromBanCount != nil {
		cfg.CyberPolicyExcludeFromBanCount = *input.CyberPolicyExcludeFromBanCount
	}
	if input.Thresholds != nil {
		cfg.Thresholds = mergeContentModerationThresholds(ContentModerationDefaultThresholds(), *input.Thresholds)
	}
	if input.ClearAPIKey {
		cfg.APIKey = ""
		cfg.APIKeys = []string{}
	} else {
		apiKeysMode := normalizeContentModerationAPIKeysMode(input.APIKeysMode)
		if input.DeleteAPIKeyHashes != nil && apiKeysMode != contentModerationAPIKeysModeReplace {
			cfg.APIKeys = deleteModerationAPIKeysByHash(cfg.apiKeys(), *input.DeleteAPIKeyHashes)
			cfg.APIKey = ""
		}
		if input.APIKeys != nil {
			if apiKeysMode == contentModerationAPIKeysModeReplace {
				cfg.APIKeys = normalizeModerationAPIKeys(*input.APIKeys)
			} else {
				cfg.APIKeys = normalizeModerationAPIKeys(append(cfg.apiKeys(), *input.APIKeys...))
			}
			cfg.APIKey = ""
		}
		if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" {
			cfg.APIKeys = normalizeModerationAPIKeys(append(cfg.APIKeys, *input.APIKey))
			cfg.APIKey = ""
		}
	}
	if err := s.validateConfig(ctx, cfg); err != nil {
		return nil, err
	}
	cfg.normalize()
	if err := s.prepareInternalAuditModels(ctx, cfg); err != nil {
		return nil, err
	}
	cfg.prepareForSave()
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal content moderation config: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyContentModerationConfig, string(raw)); err != nil {
		return nil, fmt.Errorf("save content moderation config: %w", err)
	}
	s.replaceRuntimeConfig(cfg, raw)
	return s.configView(cfg), nil
}

func (s *ContentModerationService) TestAPIKeys(ctx context.Context, input TestContentModerationAPIKeysInput) (*TestContentModerationAPIKeysResult, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	keys := normalizeModerationAPIKeys(input.APIKeys)
	configured := false
	if len(keys) == 0 {
		keys = cfg.apiKeys()
		configured = true
	}
	if strings.TrimSpace(input.BaseURL) != "" {
		cfg.BaseURL = input.BaseURL
	}
	if strings.TrimSpace(input.Model) != "" {
		cfg.Model = input.Model
	}
	if input.TimeoutMS > 0 {
		cfg.TimeoutMS = input.TimeoutMS
	}
	cfg.normalize()
	testInput, imageCount, err := buildModerationTestInput(input.Prompt, input.Images)
	if err != nil {
		return nil, err
	}
	auditOnly := contentModerationTestHasAuditInput(input.Prompt, input.Images)
	if configured && auditOnly {
		key, ok := s.nextUsableAPIKey(cfg)
		if !ok {
			return &TestContentModerationAPIKeysResult{
				Items:      s.apiKeyStatuses(keys),
				ImageCount: imageCount,
			}, nil
		}
		keys = []string{key}
	}
	if len(keys) == 0 {
		return &TestContentModerationAPIKeysResult{Items: []ContentModerationAPIKeyStatus{}, ImageCount: imageCount}, nil
	}
	items := make([]ContentModerationAPIKeyStatus, 0, len(keys))
	var auditResult *ContentModerationTestAuditResult
	for idx, key := range keys {
		start := time.Now()
		httpStatus := 0
		result, err := s.callModerationOnceWithInput(ctx, cfg, key, testInput, &httpStatus)
		latency := int(time.Since(start).Milliseconds())
		keyHash := moderationAPIKeyHash(key)
		if err != nil {
			s.markAPIKeyError(key, err.Error(), latency, httpStatus)
		} else {
			s.markAPIKeySuccess(key, latency, httpStatus)
			if auditResult == nil {
				auditResult = buildContentModerationTestAuditResult(result, cfg.Thresholds)
			}
		}
		status := s.apiKeyStatusForHash(idx, keyHash, maskSecretTail(key), configured)
		status.LastTested = true
		items = append(items, status)
	}
	return &TestContentModerationAPIKeysResult{Items: items, AuditResult: auditResult, ImageCount: imageCount}, nil
}

func (s *ContentModerationService) Check(ctx context.Context, input ContentModerationCheckInput) (*ContentModerationDecision, error) {
	allow := &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	if s == nil || s.settingRepo == nil || s.repo == nil {
		slog.Info("content_moderation.skip_unavailable",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	runtimeSnapshot, err := s.loadRuntimeSnapshot(ctx)
	if err != nil {
		slog.Warn("content_moderation.skip_config_load_failed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"error", err)
		return allow, nil
	}
	if !runtimeSnapshot.riskControlEnabled {
		slog.Info("content_moderation.skip_feature_disabled",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	cfg := runtimeSnapshot.config
	inGroupScope := cfg.includesGroup(input.GroupID)
	inModelScope := cfg.includesModel(input.Model)
	slog.Info("content_moderation.config_loaded",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"group_name", input.GroupName,
		"endpoint", input.Endpoint,
		"provider", input.Provider,
		"protocol", input.Protocol,
		"model", input.Model,
		"enabled", cfg.Enabled,
		"mode", cfg.Mode,
		"all_groups", cfg.AllGroups,
		"configured_group_ids", cfg.GroupIDs,
		"in_group_scope", inGroupScope,
		"model_filter_type", cfg.ModelFilter.Type,
		"configured_models", cfg.ModelFilter.Models,
		"in_model_scope", inModelScope,
		"sample_rate", cfg.SampleRate,
		"api_key_count", len(cfg.apiKeys()),
		"pre_hash_check_enabled", cfg.PreHashCheckEnabled,
		"record_non_hits", cfg.RecordNonHits)
	if !cfg.Enabled {
		slog.Info("content_moderation.skip_config_disabled",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	if cfg.Mode == ContentModerationModeOff {
		slog.Info("content_moderation.skip_mode_off",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	if !inGroupScope {
		slog.Info("content_moderation.skip_group_out_of_scope",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"group_name", input.GroupName,
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"all_groups", cfg.AllGroups,
			"configured_group_ids", cfg.GroupIDs)
		return allow, nil
	}
	if !inModelScope {
		slog.Info("content_moderation.skip_model_out_of_scope",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"group_name", input.GroupName,
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"model", input.Model,
			"model_filter_type", cfg.ModelFilter.Type,
			"configured_models", cfg.ModelFilter.Models)
		return allow, nil
	}
	if banStatus, err := s.GetUserBanStatus(ctx, input.UserID); err == nil && banStatus != nil && banStatus.Banned {
		message := cfg.BlockMessage
		if strings.TrimSpace(message) == "" {
			message = defaultContentModerationBlockMessage
		}
		return s.decorateBlockedDecision(cfg, input, &ContentModerationDecision{
			Allowed:    false,
			Blocked:    true,
			Flagged:    true,
			Message:    fmt.Sprintf("%s，账户封禁剩余 %d 秒", message, banStatus.RemainingSeconds),
			StatusCode: cfg.BlockStatus,
			Action:     ContentModerationActionBan,
			BanStatus:  banStatus,
		}), nil
	}
	content := ExtractContentModerationInput(input.Protocol, input.Body)
	if content.IsEmpty() {
		slog.Info("content_moderation.skip_empty_input",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"body_bytes", len(input.Body))
		return allow, nil
	}
	content.Normalize()
	slog.Info("content_moderation.input_extracted",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"endpoint", input.Endpoint,
		"protocol", input.Protocol,
		"text_runes", len([]rune(content.Text)),
		"image_count", len(content.Images))
	hashText := content.Hash()
	riskSnapshot := s.riskSnapshotForUser(ctx, cfg, input.UserID)
	contextID := s.captureModerationContext(ctx, cfg, input, hashText)
	keywordHits := matchContentModerationKeywordRules(content.Text, cfg.keywordRules())
	keywordForceAudit := keywordHitsRequireAction(keywordHits, ContentModerationActionRecordAudit)
	auditModels := cfg.enabledAuditModels()
	keywordBlockingHit := cfg.KeywordBlockingMode != ContentModerationKeywordModeAPIOnly && keywordHitRequiresBlocking(keywordHits)
	if cfg.Mode == ContentModerationModePreBlock {
		if keywordBlockingHit && len(auditModels) == 0 {
			s.recordPreBlockSyncMetric(0, ContentModerationActionKeywordBlock)
			slog.Info("content_moderation.keyword_block",
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"group_id", contentModerationLogGroupID(input.GroupID),
				"endpoint", input.Endpoint,
				"protocol", input.Protocol,
				"keyword_blocking_mode", cfg.KeywordBlockingMode,
				"keyword_hits", len(keywordHits))
			scores := map[string]float64{contentModerationKeywordCategory: 1.0}
			log := s.buildLog(input, cfg, ContentModerationActionKeywordBlock, true, contentModerationKeywordCategory, 1.0, scores, content.ExcerptText(), nil, nil, "", keywordHits, nil)
			log.MatchedKeyword = firstContentModerationMatchedKeyword(keywordHits)
			s.decorateModerationLog(log, riskSnapshot, contextID, ContentModerationRiskEventSourceSync, ContentModerationReviewStageRealtime)
			s.enqueueRecord(input, cfg, log, hashText, false, true)
			return s.decorateBlockedDecision(cfg, input, &ContentModerationDecision{
				Allowed:         false,
				Blocked:         true,
				Flagged:         true,
				Message:         cfg.BlockMessage,
				StatusCode:      cfg.BlockStatus,
				HighestCategory: contentModerationKeywordCategory,
				HighestScore:    1.0,
				CategoryScores:  scores,
				Action:          ContentModerationActionKeywordBlock,
				KeywordHits:     keywordHits,
				ContextID:       contextID,
				RiskSnapshot:    riskSnapshot,
			}), nil
		}
		if cfg.KeywordBlockingMode == ContentModerationKeywordModeKeywordOnly {
			s.recordPreBlockSyncMetric(0, ContentModerationActionAllow)
			slog.Info("content_moderation.skip_api_keyword_only",
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"group_id", contentModerationLogGroupID(input.GroupID),
				"endpoint", input.Endpoint,
				"protocol", input.Protocol)
			return allow, nil
		}
	}
	if cfg.PreHashCheckEnabled && s.hashCache != nil {
		matched, err := s.hashCache.HasFlaggedInputHash(ctx, hashText)
		if err != nil {
			slog.Warn("content_moderation.hash_check_failed", "user_id", input.UserID, "endpoint", input.Endpoint, "error", err)
		}
		if matched {
			if cfg.Mode == ContentModerationModePreBlock {
				s.recordPreBlockSyncMetric(0, ContentModerationActionHashBlock)
			}
			slog.Info("content_moderation.hash_block",
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"group_id", contentModerationLogGroupID(input.GroupID),
				"endpoint", input.Endpoint,
				"protocol", input.Protocol,
				"input_hash", hashText)
			message := cfg.BlockMessage
			if message != "" {
				message = fmt.Sprintf("%s（hash: %s）", message, hashText)
			}
			scores := map[string]float64{"hash": 1.0}
			log := s.buildLog(input, cfg, ContentModerationActionHashBlock, true, "hash", 1.0, scores, content.ExcerptText(), nil, nil, "", keywordHits, nil)
			s.decorateModerationLog(log, riskSnapshot, contextID, ContentModerationRiskEventSourceHashBlock, ContentModerationReviewStageRealtime)
			s.enqueueRecord(input, cfg, log, hashText, false, false)
			return s.decorateBlockedDecision(cfg, input, &ContentModerationDecision{
				Allowed:      false,
				Blocked:      true,
				Flagged:      true,
				Message:      message,
				StatusCode:   cfg.BlockStatus,
				InputHash:    hashText,
				Action:       ContentModerationActionHashBlock,
				ContextID:    contextID,
				RiskSnapshot: riskSnapshot,
			}), nil
		}
	}
	if !keywordForceAudit && !keywordBlockingHit && !contentModerationShouldSample(hashText, riskSnapshot.EffectiveSampleRate) {
		if cfg.Mode == ContentModerationModePreBlock {
			s.recordPreBlockSyncMetric(0, ContentModerationActionAllow)
		}
		slog.Info("content_moderation.skip_sample_rate",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"sample_rate", riskSnapshot.EffectiveSampleRate)
		allow.ContextID = contextID
		allow.RiskSnapshot = riskSnapshot
		return allow, nil
	}
	if len(cfg.apiKeys()) == 0 && len(auditModels) == 0 {
		if cfg.Mode == ContentModerationModePreBlock {
			s.recordPreBlockSyncMetric(0, ContentModerationActionError)
		}
		slog.Warn("content_moderation.skip_no_audit_api_keys",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	if cfg.Mode == ContentModerationModeObserve {
		slog.Info("content_moderation.enqueue_observe",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"keyword_hits", len(keywordHits),
			"queue_len", len(s.asyncQueue))
		s.enqueueAsync(input, cfg, content, hashText, keywordHits, keywordForceAudit || len(auditModels) > 0, contextID, riskSnapshot)
		allow.ContextID = contextID
		allow.RiskSnapshot = riskSnapshot
		return allow, nil
	}

	if len(auditModels) > 0 {
		return s.checkModelAuditSync(ctx, input, cfg, content, hashText, keywordHits, nil, true, contextID, riskSnapshot, ContentModerationRiskEventSourceSync, ContentModerationReviewStageRealtime), nil
	}
	return s.checkSync(ctx, input, cfg, content, hashText, nil, true, contextID, riskSnapshot, ContentModerationRiskEventSourceSync, ContentModerationReviewStageRealtime), nil
}

func (s *ContentModerationService) checkSync(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string, queueDelay *int, allowBlock bool, contextID *int64, riskSnapshot *ContentModerationRiskSnapshot, source string, stage string) *ContentModerationDecision {
	allow := &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow, ContextID: contextID, RiskSnapshot: riskSnapshot}
	trackPreBlock := queueDelay == nil && allowBlock && cfg != nil && cfg.Mode == ContentModerationModePreBlock
	if trackPreBlock {
		s.preBlockActive.Add(1)
		defer s.preBlockActive.Add(-1)
	}
	start := time.Now()
	result, err := s.callModeration(ctx, cfg, content.ModerationInput(), trackPreBlock)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		if trackPreBlock {
			s.recordPreBlockSyncMetric(latency, ContentModerationActionError)
		}
		slog.Warn("content_moderation.audit_api_failed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"mode", cfg.Mode,
			"allow_block", allowBlock,
			"queue_delay_ms", queueDelay,
			"latency_ms", latency,
			"error", err)
		if queueDelay != nil {
			s.asyncErrors.Add(1)
		}
		if cfg.RecordNonHits {
			log := s.buildLog(input, cfg, ContentModerationActionError, false, "", 0, nil, content.ExcerptText(), &latency, queueDelay, err.Error(), nil, nil)
			s.decorateModerationLog(log, riskSnapshot, contextID, source, stage)
			_ = s.repo.CreateLog(ctx, log)
		}
		return allow
	}

	flagged, highestCategory, highestScore := evaluateModerationScores(result.CategoryScores, cfg.Thresholds)
	action := ContentModerationActionAllow
	blocked := false
	if allowBlock && flagged && cfg.Mode == ContentModerationModePreBlock {
		action = ContentModerationActionBlock
		blocked = true
	}
	if trackPreBlock {
		s.recordPreBlockSyncMetric(latency, action)
	}
	slog.Info("content_moderation.audit_result",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"group_name", input.GroupName,
		"endpoint", input.Endpoint,
		"protocol", input.Protocol,
		"mode", cfg.Mode,
		"allow_block", allowBlock,
		"flagged", flagged,
		"blocked", blocked,
		"action", action,
		"highest_category", highestCategory,
		"highest_score", highestScore,
		"latency_ms", latency,
		"queue_delay_ms", queueDelay)
	if flagged || cfg.RecordNonHits {
		log := s.buildLog(input, cfg, action, flagged, highestCategory, highestScore, result.CategoryScores, content.ExcerptText(), &latency, queueDelay, "", nil, nil)
		s.decorateModerationLog(log, riskSnapshot, contextID, source, stage)
		if queueDelay == nil && cfg.Mode == ContentModerationModePreBlock {
			s.enqueueRecord(input, cfg, log, hashText, flagged, flagged)
		} else {
			s.persistContentModerationLog(ctx, cfg, log, hashText, flagged, flagged)
		}
		allow.LogID = log.ID
	}
	if blocked {
		return s.decorateBlockedDecision(cfg, input, &ContentModerationDecision{
			Allowed:         false,
			Blocked:         true,
			Flagged:         true,
			Message:         cfg.BlockMessage,
			StatusCode:      cfg.BlockStatus,
			HighestCategory: highestCategory,
			HighestScore:    highestScore,
			CategoryScores:  result.CategoryScores,
			Action:          action,
			ContextID:       contextID,
			LogID:           allow.LogID,
			RiskSnapshot:    riskSnapshot,
		})
	}
	return &ContentModerationDecision{
		Allowed:         true,
		Flagged:         flagged,
		Message:         "",
		HighestCategory: highestCategory,
		HighestScore:    highestScore,
		CategoryScores:  result.CategoryScores,
		Action:          action,
		ContextID:       contextID,
		LogID:           allow.LogID,
		RiskSnapshot:    riskSnapshot,
	}
}

func (s *ContentModerationService) checkModelAuditSync(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string, keywordHits []ContentModerationKeywordHit, queueDelay *int, allowBlock bool, contextID *int64, riskSnapshot *ContentModerationRiskSnapshot, source string, stage string) *ContentModerationDecision {
	allow := &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow, KeywordHits: keywordHits, ContextID: contextID, RiskSnapshot: riskSnapshot}
	models := cfg.enabledAuditModels()
	if len(models) == 0 {
		return allow
	}
	startAll := time.Now()
	details := make([]ContentModerationModelAuditDetail, 0, len(models))
	for _, modelCfg := range models {
		prompt := renderContentModerationAuditPrompt(modelCfg.PromptTemplate, content.Text, input, keywordHits)
		start := time.Now()
		raw, result, httpStatus, err := s.callAuditModel(ctx, modelCfg, prompt)
		latencyMS := int(time.Since(start).Milliseconds())
		detail := ContentModerationModelAuditDetail{
			ModelID:     modelCfg.ID,
			Model:       modelCfg.Model,
			Prompt:      trimRunes(redactContentModerationSecrets(prompt), maxModerationExcerptRunes),
			RawResponse: trimRunes(redactContentModerationSecrets(raw), maxModerationExcerptRunes),
			Result:      result,
			LatencyMS:   latencyMS,
		}
		if err != nil {
			detail.Error = contentModerationAuditModelErrorDetail(modelCfg, httpStatus, err)
		}
		s.recordAuditModelCall(modelCfg, latencyMS, httpStatus, result.Violation, err)
		details = append(details, detail)
	}
	agg := aggregateContentModerationModelResults(details, cfg.DecisionRule, models)
	s.recordAuditModelDisagreements(details, agg.Flagged)
	scores := map[string]float64{contentModerationModelAuditCategory: 0}
	if agg.TotalWeight > 0 {
		scores[contentModerationModelAuditCategory] = agg.ViolationWeight / agg.TotalWeight
	} else if agg.TotalCount > 0 {
		scores[contentModerationModelAuditCategory] = float64(agg.ViolationCount) / float64(agg.TotalCount)
	}
	for _, detail := range details {
		if detail.Result.RiskScore > scores[contentModerationModelAuditCategory] {
			scores[contentModerationModelAuditCategory] = detail.Result.RiskScore
		}
	}
	action := ContentModerationActionAllow
	if agg.Flagged {
		action = ContentModerationActionBlock
	}
	if !agg.Flagged && keywordHitRequiresBlocking(keywordHits) && agg.TotalCount == 0 {
		agg.Flagged = true
		agg.Reason = "keyword fallback: all audit models failed"
		action = ContentModerationActionKeywordBlock
		scores[contentModerationKeywordCategory] = 1
		if scores[contentModerationModelAuditCategory] < 1 {
			scores[contentModerationModelAuditCategory] = 1
		}
	}
	highestCategory := contentModerationModelAuditCategory
	if action == ContentModerationActionKeywordBlock {
		highestCategory = contentModerationKeywordCategory
	}
	highestScore := scores[contentModerationModelAuditCategory]
	if highestCategory == contentModerationKeywordCategory {
		highestScore = scores[contentModerationKeywordCategory]
	}
	latency := int(time.Since(startAll).Milliseconds())
	auditCtx := &ContentModerationAuditContext{
		Request:     buildContentModerationRequestContext(input, content.ExcerptText()),
		KeywordHits: cloneContentModerationKeywordHits(keywordHits),
		ModelAudits: details,
		Decision:    &agg,
		FinalAction: action,
	}
	if agg.Flagged || cfg.RecordNonHits || len(keywordHits) > 0 {
		log := s.buildLog(input, cfg, action, agg.Flagged, highestCategory, highestScore, scores, content.ExcerptText(), &latency, queueDelay, "", keywordHits, auditCtx)
		s.decorateModerationLog(log, riskSnapshot, contextID, source, stage)
		if queueDelay == nil && cfg.Mode == ContentModerationModePreBlock {
			s.enqueueRecord(input, cfg, log, hashText, agg.Flagged, agg.Flagged)
		} else {
			s.persistContentModerationLog(ctx, cfg, log, hashText, agg.Flagged, agg.Flagged)
		}
		allow.LogID = log.ID
	}
	if allowBlock && cfg.Mode == ContentModerationModePreBlock && agg.Flagged {
		return s.decorateBlockedDecision(cfg, input, &ContentModerationDecision{
			Allowed:         false,
			Blocked:         true,
			Flagged:         true,
			Message:         cfg.BlockMessage,
			StatusCode:      cfg.BlockStatus,
			HighestCategory: highestCategory,
			HighestScore:    highestScore,
			CategoryScores:  scores,
			Action:          action,
			KeywordHits:     keywordHits,
			ContextID:       contextID,
			LogID:           allow.LogID,
			RiskSnapshot:    riskSnapshot,
		})
	}
	return &ContentModerationDecision{
		Allowed:         true,
		Flagged:         agg.Flagged,
		HighestCategory: contentModerationModelAuditCategory,
		HighestScore:    scores[contentModerationModelAuditCategory],
		CategoryScores:  scores,
		Action:          action,
		KeywordHits:     keywordHits,
		ContextID:       contextID,
		LogID:           allow.LogID,
		RiskSnapshot:    riskSnapshot,
	}
}

func (s *ContentModerationService) recordPreBlockSyncMetric(latencyMS int, action string) {
	if s == nil {
		return
	}
	s.preBlockChecked.Add(1)
	if latencyMS < 0 {
		latencyMS = 0
	}
	s.preBlockLatencyTotalMS.Add(int64(latencyMS))
	switch action {
	case ContentModerationActionBlock, ContentModerationActionHashBlock, ContentModerationActionKeywordBlock:
		s.preBlockBlocked.Add(1)
	case ContentModerationActionError:
		s.preBlockErrors.Add(1)
	default:
		s.preBlockAllowed.Add(1)
	}
}

func (s *ContentModerationService) enqueueAsync(input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string, keywordHits []ContentModerationKeywordHit, forceAudit bool, contextID *int64, riskSnapshot *ContentModerationRiskSnapshot) {
	if s == nil || s.asyncQueue == nil {
		return
	}
	queueSize := defaultContentModerationQueueSize
	if cfg != nil && cfg.QueueSize > 0 {
		queueSize = cfg.QueueSize
	}
	if len(s.asyncQueue) >= queueSize {
		slog.Warn("content_moderation.async_queue_full", "user_id", input.UserID, "endpoint", input.Endpoint, "queue_size", queueSize)
		s.asyncDropped.Add(1)
		return
	}
	task := contentModerationTask{
		input:        input,
		content:      content,
		inputHash:    hashText,
		keywordHits:  cloneContentModerationKeywordHits(keywordHits),
		forceAudit:   forceAudit,
		contextID:    cloneInt64Ptr(contextID),
		riskSnapshot: riskSnapshot,
		enqueuedAt:   time.Now(),
	}
	select {
	case s.asyncQueue <- task:
		s.asyncEnqueued.Add(1)
	default:
		slog.Warn("content_moderation.async_queue_full", "user_id", input.UserID, "endpoint", input.Endpoint)
		s.asyncDropped.Add(1)
	}
}

func (s *ContentModerationService) enqueueRecord(input ContentModerationCheckInput, cfg *ContentModerationConfig, log *ContentModerationLog, inputHash string, recordHash bool, applySideEffects bool) {
	if s == nil || s.asyncQueue == nil || log == nil {
		return
	}
	queueSize := defaultContentModerationQueueSize
	if cfg != nil && cfg.QueueSize > 0 {
		queueSize = cfg.QueueSize
	}
	if len(s.asyncQueue) >= queueSize {
		slog.Warn("content_moderation.record_queue_full",
			"user_id", input.UserID,
			"endpoint", input.Endpoint,
			"action", log.Action,
			"queue_size", queueSize)
		s.asyncDropped.Add(1)
		return
	}
	task := contentModerationTask{
		input:            input,
		inputHash:        inputHash,
		log:              log,
		config:           cloneContentModerationConfig(cfg),
		recordHash:       recordHash,
		applySideEffects: applySideEffects,
		enqueuedAt:       time.Now(),
	}
	select {
	case s.asyncQueue <- task:
		s.asyncEnqueued.Add(1)
	default:
		slog.Warn("content_moderation.record_queue_full",
			"user_id", input.UserID,
			"endpoint", input.Endpoint,
			"action", log.Action)
		s.asyncDropped.Add(1)
	}
}

func (s *ContentModerationService) worker(id int) {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), maxContentModerationTimeoutMS*time.Millisecond+10*time.Second)
		runtimeSnapshot, err := s.loadRuntimeSnapshot(ctx)
		if err != nil || runtimeSnapshot == nil || runtimeSnapshot.config == nil || id >= runtimeSnapshot.config.WorkerCount {
			cancel()
			time.Sleep(time.Second)
			continue
		}
		cfg := runtimeSnapshot.config
		task, ok := s.dequeueAsyncTask(ctx, time.Second)
		if !ok {
			if cfg.BackgroundReviewEnabled && cfg.Enabled && cfg.Mode != ContentModerationModeOff {
				s.processBackgroundReviews(ctx, cfg)
			}
			cancel()
			continue
		}
		func() {
			defer cancel()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("content_moderation.worker_panic", "worker_id", id, "recover", r)
				}
			}()
			if task.log != nil {
				s.asyncActive.Add(1)
				defer s.asyncActive.Add(-1)
				queueDelay := int(time.Since(task.enqueuedAt).Milliseconds())
				task.log.QueueDelayMS = &queueDelay
				taskCfg := task.config
				if taskCfg == nil {
					taskCfg = cfg
				}
				s.persistContentModerationLog(ctx, taskCfg, task.log, task.inputHash, task.recordHash, task.applySideEffects)
				s.asyncProcessed.Add(1)
				return
			}
			if !cfg.Enabled || cfg.Mode == ContentModerationModeOff || (len(cfg.apiKeys()) == 0 && len(cfg.enabledAuditModels()) == 0) {
				return
			}
			if !cfg.includesGroup(task.input.GroupID) {
				return
			}
			if !cfg.includesModel(task.input.Model) {
				return
			}
			s.asyncActive.Add(1)
			defer s.asyncActive.Add(-1)
			queueDelay := int(time.Since(task.enqueuedAt).Milliseconds())
			riskSnapshot := s.riskSnapshotForUser(ctx, cfg, task.input.UserID)
			keywordHits := matchContentModerationKeywordRules(task.content.Text, cfg.keywordRules())
			keywordForceAudit := keywordHitsRequireAction(keywordHits, ContentModerationActionRecordAudit)
			keywordBlockingHit := cfg.KeywordBlockingMode != ContentModerationKeywordModeAPIOnly && keywordHitRequiresBlocking(keywordHits)
			if !keywordForceAudit && !keywordBlockingHit && !contentModerationShouldSample(task.inputHash, riskSnapshot.EffectiveSampleRate) {
				s.asyncProcessed.Add(1)
				return
			}
			if len(cfg.enabledAuditModels()) > 0 {
				_ = s.checkModelAuditSync(ctx, task.input, cfg, task.content, task.inputHash, keywordHits, &queueDelay, false, task.contextID, riskSnapshot, ContentModerationRiskEventSourceAsync, ContentModerationReviewStageAsync)
			} else {
				_ = s.checkSync(ctx, task.input, cfg, task.content, task.inputHash, &queueDelay, false, task.contextID, riskSnapshot, ContentModerationRiskEventSourceAsync, ContentModerationReviewStageAsync)
			}
			s.asyncProcessed.Add(1)
		}()
	}
}

func (s *ContentModerationService) dequeueAsyncTask(ctx context.Context, idleWait time.Duration) (contentModerationTask, bool) {
	var zero contentModerationTask
	if s == nil || s.asyncQueue == nil {
		return zero, false
	}
	if idleWait <= 0 {
		idleWait = time.Second
	}
	timer := time.NewTimer(idleWait)
	defer timer.Stop()
	select {
	case task, ok := <-s.asyncQueue:
		return task, ok
	case <-ctx.Done():
		return zero, false
	case <-timer.C:
		return zero, false
	}
}

func (s *ContentModerationService) ListLogs(ctx context.Context, filter ContentModerationLogFilter) ([]ContentModerationLog, *pagination.PaginationResult, error) {
	if filter.Pagination.Page <= 0 {
		filter.Pagination.Page = 1
	}
	if filter.Pagination.PageSize <= 0 {
		filter.Pagination.PageSize = 20
	}
	if filter.Pagination.PageSize > 100 {
		filter.Pagination.PageSize = 100
	}
	if filter.Pagination.SortOrder == "" {
		filter.Pagination.SortOrder = pagination.SortOrderDesc
	}
	return s.repo.ListLogs(ctx, filter)
}

func (s *ContentModerationService) GetUserBanStatus(ctx context.Context, userID int64) (*ContentModerationBanStatus, error) {
	if userID <= 0 {
		return &ContentModerationBanStatus{UserID: userID}, nil
	}
	cfg, _ := s.loadConfig(ctx)
	selfCfg := ContentModerationSelfUnbanConfig{}
	if cfg != nil {
		selfCfg = cfg.SelfUnban
		selfCfg.normalize()
	}
	status := &ContentModerationBanStatus{
		UserID:               userID,
		SelfUnbanMaxAttempts: selfCfg.MaxAttempts,
	}
	if s == nil || s.repo == nil {
		return status, nil
	}
	now := time.Now()
	ban, err := s.repo.GetActiveUserBan(ctx, userID, now)
	if err != nil {
		return nil, fmt.Errorf("get content moderation user ban: %w", err)
	}
	if ban == nil {
		return status, nil
	}
	status.Banned = true
	status.Reason = ban.Reason
	status.TriggeredAt = &ban.TriggeredAt
	status.BannedUntil = &ban.BannedUntil
	status.RemainingSeconds = int64(time.Until(ban.BannedUntil).Seconds())
	if status.RemainingSeconds < 0 {
		status.RemainingSeconds = 0
	}
	if selfCfg.Enabled {
		windowStart := now.Add(-time.Duration(selfCfg.WindowMinutes) * time.Minute)
		used, err := s.repo.CountSelfUnbanAttempts(ctx, userID, windowStart)
		if err != nil {
			return nil, fmt.Errorf("count content moderation self unban attempts: %w", err)
		}
		status.SelfUnbanAttemptsUsed = used
		reset := windowStart.Add(time.Duration(selfCfg.WindowMinutes) * time.Minute)
		status.SelfUnbanWindowResetAt = &reset
		waitSeconds := int64(0)
		if used == 1 {
			secondAllowedAt := ban.TriggeredAt.Add(time.Duration(selfCfg.SecondAttemptWaitMinutes) * time.Minute)
			if now.Before(secondAllowedAt) {
				waitSeconds = int64(secondAllowedAt.Sub(now).Seconds())
			}
		}
		status.SelfUnbanWaitSeconds = waitSeconds
		status.SelfUnbanAvailable = used < selfCfg.MaxAttempts && waitSeconds == 0
	}
	return status, nil
}

func (s *ContentModerationService) SelfUnbanUser(ctx context.Context, userID int64) (*ContentModerationSelfUnbanResult, error) {
	if s == nil || s.userRepo == nil || s.repo == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_REPOSITORY_UNAVAILABLE", "风控仓储不可用")
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER_ID", "用户 ID 无效")
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	cfg.SelfUnban.normalize()
	if !cfg.SelfUnban.Enabled {
		return nil, infraerrors.Forbidden("SELF_UNBAN_DISABLED", "自助解封未启用")
	}
	now := time.Now()
	ban, err := s.repo.GetActiveUserBan(ctx, userID, now)
	if err != nil {
		return nil, fmt.Errorf("get active content moderation ban: %w", err)
	}
	if ban == nil {
		return &ContentModerationSelfUnbanResult{UserID: userID, Unbanned: true, Status: StatusActive, Message: "当前未处于封禁状态"}, nil
	}
	windowStart := now.Add(-time.Duration(cfg.SelfUnban.WindowMinutes) * time.Minute)
	used, err := s.repo.CountSelfUnbanAttempts(ctx, userID, windowStart)
	if err != nil {
		return nil, fmt.Errorf("count self unban attempts: %w", err)
	}
	reset := windowStart.Add(time.Duration(cfg.SelfUnban.WindowMinutes) * time.Minute)
	result := &ContentModerationSelfUnbanResult{UserID: userID, AttemptsUsed: used, MaxAttempts: cfg.SelfUnban.MaxAttempts, WindowResetAt: &reset}
	if used >= cfg.SelfUnban.MaxAttempts {
		result.Message = "自助解封次数已用完，请等待窗口重置"
		return result, nil
	}
	if used == 1 {
		allowedAt := ban.TriggeredAt.Add(time.Duration(cfg.SelfUnban.SecondAttemptWaitMinutes) * time.Minute)
		if now.Before(allowedAt) {
			result.WaitSeconds = int64(allowedAt.Sub(now).Seconds())
			result.Message = "第二次自助解封需等待冷却时间"
			return result, nil
		}
	}
	attemptNo := used + 1
	if err := s.repo.CreateSelfUnbanRecord(ctx, &ContentModerationSelfUnbanRecord{UserID: userID, BanTriggeredAt: ban.TriggeredAt, AttemptNo: attemptNo, Allowed: true, Reason: "self_unban"}); err != nil {
		return nil, fmt.Errorf("create self unban record: %w", err)
	}
	if _, err := s.UnbanUser(ctx, userID); err != nil {
		return nil, err
	}
	result.Unbanned = true
	result.Status = StatusActive
	result.AttemptsUsed = attemptNo
	result.Message = "解封成功"
	return result, nil
}

func (s *ContentModerationService) UnbanUser(ctx context.Context, userID int64) (*ContentModerationUnbanUserResult, error) {
	if s == nil || s.userRepo == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_USER_REPOSITORY_UNAVAILABLE", "用户仓储不可用")
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER_ID", "用户 ID 无效")
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, infraerrors.NotFound("USER_NOT_FOUND", "用户不存在")
		}
		return nil, fmt.Errorf("get content moderation unban user: %w", err)
	}
	if user.Status != StatusActive {
		user.Status = StatusActive
		if err := s.userRepo.Update(ctx, user); err != nil {
			return nil, fmt.Errorf("update content moderation unban user: %w", err)
		}
	}
	if s.repo != nil {
		if err := s.repo.ClearUserBan(ctx, userID, time.Now()); err != nil {
			return nil, fmt.Errorf("clear content moderation user ban: %w", err)
		}
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.settingRepo != nil {
		if cfg, err := s.loadConfig(ctx); err == nil {
			s.recordUserRiskEvent(ctx, cfg, userID, ContentModerationRiskEventUnban, 0, ContentModerationRiskEventSourceManualAdmin, ContentModerationReviewStageRealtime, "manual unban", nil, nil, nil)
		}
	}
	return &ContentModerationUnbanUserResult{
		UserID: userID,
		Status: StatusActive,
	}, nil
}

func (s *ContentModerationService) GetUserRiskDetail(ctx context.Context, userID int64) (*ContentModerationUserRiskDetail, error) {
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER_ID", "用户 ID 无效")
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := s.riskProfileWithEffectiveWeight(ctx, cfg, userID)
	if err != nil {
		return nil, err
	}
	events := []ContentModerationUserRiskEvent{}
	if s.repo != nil {
		if items, err := s.repo.ListUserRiskEvents(ctx, userID, 100); err == nil {
			events = items
		} else {
			return nil, err
		}
	}
	banStatus, err := s.GetUserBanStatus(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &ContentModerationUserRiskDetail{
		Profile:               profile,
		Events:                events,
		BanStatus:             banStatus,
		EffectiveSampleRate:   contentModerationEffectiveSampleRate(cfg, profile.EffectiveWeight),
		EffectiveBanThreshold: contentModerationEffectiveBanThreshold(cfg, profile.EffectiveWeight),
	}, nil
}

func (s *ContentModerationService) SetUserManualSuspicious(ctx context.Context, userID int64, suspicious bool, reason string) (*ContentModerationUserRiskDetail, error) {
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER_ID", "用户 ID 无效")
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	currentSuspicious := false
	if s != nil && s.repo != nil {
		profile, err := s.repo.GetUserRiskProfile(ctx, userID)
		if err != nil {
			return nil, err
		}
		if profile != nil {
			currentSuspicious = profile.ManualSuspicious
		}
	}
	if currentSuspicious == suspicious {
		return s.GetUserRiskDetail(ctx, userID)
	}
	eventType := ContentModerationRiskEventManualSuspiciousClear
	delta := -cfg.ManualSuspiciousWeight
	if suspicious {
		eventType = ContentModerationRiskEventManualSuspicious
		delta = cfg.ManualSuspiciousWeight
	}
	s.recordUserRiskEvent(ctx, cfg, userID, eventType, delta, ContentModerationRiskEventSourceManualAdmin, ContentModerationReviewStageRealtime, reason, nil, nil, &suspicious)
	return s.GetUserRiskDetail(ctx, userID)
}

func (s *ContentModerationService) ListUserContexts(ctx context.Context, userID int64) ([]ContentModerationContext, error) {
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER_ID", "用户 ID 无效")
	}
	if s == nil || s.repo == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_REPOSITORY_UNAVAILABLE", "内容审计仓储不可用")
	}
	items, err := s.repo.ListUserContexts(ctx, userID, 100)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].EncryptedContext = ""
	}
	return items, nil
}

func (s *ContentModerationService) GetContextDetail(ctx context.Context, contextID int64, adminUserID int64) (*ContentModerationContext, error) {
	if contextID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_CONTEXT_ID", "上下文 ID 无效")
	}
	if s == nil || s.repo == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_REPOSITORY_UNAVAILABLE", "内容审计仓储不可用")
	}
	item, err := s.repo.GetContextByID(ctx, contextID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, infraerrors.NotFound("CONTENT_MODERATION_CONTEXT_NOT_FOUND", "上下文不存在")
	}
	return s.decryptModerationContext(ctx, item, adminUserID)
}

func (s *ContentModerationService) DeleteFlaggedInputHash(ctx context.Context, inputHash string) (*ContentModerationDeleteHashResult, error) {
	inputHash = normalizeContentModerationHash(inputHash)
	if inputHash == "" {
		return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_HASH", "风险输入哈希无效")
	}
	if s == nil || s.hashCache == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_HASH_CACHE_UNAVAILABLE", "内容审计哈希缓存不可用")
	}
	deleted, err := s.hashCache.DeleteFlaggedInputHash(ctx, inputHash)
	if err != nil {
		return nil, fmt.Errorf("delete content moderation flagged hash: %w", err)
	}
	return &ContentModerationDeleteHashResult{
		InputHash: inputHash,
		Deleted:   deleted,
	}, nil
}

func (s *ContentModerationService) ClearFlaggedInputHashes(ctx context.Context) (*ContentModerationClearHashesResult, error) {
	if s == nil || s.hashCache == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_HASH_CACHE_UNAVAILABLE", "内容审计哈希缓存不可用")
	}
	deleted, err := s.hashCache.ClearFlaggedInputHashes(ctx)
	if err != nil {
		return nil, fmt.Errorf("clear content moderation flagged hashes: %w", err)
	}
	return &ContentModerationClearHashesResult{Deleted: deleted}, nil
}

func (s *ContentModerationService) GetStatus(ctx context.Context) (*ContentModerationRuntimeStatus, error) {
	if s == nil {
		return &ContentModerationRuntimeStatus{}, nil
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	riskEnabled := s.isRiskControlEnabled(ctx)
	active := int(s.asyncActive.Load())
	if active < 0 {
		active = 0
	}
	if active > cfg.WorkerCount {
		active = cfg.WorkerCount
	}
	preBlockActive := int(s.preBlockActive.Load())
	if preBlockActive < 0 {
		preBlockActive = 0
	}
	preBlockChecked := s.preBlockChecked.Load()
	preBlockAvgLatency := int64(0)
	if preBlockChecked > 0 {
		preBlockAvgLatency = s.preBlockLatencyTotalMS.Load() / preBlockChecked
	}
	queueLength := 0
	if s.asyncQueue != nil {
		queueLength = len(s.asyncQueue)
	}
	queueUsage := 0.0
	if cfg.QueueSize > 0 {
		queueUsage = float64(queueLength) * 100 / float64(cfg.QueueSize)
	}
	var flaggedHashCount int64
	if s.hashCache != nil {
		if n, err := s.hashCache.CountFlaggedInputHashes(ctx); err == nil {
			flaggedHashCount = n
		} else {
			slog.Warn("content_moderation.hash_count_failed", "error", err)
		}
	}
	var lastCleanupAt *time.Time
	if unix := s.lastCleanupUnix.Load(); unix > 0 {
		t := time.Unix(unix, 0)
		lastCleanupAt = &t
	}
	var contextCounts *ContentModerationContextStatusCounts
	if s.repo != nil {
		if counts, err := s.repo.CountContextsByStatus(ctx); err == nil {
			contextCounts = counts
		} else {
			slog.Warn("content_moderation.context_count_failed", "error", err)
		}
	}
	if contextCounts == nil {
		contextCounts = &ContentModerationContextStatusCounts{}
	}
	var lastBackgroundReviewAt *time.Time
	if unix := s.lastBackgroundReviewUnix.Load(); unix > 0 {
		t := time.Unix(unix, 0)
		lastBackgroundReviewAt = &t
	} else if contextCounts.LastReviewedAt != nil {
		lastBackgroundReviewAt = contextCounts.LastReviewedAt
	}
	contextError, contextErrorAt := s.contextCaptureErrorSnapshot()
	return &ContentModerationRuntimeStatus{
		Enabled:                      cfg.Enabled,
		RiskControlEnabled:           riskEnabled,
		Mode:                         cfg.Mode,
		WorkerCount:                  cfg.WorkerCount,
		MaxWorkers:                   maxContentModerationWorkerCount,
		ActiveWorkers:                active,
		IdleWorkers:                  cfg.WorkerCount - active,
		QueueSize:                    cfg.QueueSize,
		QueueLength:                  queueLength,
		QueueUsagePercent:            queueUsage,
		Enqueued:                     s.asyncEnqueued.Load(),
		Dropped:                      s.asyncDropped.Load(),
		Processed:                    s.asyncProcessed.Load(),
		Errors:                       s.asyncErrors.Load(),
		PreBlockActive:               preBlockActive,
		PreBlockChecked:              preBlockChecked,
		PreBlockAllowed:              s.preBlockAllowed.Load(),
		PreBlockBlocked:              s.preBlockBlocked.Load(),
		PreBlockErrors:               s.preBlockErrors.Load(),
		PreBlockAvgLatencyMS:         preBlockAvgLatency,
		PreBlockAPIKeyActive:         s.preBlockAPIKeyActive(cfg.apiKeys()),
		PreBlockAPIKeyAvailableCount: s.preBlockAPIKeyAvailableCount(cfg.apiKeys()),
		PreBlockAPIKeyTotalCalls:     s.preBlockAPIKeyTotalCalls(cfg.apiKeys()),
		PreBlockAPIKeyLoads:          s.preBlockAPIKeyLoads(cfg.apiKeys()),
		APIKeyStatuses:               s.apiKeyStatuses(cfg.apiKeys()),
		AuditModelStatuses:           s.auditModelStatuses(cfg.enabledAuditModels()),
		FlaggedHashCount:             flaggedHashCount,
		PendingContextCount:          contextCounts.Pending,
		ProcessingContextCount:       contextCounts.Processing,
		FailedContextCount:           contextCounts.Failed,
		LastBackgroundReviewAt:       lastBackgroundReviewAt,
		ContextTotalCount:            contextCounts.Total,
		ContextTotalBytes:            contextCounts.TotalBytes,
		ContextAvgBytes:              contextCounts.AvgBytes,
		ContextDropCount:             s.contextDrops.Load(),
		ContextCaptureError:          contextError,
		LastContextCaptureErrorAt:    contextErrorAt,
		LastCleanupAt:                lastCleanupAt,
		LastCleanupDeletedHit:        s.lastCleanupDeletedHit.Load(),
		LastCleanupDeletedNonHit:     s.lastCleanupDeletedNonHit.Load(),
		LastCleanupDeletedContext:    s.lastCleanupDeletedContext.Load(),
	}, nil
}

func (s *ContentModerationService) cleanupWorker() {
	timer := time.NewTimer(contentModerationCleanupDelay)
	defer timer.Stop()
	for {
		<-timer.C
		s.runCleanupOnce()
		timer.Reset(contentModerationCleanupInterval)
	}
}

func (s *ContentModerationService) runCleanupOnce() {
	if s == nil || s.repo == nil || s.settingRepo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), contentModerationCleanupTimeout)
	defer cancel()
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		slog.Warn("content_moderation.cleanup_load_config_failed", "error", err)
		return
	}
	now := time.Now()
	hitBefore := now.AddDate(0, 0, -cfg.HitRetentionDays)
	nonHitBefore := now.AddDate(0, 0, -cfg.NonHitRetentionDays)
	contextBefore := now.AddDate(0, 0, -cfg.ContextRetentionDays)
	result, err := s.repo.CleanupExpiredLogs(ctx, hitBefore, nonHitBefore, contextBefore)
	if err != nil {
		slog.Warn("content_moderation.cleanup_failed", "error", err)
		return
	}
	if result == nil {
		return
	}
	s.cleanupRequestRiskEvents(ctx, now)
	s.lastCleanupUnix.Store(result.FinishedAt.Unix())
	s.lastCleanupDeletedHit.Store(result.DeletedHit)
	s.lastCleanupDeletedNonHit.Store(result.DeletedNonHit)
	s.lastCleanupDeletedContext.Store(result.DeletedContext)
}

func (s *ContentModerationService) loadConfig(ctx context.Context) (*ContentModerationConfig, error) {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyContentModerationConfig)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return parseContentModerationConfig("")
		}
		return nil, fmt.Errorf("get content moderation config: %w", err)
	}
	return parseContentModerationConfig(raw)
}

func parseContentModerationConfig(raw string) (*ContentModerationConfig, error) {
	cfg := defaultContentModerationConfig()
	if strings.TrimSpace(raw) == "" {
		cfg.normalize()
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), cfg); err != nil {
		return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_CONFIG", "内容审计配置不是有效 JSON")
	}
	cfg.normalize()
	return cfg, nil
}

func (s *ContentModerationService) loadRuntimeSnapshot(ctx context.Context) (*contentModerationRuntimeSnapshot, error) {
	if s == nil || s.settingRepo == nil {
		return nil, errors.New("content moderation setting repository unavailable")
	}
	now := time.Now()
	if snapshot := s.runtimeSnapshot.Load(); snapshot != nil {
		if now.Sub(snapshot.loadedAt) < s.runtimeSnapshotTTL() {
			return snapshot, nil
		}
		s.triggerRuntimeSnapshotRefresh()
		return snapshot, nil
	}

	s.runtimeRefreshMu.Lock()
	defer s.runtimeRefreshMu.Unlock()
	if snapshot := s.runtimeSnapshot.Load(); snapshot != nil {
		return snapshot, nil
	}
	return s.refreshRuntimeSnapshot(ctx)
}

func (s *ContentModerationService) runtimeSnapshotTTL() time.Duration {
	if s != nil && s.runtimeCacheTTL > 0 {
		return s.runtimeCacheTTL
	}
	return contentModerationRuntimeCacheTTL
}

func (s *ContentModerationService) triggerRuntimeSnapshotRefresh() {
	if s == nil || s.runtimeRefreshDeferred() || !s.runtimeRefreshMu.TryLock() {
		return
	}
	if s.runtimeRefreshDeferred() {
		s.runtimeRefreshMu.Unlock()
		return
	}
	go func() {
		defer s.runtimeRefreshMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), contentModerationRuntimeRefreshTimeout)
		defer cancel()
		if _, err := s.refreshRuntimeSnapshot(ctx); err != nil {
			s.runtimeRefreshRetryAt.Store(time.Now().Add(s.runtimeSnapshotTTL()).UnixNano())
			slog.Warn("content_moderation.runtime_snapshot_refresh_failed", "error", err)
		}
	}()
}

func (s *ContentModerationService) runtimeRefreshDeferred() bool {
	if s == nil {
		return false
	}
	return time.Now().UnixNano() < s.runtimeRefreshRetryAt.Load()
}

func (s *ContentModerationService) refreshRuntimeSnapshot(ctx context.Context) (*contentModerationRuntimeSnapshot, error) {
	values, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyRiskControlEnabled,
		SettingKeyContentModerationConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("get content moderation runtime settings: %w", err)
	}
	rawConfig := values[SettingKeyContentModerationConfig]
	configDigest := sha256.Sum256([]byte(rawConfig))
	if current := s.runtimeSnapshot.Load(); current != nil && current.configDigest == configDigest {
		snapshot := &contentModerationRuntimeSnapshot{
			riskControlEnabled: values[SettingKeyRiskControlEnabled] == "true",
			config:             current.config,
			keywordMatcher:     current.keywordMatcher,
			configDigest:       configDigest,
			loadedAt:           time.Now(),
		}
		s.runtimeSnapshot.Store(snapshot)
		s.runtimeRefreshRetryAt.Store(0)
		return snapshot, nil
	}
	cfg, err := parseContentModerationConfig(rawConfig)
	if err != nil {
		return nil, err
	}
	snapshot := &contentModerationRuntimeSnapshot{
		riskControlEnabled: values[SettingKeyRiskControlEnabled] == "true",
		config:             cfg,
		keywordMatcher:     newContentModerationKeywordMatcher(cfg.BlockedKeywords),
		configDigest:       configDigest,
		loadedAt:           time.Now(),
	}
	s.runtimeSnapshot.Store(snapshot)
	s.runtimeRefreshRetryAt.Store(0)
	return snapshot, nil
}

func (s *ContentModerationService) replaceRuntimeConfig(cfg *ContentModerationConfig, raw []byte) {
	if s == nil || cfg == nil {
		return
	}
	s.runtimeRefreshMu.Lock()
	hasSnapshot := s.runtimeSnapshot.Load() != nil
	s.runtimeRefreshMu.Unlock()
	if !hasSnapshot {
		return
	}
	config := cloneContentModerationConfig(cfg)
	keywordMatcher := newContentModerationKeywordMatcher(cfg.BlockedKeywords)
	configDigest := sha256.Sum256(raw)

	s.runtimeRefreshMu.Lock()
	defer s.runtimeRefreshMu.Unlock()
	current := s.runtimeSnapshot.Load()
	if current == nil {
		return
	}
	s.runtimeSnapshot.Store(&contentModerationRuntimeSnapshot{
		riskControlEnabled: current.riskControlEnabled,
		config:             config,
		keywordMatcher:     keywordMatcher,
		configDigest:       configDigest,
		loadedAt:           time.Now(),
	})
}

func (s *ContentModerationService) isRiskControlEnabled(ctx context.Context) bool {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyRiskControlEnabled)
	if err != nil {
		return false
	}
	return raw == "true"
}

func (s *ContentModerationService) validateConfig(ctx context.Context, cfg *ContentModerationConfig) error {
	if cfg == nil {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_CONFIG", "内容审计配置不能为空")
	}
	cfg.normalize()
	switch cfg.Mode {
	case ContentModerationModeOff, ContentModerationModeObserve, ContentModerationModePreBlock:
	default:
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_MODE", "内容审计模式无效")
	}
	if _, err := url.ParseRequestURI(cfg.BaseURL); err != nil {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_BASE_URL", "OpenAI Base URL 无效")
	}
	for _, model := range cfg.AuditModels {
		if !model.Enabled {
			continue
		}
		switch model.Protocol {
		case ContentModerationAuditProtocolOpenAICompatible:
			if strings.TrimSpace(model.BaseURL) == "" || strings.TrimSpace(model.Model) == "" {
				return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_AUDIT_MODEL", "审计模型 base_url 和 model 不能为空")
			}
			if _, err := url.ParseRequestURI(model.BaseURL); err != nil {
				return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_AUDIT_MODEL_BASE_URL", "审计模型 Base URL 无效")
			}
		case ContentModerationAuditProtocolInternalGroup:
			if strings.TrimSpace(model.Model) == "" {
				return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_AUDIT_MODEL", "系统分组审计模型不能为空")
			}
			if model.GroupID == nil || *model.GroupID <= 0 {
				return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_AUDIT_MODEL_GROUP", "系统分组审计模型必须选择分组")
			}
			if s == nil || s.groupRepo == nil {
				return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_AUDIT_MODEL_GROUP", "系统分组审计模型需要分组仓库支持")
			}
			group, err := s.groupRepo.GetByID(ctx, *model.GroupID)
			if err != nil {
				return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_AUDIT_MODEL_GROUP", fmt.Sprintf("系统分组不存在: %d", *model.GroupID))
			}
			if group == nil || !group.IsActive() {
				return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_AUDIT_MODEL_GROUP", fmt.Sprintf("系统分组未启用: %d", *model.GroupID))
			}
		default:
			return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_AUDIT_MODEL_PROTOCOL", "审计模型协议无效")
		}
	}
	if cfg.BlockStatus < 400 || cfg.BlockStatus > 599 {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_BLOCK_STATUS", "拦截 HTTP 状态码必须在 400-599 之间")
	}
	if cfg.ModelFilter.Type != ContentModerationModelFilterAll && len(cfg.ModelFilter.Models) == 0 {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_MODEL_FILTER", "指定或排除模型时至少需要配置 1 个模型")
	}
	if !cfg.AllGroups && len(cfg.GroupIDs) > 0 && s.groupRepo != nil {
		for _, groupID := range cfg.GroupIDs {
			if _, err := s.groupRepo.GetByIDLite(ctx, groupID); err != nil {
				return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_GROUP", fmt.Sprintf("审计分组不存在: %d", groupID))
			}
		}
	}
	if cfg.CyberuseResponse.Enabled && cfg.CyberuseResponse.UserScope.Mode == ContentModerationCyberuseUserScopeInclude && len(cfg.CyberuseResponse.UserScope.UserIDs) == 0 {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_CYBERUSE_SCOPE", "Cyberuse 用户范围为指定用户时至少需要 1 个用户 ID")
	}
	return nil
}

func (s *ContentModerationService) prepareInternalAuditModels(ctx context.Context, cfg *ContentModerationConfig) error {
	if s == nil || cfg == nil {
		return nil
	}
	for i := range cfg.AuditModels {
		model := &cfg.AuditModels[i]
		if !model.Enabled || model.Protocol != ContentModerationAuditProtocolInternalGroup {
			continue
		}
		groupID := derefInt64(model.GroupID)
		group, err := s.groupRepo.GetByID(ctx, groupID)
		if err != nil {
			return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_AUDIT_MODEL_GROUP", fmt.Sprintf("系统分组不存在: %d", groupID))
		}
		if group == nil || !group.IsActive() {
			return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_AUDIT_MODEL_GROUP", fmt.Sprintf("系统分组未启用: %d", groupID))
		}
		apiKey, err := s.ensureInternalAuditAPIKey(ctx, group)
		if err != nil {
			return err
		}
		if apiKey != nil {
			model.InternalAPIKeyID = &apiKey.ID
		}
		model.GroupName = strings.TrimSpace(group.Name)
		model.BaseURL = ""
		model.APIKey = ""
	}
	return nil
}

func (s *ContentModerationService) ensureInternalAuditAPIKey(ctx context.Context, group *Group) (*APIKey, error) {
	if s == nil || s.userRepo == nil || s.apiKeyRepo == nil {
		return nil, infraerrors.BadRequest("CONTENT_MODERATION_INTERNAL_AUDIT_UNAVAILABLE", "系统分组审计模型需要用户和 API Key 仓库支持")
	}
	if group == nil || group.ID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_AUDIT_MODEL_GROUP", "系统分组审计模型必须选择分组")
	}
	user, err := s.ensureInternalAuditUser(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	keyName := internalAuditAPIKeyName(group.ID)
	keys, _, err := s.apiKeyRepo.ListByUserID(ctx, user.ID, pagination.PaginationParams{Page: 1, PageSize: 500}, APIKeyListFilters{})
	if err != nil {
		return nil, fmt.Errorf("list internal audit api keys: %w", err)
	}
	for i := range keys {
		key := keys[i]
		if strings.TrimSpace(key.Name) != keyName {
			continue
		}
		changed := false
		if key.Status != StatusActive {
			key.Status = StatusActive
			changed = true
		}
		if key.GroupID == nil || *key.GroupID != group.ID {
			groupID := group.ID
			key.GroupID = &groupID
			changed = true
		}
		if changed {
			if err := s.apiKeyRepo.Update(ctx, &key); err != nil {
				return nil, fmt.Errorf("update internal audit api key: %w", err)
			}
			if s.authCacheInvalidator != nil {
				s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, key.Key)
			}
		}
		return &key, nil
	}
	key, err := s.generateInternalAuditAPIKey()
	if err != nil {
		return nil, err
	}
	groupID := group.ID
	apiKey := &APIKey{
		UserID:  user.ID,
		Key:     key,
		Name:    keyName,
		GroupID: &groupID,
		Status:  StatusActive,
	}
	if err := s.apiKeyRepo.Create(ctx, apiKey); err != nil {
		return nil, fmt.Errorf("create internal audit api key: %w", err)
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, apiKey.Key)
	}
	return apiKey, nil
}

func (s *ContentModerationService) ensureInternalAuditUser(ctx context.Context, groupID int64) (*User, error) {
	user, err := s.userRepo.GetByEmail(ctx, contentModerationInternalAuditUserEmail)
	if err == nil {
		changed := false
		if user.Status != StatusActive {
			user.Status = StatusActive
			changed = true
		}
		if user.Role == "" {
			user.Role = RoleUser
			changed = true
		}
		if user.Balance < contentModerationInternalAuditBalance {
			user.Balance = contentModerationInternalAuditBalance
			changed = true
		}
		if user.Concurrency <= 0 {
			user.Concurrency = 4
			changed = true
		}
		if strings.TrimSpace(user.Username) == "" {
			user.Username = contentModerationInternalAuditUserName
			changed = true
		}
		if !int64SliceContains(user.AllowedGroups, groupID) {
			user.AllowedGroups = append(user.AllowedGroups, groupID)
			changed = true
		}
		if changed {
			if err := s.userRepo.Update(ctx, user); err != nil {
				return nil, fmt.Errorf("update internal audit user: %w", err)
			}
		}
		return user, nil
	}
	if !errors.Is(err, ErrUserNotFound) {
		return nil, fmt.Errorf("get internal audit user: %w", err)
	}
	password, err := contentModerationRandomHex(32)
	if err != nil {
		return nil, err
	}
	user = &User{
		Email:         contentModerationInternalAuditUserEmail,
		Username:      contentModerationInternalAuditUserName,
		Notes:         contentModerationInternalAuditUserNotes,
		Role:          RoleUser,
		Balance:       contentModerationInternalAuditBalance,
		Concurrency:   4,
		Status:        StatusActive,
		AllowedGroups: []int64{groupID},
		SignupSource:  "email",
	}
	if err := user.SetPassword(password); err != nil {
		return nil, fmt.Errorf("set internal audit user password: %w", err)
	}
	if err := s.userRepo.Create(ctx, user); err != nil {
		if errors.Is(err, ErrEmailExists) {
			return s.ensureInternalAuditUser(ctx, groupID)
		}
		return nil, fmt.Errorf("create internal audit user: %w", err)
	}
	return user, nil
}

func (s *ContentModerationService) generateInternalAuditAPIKey() (string, error) {
	token, err := contentModerationRandomHex(32)
	if err != nil {
		return "", err
	}
	prefix := "sk-"
	if s != nil && s.cfg != nil && strings.TrimSpace(s.cfg.Default.APIKeyPrefix) != "" {
		prefix = strings.TrimSpace(s.cfg.Default.APIKeyPrefix)
	}
	return prefix + "rca-" + token, nil
}

func contentModerationRandomHex(size int) (string, error) {
	if size <= 0 {
		size = 32
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random secret: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func internalAuditAPIKeyName(groupID int64) string {
	return fmt.Sprintf("%s:%d", contentModerationInternalAuditKeyPrefix, groupID)
}

func int64SliceContains(values []int64, target int64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func (s *ContentModerationService) callModeration(ctx context.Context, cfg *ContentModerationConfig, input any, trackKeyLoad ...bool) (*moderationAPIResult, error) {
	attempts := cfg.RetryCount + 1
	if attempts <= 0 {
		attempts = 1
	}
	if attempts > maxContentModerationRetryCount+1 {
		attempts = maxContentModerationRetryCount + 1
	}
	trackLoad := len(trackKeyLoad) > 0 && trackKeyLoad[0]
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		key, ok := s.nextUsableAPIKey(cfg)
		if !ok {
			lastErr = errors.New("no moderation api key available")
			break
		}
		if trackLoad {
			s.beginModerationAPIKeyCall(key)
		}
		start := time.Now()
		httpStatus := 0
		result, err := s.callModerationOnceWithInput(ctx, cfg, key, input, &httpStatus)
		latency := int(time.Since(start).Milliseconds())
		if err == nil {
			if trackLoad {
				s.finishModerationAPIKeyCall(key, latency, true)
			}
			s.markAPIKeySuccess(key, latency, httpStatus)
			return result, nil
		}
		if trackLoad {
			s.finishModerationAPIKeyCall(key, latency, false)
		}
		s.markAPIKeyError(key, err.Error(), latency, httpStatus)
		lastErr = err
		if httpStatus == http.StatusBadRequest {
			break
		}
		if attempt == attempts-1 {
			break
		}
		wait := time.Duration(100*(attempt+1)) * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, lastErr
}

func (s *ContentModerationService) callAuditModel(ctx context.Context, cfg ContentModerationAuditModelConfig, prompt string) (string, ContentModerationModelResult, int, error) {
	if cfg.Protocol == ContentModerationAuditProtocolInternalGroup {
		return s.callInternalGroupAuditModel(ctx, cfg, prompt)
	}
	return s.callOpenAICompatibleAuditModel(ctx, cfg, prompt)
}

func (s *ContentModerationService) callInternalGroupAuditModel(ctx context.Context, cfg ContentModerationAuditModelConfig, prompt string) (string, ContentModerationModelResult, int, error) {
	apiKey, err := s.internalAuditAPIKeyForModel(ctx, cfg)
	if err != nil {
		return "", ContentModerationModelResult{}, 0, err
	}
	endpoint, err := s.internalAuditChatCompletionsEndpoint()
	if err != nil {
		return "", ContentModerationModelResult{}, 0, err
	}
	return s.callOpenAIChatCompletionAuditModel(ctx, endpoint, apiKey.Key, cfg, prompt)
}

func (s *ContentModerationService) internalAuditAPIKeyForModel(ctx context.Context, cfg ContentModerationAuditModelConfig) (*APIKey, error) {
	if s == nil || s.apiKeyRepo == nil || s.groupRepo == nil {
		return nil, errors.New("internal audit model dependencies unavailable")
	}
	if cfg.InternalAPIKeyID != nil && *cfg.InternalAPIKeyID > 0 {
		key, err := s.apiKeyRepo.GetByID(ctx, *cfg.InternalAPIKeyID)
		if err == nil && IsContentModerationInternalAuditAPIKey(key) && key.GroupID != nil && cfg.GroupID != nil && *key.GroupID == *cfg.GroupID {
			return key, nil
		}
	}
	groupID := derefInt64(cfg.GroupID)
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("get internal audit group: %w", err)
	}
	return s.ensureInternalAuditAPIKey(ctx, group)
}

func (s *ContentModerationService) internalAuditChatCompletionsEndpoint() (string, error) {
	base := "http://127.0.0.1:8080"
	if s != nil && s.cfg != nil {
		host := strings.TrimSpace(s.cfg.Server.Host)
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		port := s.cfg.Server.Port
		if port <= 0 {
			port = 8080
		}
		base = fmt.Sprintf("http://%s:%d", host, port)
	}
	return url.JoinPath(strings.TrimRight(base, "/"), "/v1/chat/completions")
}

func (s *ContentModerationService) callOpenAICompatibleAuditModel(ctx context.Context, cfg ContentModerationAuditModelConfig, prompt string) (string, ContentModerationModelResult, int, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	endpoint, err := url.JoinPath(base, "/v1/chat/completions")
	if err != nil {
		return "", ContentModerationModelResult{}, 0, err
	}
	return s.callOpenAIChatCompletionAuditModel(ctx, endpoint, cfg.APIKey, cfg, prompt)
}

func (s *ContentModerationService) callOpenAIChatCompletionAuditModel(ctx context.Context, endpoint string, apiKey string, cfg ContentModerationAuditModelConfig, prompt string) (string, ContentModerationModelResult, int, error) {
	payload := openAIChatCompletionRequest{
		Model:       cfg.Model,
		Messages:    []openAIChatMessage{{Role: "user", Content: prompt}},
		Temperature: cfg.Temperature,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", ContentModerationModelResult{}, 0, err
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = defaultContentModerationTimeoutMS * time.Millisecond
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", ContentModerationModelResult{}, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Content-Type", "application/json")
	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", ContentModerationModelResult{}, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := contentModerationAuditModelErrorMessage(resp.StatusCode, string(body))
		return string(body), ContentModerationModelResult{}, resp.StatusCode, fmt.Errorf("audit model status %d: %s", resp.StatusCode, message)
	}
	var out openAIChatCompletionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return string(body), parseContentModerationModelTextResult(string(body)), resp.StatusCode, nil
	}
	content := ""
	if len(out.Choices) > 0 {
		content = out.Choices[0].Message.Content
	}
	if strings.TrimSpace(content) == "" {
		content = string(body)
	}
	return content, parseContentModerationModelTextResult(content), resp.StatusCode, nil
}

func contentModerationAuditModelErrorMessage(status int, body string) string {
	trimmed := strings.TrimSpace(body)
	lower := strings.ToLower(trimmed)
	if status == http.StatusForbidden && contentModerationAuditModelIsInternalAccessBlocked(body) {
		return "系统分组审计模型被订阅/分组权限拦截：内部审计账号应跳过订阅计费限制，请检查认证中间件配置"
	}
	if status == http.StatusUnauthorized && strings.Contains(lower, "invalid_api_key") {
		return "系统分组审计模型内部 API Key 无效，请保存风控配置后重建内部审计 Key"
	}
	if strings.Contains(lower, "model_not_found") || strings.Contains(lower, "no available channel for model") {
		return "所选分组没有可用该模型，请更换模型或补充渠道"
	}
	if status >= 500 {
		return "审核模型上游请求失败，请检查分组渠道和账号健康"
	}
	return trimmed
}

func contentModerationAuditModelIsInternalAccessBlocked(body string) bool {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return false
	}
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		switch strings.ToUpper(strings.TrimSpace(payload.Code)) {
		case "SUBSCRIPTION_NOT_FOUND", "SUBSCRIPTION_INVALID", "GROUP_NOT_ALLOWED", "GROUP_DISABLED", "GROUP_DELETED":
			return true
		}
	}
	lower := strings.ToLower(trimmed)
	return strings.Contains(lower, "no active subscription found for this group") ||
		strings.Contains(lower, "subscription_not_found") ||
		strings.Contains(lower, "group_not_allowed") ||
		strings.Contains(lower, "group_disabled") ||
		strings.Contains(lower, "group_deleted")
}

func contentModerationAuditModelErrorDetail(model ContentModerationAuditModelConfig, httpStatus int, err error) string {
	if err == nil {
		return ""
	}
	groupLabel := "未配置"
	if model.GroupID != nil && *model.GroupID > 0 {
		groupLabel = fmt.Sprintf("%d", *model.GroupID)
		if strings.TrimSpace(model.GroupName) != "" {
			groupLabel = fmt.Sprintf("%s(%d)", strings.TrimSpace(model.GroupName), *model.GroupID)
		}
	}
	prefix := fmt.Sprintf("审计模型 %q 调用失败", strings.TrimSpace(model.Model))
	if model.Protocol == ContentModerationAuditProtocolInternalGroup {
		prefix = fmt.Sprintf("系统分组审计模型 %q 调用失败，分组：%s", strings.TrimSpace(model.Model), groupLabel)
	}
	if errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "context canceled") {
		return prefix + "；原因：请求上下文已取消，通常是客户端断开、上游超时或流式请求提前结束"
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "deadline exceeded") {
		return prefix + "；原因：请求超时，请检查审计模型超时配置、分组渠道延迟和账号健康"
	}
	if httpStatus > 0 {
		return fmt.Sprintf("%s；HTTP %d；%s", prefix, httpStatus, err.Error())
	}
	return prefix + "；" + err.Error()
}

func (s *ContentModerationService) callModerationOnceWithInput(ctx context.Context, cfg *ContentModerationConfig, apiKey string, input any, httpStatus *int) (*moderationAPIResult, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	endpoint, err := url.JoinPath(base, "/v1/moderations")
	if err != nil {
		return nil, err
	}
	payload := moderationAPIRequest{
		Model: cfg.Model,
		Input: input,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if httpStatus != nil {
		*httpStatus = resp.StatusCode
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("moderation api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out moderationAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Results) == 0 {
		return nil, errors.New("moderation api returned empty results")
	}
	return &out.Results[0], nil
}

func (s *ContentModerationService) buildLog(input ContentModerationCheckInput, cfg *ContentModerationConfig, action string, flagged bool, highestCategory string, highestScore float64, scores map[string]float64, text string, latency *int, queueDelay *int, errText string, extras ...any) *ContentModerationLog {
	var keywordHits []ContentModerationKeywordHit
	var auditCtx *ContentModerationAuditContext
	if len(extras) > 0 {
		if v, ok := extras[0].([]ContentModerationKeywordHit); ok {
			keywordHits = v
		}
	}
	if len(extras) > 1 {
		if v, ok := extras[1].(*ContentModerationAuditContext); ok {
			auditCtx = v
		}
	}
	var userID *int64
	if input.UserID > 0 {
		userID = &input.UserID
	}
	var apiKeyID *int64
	if input.APIKeyID > 0 {
		apiKeyID = &input.APIKeyID
	}
	return &ContentModerationLog{
		RequestID:         input.RequestID,
		UserID:            userID,
		UserEmail:         input.UserEmail,
		APIKeyID:          apiKeyID,
		APIKeyName:        input.APIKeyName,
		GroupID:           cloneInt64Ptr(input.GroupID),
		GroupName:         input.GroupName,
		Endpoint:          input.Endpoint,
		Provider:          input.Provider,
		Model:             input.Model,
		Protocol:          input.Protocol,
		Mode:              cfg.Mode,
		Action:            action,
		Flagged:           flagged,
		HighestCategory:   highestCategory,
		HighestScore:      highestScore,
		CategoryScores:    cloneFloatMap(scores),
		ThresholdSnapshot: cloneFloatMap(cfg.Thresholds),
		InputExcerpt:      trimRunes(redactContentModerationSecrets(text), maxModerationExcerptRunes),
		KeywordHits:       cloneContentModerationKeywordHits(keywordHits),
		AuditContext:      auditCtx,
		UpstreamLatencyMS: latency,
		QueueDelayMS:      queueDelay,
		Error:             errText,
	}
}

func (s *ContentModerationService) decorateBlockedDecision(cfg *ContentModerationConfig, input ContentModerationCheckInput, decision *ContentModerationDecision) *ContentModerationDecision {
	if decision == nil || cfg == nil || !cfg.cyberuseResponseApplies(input.UserID) || !cfg.CyberuseResponse.EmitToClient {
		return decision
	}
	decision.ErrorCode = cfg.CyberuseResponse.ErrorCode
	message := strings.TrimSpace(cfg.CyberuseResponse.Message)
	if message == "" {
		message = defaultContentModerationCyberuseMessage
	}
	if cfg.CyberuseResponse.IncludeRequestID && strings.TrimSpace(input.RequestID) != "" {
		message = fmt.Sprintf("%s (request_id: %s)", message, strings.TrimSpace(input.RequestID))
	}
	decision.Message = message
	if strings.EqualFold(strings.TrimSpace(decision.HighestCategory), contentModerationKeywordCategory) {
		decision.HighestCategory = contentModerationClientDelayedTriggerCategory
	}
	return decision
}

func (s *ContentModerationService) decorateCyberuseAuditMetadata(cfg *ContentModerationConfig, log *ContentModerationLog, inputHash string) {
	if cfg == nil || log == nil || !log.Flagged || !cfg.cyberuseResponseApplies(contentModerationEmailUserID(log)) || !cfg.CyberuseResponse.AuditMetadataEnabled {
		return
	}
	if log.AuditContext == nil {
		log.AuditContext = &ContentModerationAuditContext{
			Request:     contentModerationRequestContextFromLog(log),
			KeywordHits: cloneContentModerationKeywordHits(log.KeywordHits),
			FinalAction: log.Action,
		}
	} else {
		log.AuditContext.Request = contentModerationRequestContextFromLog(log)
		if len(log.AuditContext.KeywordHits) == 0 {
			log.AuditContext.KeywordHits = cloneContentModerationKeywordHits(log.KeywordHits)
		}
		if strings.TrimSpace(log.AuditContext.FinalAction) == "" {
			log.AuditContext.FinalAction = log.Action
		}
	}
	log.AuditContext.Metadata = &ContentModerationPolicyMetadata{
		Source:              "sub2api",
		Origin:              "local_content_moderation",
		PolicySignal:        "cyberuse",
		UpstreamPolicy:      false,
		RequestID:           log.RequestID,
		UserID:              contentModerationEmailUserID(log),
		UserEmail:           log.UserEmail,
		APIKeyID:            contentModerationLogAPIKeyID(log),
		APIKeyName:          log.APIKeyName,
		GroupID:             cloneInt64Ptr(log.GroupID),
		GroupName:           log.GroupName,
		Endpoint:            log.Endpoint,
		Provider:            log.Provider,
		Model:               log.Model,
		Protocol:            log.Protocol,
		InputHash:           strings.TrimSpace(inputHash),
		ContextID:           cloneInt64Ptr(log.ContextID),
		Action:              log.Action,
		HighestCategory:     contentModerationClientVisibleCategory(log.HighestCategory),
		ClientErrorCode:     cfg.CyberuseResponse.ErrorCode,
		ClientMessage:       cfg.CyberuseResponse.Message,
		AnnouncementEnabled: cfg.CyberuseResponse.AnnouncementEnabled,
		AnnouncementTitle:   cfg.CyberuseResponse.AnnouncementTitle,
		AnnouncementContent: cfg.CyberuseResponse.AnnouncementContent,
	}
}

func contentModerationClientVisibleCategory(category string) string {
	if strings.EqualFold(strings.TrimSpace(category), contentModerationKeywordCategory) {
		return contentModerationClientDelayedTriggerCategory
	}
	return category
}

func IsContentModerationInternalAuditAPIKey(apiKey *APIKey) bool {
	if apiKey == nil {
		return false
	}
	if strings.HasPrefix(strings.TrimSpace(apiKey.Name), contentModerationInternalAuditKeyPrefix+":") {
		return true
	}
	if apiKey.User != nil && strings.EqualFold(strings.TrimSpace(apiKey.User.Email), contentModerationInternalAuditUserEmail) {
		return true
	}
	return false
}

func contentModerationRequestContextFromLog(log *ContentModerationLog) ContentModerationRequestContext {
	if log == nil {
		return ContentModerationRequestContext{}
	}
	return ContentModerationRequestContext{
		RequestID:  log.RequestID,
		UserID:     contentModerationEmailUserID(log),
		UserEmail:  log.UserEmail,
		APIKeyID:   contentModerationLogAPIKeyID(log),
		APIKeyName: log.APIKeyName,
		GroupID:    cloneInt64Ptr(log.GroupID),
		GroupName:  log.GroupName,
		Endpoint:   log.Endpoint,
		Provider:   log.Provider,
		Model:      log.Model,
		Protocol:   log.Protocol,
		Input:      log.InputExcerpt,
	}
}

func contentModerationLogAPIKeyID(log *ContentModerationLog) int64 {
	if log == nil || log.APIKeyID == nil {
		return 0
	}
	return *log.APIKeyID
}

func (s *ContentModerationService) persistContentModerationLog(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog, hashText string, recordHash bool, applySideEffects bool) {
	if s == nil || log == nil {
		return
	}
	if recordHash && s.hashCache != nil {
		if err := s.hashCache.RecordFlaggedInputHash(ctx, hashText); err != nil {
			slog.Warn("content_moderation.record_hash_failed", "user_id", contentModerationEmailUserID(log), "endpoint", log.Endpoint, "error", err)
		}
	}
	autoBanJustApplied := false
	if applySideEffects {
		autoBanJustApplied = s.applyFlaggedAccountSideEffects(ctx, cfg, log)
		s.sendFlaggedNotificationSideEffects(ctx, cfg, log, autoBanJustApplied)
	}
	s.decorateCyberuseAuditMetadata(cfg, log, hashText)
	if s.repo != nil {
		if err := s.repo.CreateLog(ctx, log); err != nil {
			slog.Warn("content_moderation.create_log_failed", "user_id", contentModerationEmailUserID(log), "endpoint", log.Endpoint, "action", log.Action, "error", err)
			return
		}
	}
	if applySideEffects && cfg != nil && log.Flagged && log.UserID != nil && *log.UserID > 0 {
		logID := log.ID
		s.recordUserRiskEvent(ctx, cfg, *log.UserID, ContentModerationRiskEventFlagged, cfg.FlaggedWeight, log.RiskEventSource, log.ReviewStage, log.HighestCategory, &logID, log.ContextID, nil)
		if log.AutoBanned {
			s.recordUserRiskEvent(ctx, cfg, *log.UserID, ContentModerationRiskEventBan, cfg.BanWeight, log.RiskEventSource, log.ReviewStage, log.HighestCategory, &logID, log.ContextID, nil)
		}
	}
}

func (s *ContentModerationService) applyFlaggedAccountSideEffects(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) bool {
	if s == nil || cfg == nil || log == nil || !log.Flagged || log.UserID == nil || *log.UserID <= 0 {
		return false
	}
	count := 1
	if s.repo != nil && cfg.ViolationWindowHours > 0 {
		since := time.Now().Add(-time.Duration(cfg.ViolationWindowHours) * time.Hour)
		if n, err := s.repo.CountFlaggedByUserSince(ctx, *log.UserID, since, cfg.CyberPolicyExcludeFromBanCount); err == nil {
			count = n + 1
		}
	}
	log.ViolationCount = count
	banThreshold := cfg.BanThreshold
	if log.EffectiveBanThreshold > 0 {
		banThreshold = log.EffectiveBanThreshold
	}
	if !cfg.AutoBanEnabled || banThreshold <= 0 || count < banThreshold {
		return false
	}
	if s.userRepo != nil {
		user, err := s.userRepo.GetByID(ctx, *log.UserID)
		if err != nil {
			slog.Warn("content_moderation.ban_get_user_failed", "user_id", *log.UserID, "error", err)
			return false
		}
		if user.IsAdmin() {
			slog.Warn("content_moderation.autoban_skipped_admin", "user_id", *log.UserID, "role", user.Role, "count", count, "threshold", banThreshold)
			return false
		}
	}
	now := time.Now()
	banUntil := now.Add(time.Duration(cfg.BanDurationMinutes) * time.Minute)
	if s.repo != nil {
		if err := s.repo.UpsertUserBan(ctx, &ContentModerationUserBan{UserID: *log.UserID, Reason: log.HighestCategory, TriggeredAt: now, BannedUntil: banUntil}); err != nil {
			slog.Warn("content_moderation.ban_record_failed", "user_id", *log.UserID, "error", err)
			return false
		}
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, *log.UserID)
	}
	log.AutoBanned = true
	return true
}

func (s *ContentModerationService) sendFlaggedNotificationSideEffects(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog, autoBanJustApplied bool) {
	if s == nil || cfg == nil || log == nil || !log.Flagged {
		return
	}
	userID := contentModerationEmailUserID(log)
	emailSent := false
	if s.emailService != nil && strings.TrimSpace(log.UserEmail) != "" {
		if autoBanJustApplied {
			if err := s.sendAccountDisabledEmail(ctx, cfg, log); err != nil {
				slog.Warn("content_moderation.ban_email_failed", "user_id", userID, "email", log.UserEmail, "error", err)
			} else {
				emailSent = true
			}
		}
	}
	if cfg.EmailOnHit {
		s.sendContentModerationViolationFeishu(ctx, cfg, log)
	}
	if autoBanJustApplied {
		s.sendContentModerationBanFeishu(ctx, cfg, log)
	}
	log.EmailSent = emailSent
}

func (s *ContentModerationService) sendContentModerationViolationFeishu(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) {
	if s == nil || s.feishuNotificationService == nil || cfg == nil || log == nil {
		return
	}
	userID := contentModerationEmailUserID(log)
	if userID <= 0 {
		return
	}
	input := s.contentModerationFeishuInput(cfg, log)
	if err := s.feishuNotificationService.SendContentModerationViolation(ctx, FeishuContentModerationViolationNotification(input)); err != nil {
		slog.Warn("content_moderation.violation_feishu_failed", "user_id", userID, "error", err)
	}
}

func (s *ContentModerationService) sendContentModerationBanFeishu(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) {
	if s == nil || s.feishuNotificationService == nil || cfg == nil || log == nil {
		return
	}
	userID := contentModerationEmailUserID(log)
	if userID <= 0 {
		return
	}
	input := s.contentModerationFeishuInput(cfg, log)
	if err := s.feishuNotificationService.SendContentModerationBan(ctx, FeishuContentModerationBanNotification{
		UserID:         input.UserID,
		UserName:       input.UserName,
		UserEmail:      input.UserEmail,
		GroupName:      input.GroupName,
		Category:       input.Category,
		Score:          input.Score,
		ViolationCount: input.ViolationCount,
		BanThreshold:   input.BanThreshold,
		BanDurationMin: cfg.BanDurationMinutes,
	}); err != nil {
		slog.Warn("content_moderation.ban_feishu_failed", "user_id", userID, "error", err)
	}
}

func (s *ContentModerationService) contentModerationFeishuInput(cfg *ContentModerationConfig, log *ContentModerationLog) FeishuContentModerationViolationNotification {
	banThreshold := cfg.BanThreshold
	if log.EffectiveBanThreshold > 0 {
		banThreshold = log.EffectiveBanThreshold
	}
	return FeishuContentModerationViolationNotification{
		UserID:         contentModerationEmailUserID(log),
		UserEmail:      log.UserEmail,
		GroupName:      log.GroupName,
		Category:       contentModerationClientVisibleCategory(log.HighestCategory),
		Score:          log.HighestScore,
		ViolationCount: log.ViolationCount,
		BanThreshold:   banThreshold,
	}
}

func (s *ContentModerationService) sendAccountDisabledEmail(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) error {
	siteName := s.siteName(ctx)
	if s.emailService.notificationEmailService != nil {
		if err := s.emailService.notificationEmailService.Send(ctx, NotificationEmailSendInput{
			Event:          NotificationEmailEventContentModerationDisabled,
			RecipientEmail: log.UserEmail,
			RecipientName:  emailRecipientName(log.UserEmail),
			UserID:         contentModerationEmailUserID(log),
			SourceType:     "content_moderation",
			SourceID:       contentModerationEmailSourceID(log),
			Variables:      contentModerationEmailVariables(log, cfg),
		}); err == nil {
			return nil
		} else {
			if !shouldFallbackNotificationEmail(err) {
				return err
			}
			slog.Warn("template content moderation disabled email failed; falling back to built-in body", "log_id", log.ID, "recipient_hash", notificationEmailHash(log.UserEmail), "err", err.Error())
		}
	}
	subject := fmt.Sprintf("[%s] 账户已被禁用 / Account Disabled", sanitizeEmailHeader(siteName))
	body := buildContentModerationAccountDisabledEmailBody(siteName, log, cfg)
	return s.emailService.SendEmail(ctx, log.UserEmail, subject, body)
}

func contentModerationEmailUserID(log *ContentModerationLog) int64 {
	if log == nil || log.UserID == nil {
		return 0
	}
	return *log.UserID
}

func contentModerationEmailSourceID(log *ContentModerationLog) string {
	if log == nil || log.ID <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", log.ID)
}

func contentModerationEmailVariables(log *ContentModerationLog, cfg *ContentModerationConfig) map[string]string {
	variables := map[string]string{
		"triggered_at":        time.Now().UTC().Format(time.RFC3339),
		"group_name":          "-",
		"moderation_category": "-",
		"moderation_score":    "0.000",
		"violation_count":     "0",
		"ban_threshold":       "0",
	}
	if log != nil {
		if !log.CreatedAt.IsZero() {
			variables["triggered_at"] = log.CreatedAt.UTC().Format(time.RFC3339)
		}
		if strings.TrimSpace(log.GroupName) != "" {
			variables["group_name"] = strings.TrimSpace(log.GroupName)
		}
		if strings.TrimSpace(log.HighestCategory) != "" {
			variables["moderation_category"] = strings.TrimSpace(log.HighestCategory)
		}
		variables["moderation_score"] = fmt.Sprintf("%.3f", log.HighestScore)
		variables["violation_count"] = fmt.Sprintf("%d", log.ViolationCount)
	}
	if cfg != nil {
		variables["ban_threshold"] = fmt.Sprintf("%d", cfg.BanThreshold)
	}
	return variables
}

func (s *ContentModerationService) siteName(ctx context.Context) string {
	if s == nil || s.settingRepo == nil {
		return "Sub2API"
	}
	name, err := s.settingRepo.GetValue(ctx, SettingKeySiteName)
	if err != nil || strings.TrimSpace(name) == "" {
		return "Sub2API"
	}
	return strings.TrimSpace(name)
}

func defaultContentModerationConfig() *ContentModerationConfig {
	return &ContentModerationConfig{
		Enabled:                             false,
		Mode:                                ContentModerationModePreBlock,
		BaseURL:                             defaultContentModerationBaseURL,
		Model:                               defaultContentModerationModel,
		TimeoutMS:                           defaultContentModerationTimeoutMS,
		SampleRate:                          100,
		AllGroups:                           true,
		GroupIDs:                            []int64{},
		RecordNonHits:                       false,
		Thresholds:                          ContentModerationDefaultThresholds(),
		WorkerCount:                         defaultContentModerationWorkerCount,
		QueueSize:                           defaultContentModerationQueueSize,
		BlockStatus:                         defaultContentModerationBlockHTTPStatus,
		BlockMessage:                        defaultContentModerationBlockMessage,
		EmailOnHit:                          true,
		AutoBanEnabled:                      true,
		BanThreshold:                        defaultContentModerationBanThreshold,
		BanDurationMinutes:                  defaultContentModerationBanDurationMinutes,
		ViolationWindowHours:                defaultContentModerationViolationWindowHours,
		RetryCount:                          defaultContentModerationRetryCount,
		HitRetentionDays:                    defaultContentModerationHitRetentionDays,
		NonHitRetentionDays:                 defaultContentModerationNonHitRetentionDays,
		ContextRetentionDays:                defaultContentModerationContextRetentionDays,
		PreHashCheckEnabled:                 false,
		BlockedKeywords:                     []string{},
		KeywordBlockingMode:                 ContentModerationKeywordModeKeywordAndAPI,
		KeywordRules:                        []ContentModerationKeywordRule{},
		AuditModels:                         []ContentModerationAuditModelConfig{},
		DecisionRule:                        ContentModerationDecisionRule{Type: ContentModerationDecisionRuleAny},
		SelfUnban:                           ContentModerationSelfUnbanConfig{Enabled: true, WindowMinutes: defaultContentModerationUnbanWindowMinutes, MaxAttempts: 2, SecondAttemptWaitMinutes: defaultContentModerationSecondUnbanWaitMins},
		RiskWeightEnabled:                   true,
		FlaggedWeight:                       defaultContentModerationFlaggedWeight,
		BanWeight:                           defaultContentModerationBanWeight,
		ManualSuspiciousWeight:              defaultContentModerationManualWeight,
		DecayHalfLifeDays:                   defaultContentModerationDecayHalfLifeDays,
		MaxSampleRate:                       defaultContentModerationMaxSampleRate,
		BanThresholdWeightStep:              defaultContentModerationBanThresholdStep,
		MinEffectiveBanThreshold:            defaultContentModerationMinBanThreshold,
		BackgroundReviewEnabled:             true,
		BackgroundReviewBatchSize:           defaultContentModerationReviewBatchSize,
		BackgroundReviewMaxAttempts:         defaultContentModerationReviewMaxAttempts,
		BackgroundReviewRetryBackoffSeconds: defaultContentModerationReviewBackoffSeconds,
		ContextCaptureEnabled:               true,
		ContextMaxBytes:                     defaultContentModerationContextMaxBytes,
		CyberuseResponse:                    defaultContentModerationCyberuseConfig(),
		ModelFilter: ContentModerationModelFilter{
			Type:   ContentModerationModelFilterAll,
			Models: []string{},
		},
		CyberPolicyExcludeFromBanCount: false,
	}
}

func defaultContentModerationCyberuseConfig() ContentModerationCyberuseConfig {
	return ContentModerationCyberuseConfig{
		Enabled:              false,
		EmitToClient:         true,
		ErrorCode:            defaultContentModerationCyberuseErrorCode,
		Message:              defaultContentModerationCyberuseMessage,
		IncludeRequestID:     true,
		AuditMetadataEnabled: true,
		AnnouncementEnabled:  false,
		AnnouncementTitle:    "",
		AnnouncementContent:  "",
		UserScope: ContentModerationCyberuseUserScope{
			Mode:    ContentModerationCyberuseUserScopeAll,
			UserIDs: []int64{},
		},
	}
}

func cloneContentModerationConfig(cfg *ContentModerationConfig) *ContentModerationConfig {
	if cfg == nil {
		return nil
	}
	clone := *cfg
	clone.APIKeys = append([]string(nil), cfg.APIKeys...)
	clone.GroupIDs = append([]int64(nil), cfg.GroupIDs...)
	clone.BlockedKeywords = append([]string(nil), cfg.BlockedKeywords...)
	clone.KeywordRules = append([]ContentModerationKeywordRule(nil), cfg.KeywordRules...)
	clone.AuditModels = append([]ContentModerationAuditModelConfig(nil), cfg.AuditModels...)
	clone.Thresholds = cloneFloatMap(cfg.Thresholds)
	clone.CyberuseResponse = cloneContentModerationCyberuseConfig(cfg.CyberuseResponse)
	clone.ModelFilter = ContentModerationModelFilter{
		Type:   cfg.ModelFilter.Type,
		Models: append([]string(nil), cfg.ModelFilter.Models...),
	}
	return &clone
}

func (cfg *ContentModerationConfig) normalize() {
	if cfg.APIKey != "" {
		cfg.APIKeys = normalizeModerationAPIKeys(append(cfg.APIKeys, cfg.APIKey))
		cfg.APIKey = ""
	} else {
		cfg.APIKeys = normalizeModerationAPIKeys(cfg.APIKeys)
	}
	if cfg.Mode == "" {
		cfg.Mode = ContentModerationModePreBlock
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultContentModerationBaseURL
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.Model == "" {
		cfg.Model = defaultContentModerationModel
	}
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.TimeoutMS <= 0 {
		cfg.TimeoutMS = defaultContentModerationTimeoutMS
	}
	if cfg.TimeoutMS > maxContentModerationTimeoutMS {
		cfg.TimeoutMS = maxContentModerationTimeoutMS
	}
	if cfg.SampleRate < 0 {
		cfg.SampleRate = 0
	}
	if cfg.SampleRate > 100 {
		cfg.SampleRate = 100
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = defaultContentModerationWorkerCount
	}
	if cfg.WorkerCount > maxContentModerationWorkerCount {
		cfg.WorkerCount = maxContentModerationWorkerCount
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = defaultContentModerationQueueSize
	}
	if cfg.QueueSize > maxContentModerationQueueSize {
		cfg.QueueSize = maxContentModerationQueueSize
	}
	if strings.TrimSpace(cfg.BlockMessage) == "" {
		cfg.BlockMessage = defaultContentModerationBlockMessage
	}
	cfg.BlockMessage = strings.TrimSpace(cfg.BlockMessage)
	if cfg.BlockStatus <= 0 {
		cfg.BlockStatus = defaultContentModerationBlockHTTPStatus
	}
	if cfg.BanThreshold <= 0 {
		cfg.BanThreshold = defaultContentModerationBanThreshold
	}
	if cfg.BanDurationMinutes <= 0 {
		cfg.BanDurationMinutes = defaultContentModerationBanDurationMinutes
	}
	if cfg.ViolationWindowHours <= 0 {
		cfg.ViolationWindowHours = defaultContentModerationViolationWindowHours
	}
	if cfg.RetryCount < 0 {
		cfg.RetryCount = 0
	}
	if cfg.RetryCount > maxContentModerationRetryCount {
		cfg.RetryCount = maxContentModerationRetryCount
	}
	if cfg.HitRetentionDays <= 0 {
		cfg.HitRetentionDays = defaultContentModerationHitRetentionDays
	}
	if cfg.HitRetentionDays > maxContentModerationRetentionDays {
		cfg.HitRetentionDays = maxContentModerationRetentionDays
	}
	if cfg.NonHitRetentionDays <= 0 {
		cfg.NonHitRetentionDays = defaultContentModerationNonHitRetentionDays
	}
	if cfg.NonHitRetentionDays > maxContentModerationNonHitRetentionDays {
		cfg.NonHitRetentionDays = maxContentModerationNonHitRetentionDays
	}
	if cfg.ContextRetentionDays <= 0 {
		cfg.ContextRetentionDays = defaultContentModerationContextRetentionDays
	}
	if cfg.ContextRetentionDays > maxContentModerationContextRetentionDays {
		cfg.ContextRetentionDays = maxContentModerationContextRetentionDays
	}
	cfg.GroupIDs = normalizeInt64IDs(cfg.GroupIDs)
	cfg.Thresholds = mergeContentModerationThresholds(ContentModerationDefaultThresholds(), cfg.Thresholds)
	cfg.BlockedKeywords = normalizeBlockedKeywords(cfg.BlockedKeywords)
	cfg.KeywordBlockingMode = normalizeKeywordBlockingMode(cfg.KeywordBlockingMode)
	cfg.KeywordRules = normalizeContentModerationKeywordRules(cfg.KeywordRules, cfg.BlockedKeywords)
	cfg.AuditModels = normalizeContentModerationAuditModels(cfg.AuditModels)
	cfg.DecisionRule = normalizeContentModerationDecisionRule(cfg.DecisionRule)
	cfg.SelfUnban.normalize()
	if cfg.FlaggedWeight <= 0 {
		cfg.FlaggedWeight = defaultContentModerationFlaggedWeight
	}
	if cfg.BanWeight <= 0 {
		cfg.BanWeight = defaultContentModerationBanWeight
	}
	if cfg.ManualSuspiciousWeight <= 0 {
		cfg.ManualSuspiciousWeight = defaultContentModerationManualWeight
	}
	if cfg.DecayHalfLifeDays <= 0 {
		cfg.DecayHalfLifeDays = defaultContentModerationDecayHalfLifeDays
	}
	if cfg.MaxSampleRate <= 0 {
		cfg.MaxSampleRate = defaultContentModerationMaxSampleRate
	}
	if cfg.MaxSampleRate > 100 {
		cfg.MaxSampleRate = 100
	}
	if cfg.BanThresholdWeightStep <= 0 {
		cfg.BanThresholdWeightStep = defaultContentModerationBanThresholdStep
	}
	if cfg.MinEffectiveBanThreshold <= 0 {
		cfg.MinEffectiveBanThreshold = defaultContentModerationMinBanThreshold
	}
	if cfg.MinEffectiveBanThreshold > cfg.BanThreshold {
		cfg.MinEffectiveBanThreshold = cfg.BanThreshold
	}
	if cfg.BackgroundReviewBatchSize <= 0 {
		cfg.BackgroundReviewBatchSize = defaultContentModerationReviewBatchSize
	}
	if cfg.BackgroundReviewBatchSize > maxContentModerationReviewBatchSize {
		cfg.BackgroundReviewBatchSize = maxContentModerationReviewBatchSize
	}
	if cfg.BackgroundReviewMaxAttempts <= 0 {
		cfg.BackgroundReviewMaxAttempts = defaultContentModerationReviewMaxAttempts
	}
	if cfg.BackgroundReviewRetryBackoffSeconds <= 0 {
		cfg.BackgroundReviewRetryBackoffSeconds = defaultContentModerationReviewBackoffSeconds
	}
	if cfg.ContextMaxBytes <= 0 {
		cfg.ContextMaxBytes = defaultContentModerationContextMaxBytes
	}
	if cfg.ContextMaxBytes > maxContentModerationContextMaxBytes {
		cfg.ContextMaxBytes = maxContentModerationContextMaxBytes
	}
	cfg.CyberuseResponse.normalize()
	cfg.ModelFilter = normalizeContentModerationModelFilter(cfg.ModelFilter)
}

func (cfg *ContentModerationConfig) prepareForSave() {
	if cfg == nil {
		return
	}
	cfg.KeywordRules = filterGeneratedContentModerationKeywordRules(cfg.KeywordRules)
}

func (cfg *ContentModerationConfig) includesGroup(groupID *int64) bool {
	if cfg.AllGroups {
		return true
	}
	if groupID == nil {
		return false
	}
	for _, id := range cfg.GroupIDs {
		if id == *groupID {
			return true
		}
	}
	return false
}

func (cfg *ContentModerationConfig) includesModel(model string) bool {
	if cfg == nil {
		return true
	}
	filter := normalizeContentModerationModelFilter(cfg.ModelFilter)
	switch filter.Type {
	case ContentModerationModelFilterInclude:
		return contentModerationModelListContains(filter.Models, model)
	case ContentModerationModelFilterExclude:
		return !contentModerationModelListContains(filter.Models, model)
	default:
		return true
	}
}

func (cfg *ContentModerationConfig) cyberuseResponseApplies(userID int64) bool {
	if cfg == nil || !cfg.CyberuseResponse.Enabled {
		return false
	}
	return cfg.CyberuseResponse.appliesToUser(userID)
}

func contentModerationLogGroupID(groupID *int64) int64 {
	if groupID == nil {
		return 0
	}
	return *groupID
}

func contentModerationShouldSample(hashText string, sampleRate int) bool {
	if sampleRate >= 100 {
		return true
	}
	if sampleRate <= 0 {
		return false
	}
	raw, err := hex.DecodeString(hashText)
	if err != nil || len(raw) < 2 {
		return true
	}
	return int(binary.BigEndian.Uint16(raw[:2])%100) < sampleRate
}

func (cfg *ContentModerationConfig) apiKeys() []string {
	if cfg == nil {
		return nil
	}
	return normalizeModerationAPIKeys(cfg.APIKeys)
}

func (cfg *ContentModerationConfig) keywordRules() []ContentModerationKeywordRule {
	if cfg == nil {
		return nil
	}
	return normalizeContentModerationKeywordRules(cfg.KeywordRules, cfg.BlockedKeywords)
}

func (cfg *ContentModerationConfig) enabledAuditModels() []ContentModerationAuditModelConfig {
	if cfg == nil {
		return nil
	}
	models := normalizeContentModerationAuditModels(cfg.AuditModels)
	out := make([]ContentModerationAuditModelConfig, 0, len(models))
	for _, model := range models {
		if model.Enabled {
			out = append(out, model)
		}
	}
	return out
}

func (cfg *ContentModerationSelfUnbanConfig) normalize() {
	if cfg == nil {
		return
	}
	if cfg.WindowMinutes <= 0 {
		cfg.WindowMinutes = defaultContentModerationUnbanWindowMinutes
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 2
	}
	if cfg.SecondAttemptWaitMinutes <= 0 {
		cfg.SecondAttemptWaitMinutes = defaultContentModerationSecondUnbanWaitMins
	}
}

func normalizeContentModerationDecisionRule(rule ContentModerationDecisionRule) ContentModerationDecisionRule {
	switch strings.TrimSpace(rule.Type) {
	case ContentModerationDecisionRuleAll, ContentModerationDecisionRuleNOfM, ContentModerationDecisionRuleWeightThreshold:
		// keep
	default:
		rule.Type = ContentModerationDecisionRuleAny
	}
	if rule.RequiredCount <= 0 {
		rule.RequiredCount = 1
	}
	if rule.WeightThreshold <= 0 {
		rule.WeightThreshold = 1
	}
	return rule
}

func normalizeContentModerationAuditModels(models []ContentModerationAuditModelConfig) []ContentModerationAuditModelConfig {
	out := make([]ContentModerationAuditModelConfig, 0, len(models))
	for i, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		if model.ID == "" {
			model.ID = fmt.Sprintf("model_%d", i+1)
		}
		model.Name = strings.TrimSpace(model.Name)
		model.Protocol = strings.TrimSpace(model.Protocol)
		if model.Protocol == "" {
			model.Protocol = ContentModerationAuditProtocolOpenAICompatible
		}
		if model.Protocol != ContentModerationAuditProtocolInternalGroup {
			model.Protocol = ContentModerationAuditProtocolOpenAICompatible
			model.GroupID = nil
			model.GroupName = ""
			model.InternalAPIKeyID = nil
		}
		model.BaseURL = strings.TrimRight(strings.TrimSpace(model.BaseURL), "/")
		model.APIKey = strings.TrimSpace(model.APIKey)
		model.Model = strings.TrimSpace(model.Model)
		model.GroupName = strings.TrimSpace(model.GroupName)
		if model.Protocol == ContentModerationAuditProtocolInternalGroup {
			model.BaseURL = ""
			model.APIKey = ""
		}
		if model.TimeoutMS <= 0 {
			model.TimeoutMS = defaultContentModerationTimeoutMS
		}
		if model.TimeoutMS > maxContentModerationTimeoutMS {
			model.TimeoutMS = maxContentModerationTimeoutMS
		}
		if model.Weight <= 0 {
			model.Weight = 1
		}
		if strings.TrimSpace(model.PromptTemplate) == "" {
			model.PromptTemplate = "请审核以下用户请求是否违规。只输出 JSON：{\"violation\":boolean,\"risk_score\":0-1,\"reason\":\"...\",\"categories\":[\"...\"]}\n用户输入：{{input}}\n关键词命中：{{keyword_hits}}"
		}
		out = append(out, model)
	}
	return out
}

func maskedContentModerationAuditModels(models []ContentModerationAuditModelConfig) []ContentModerationAuditModelConfig {
	out := normalizeContentModerationAuditModels(models)
	for i := range out {
		if strings.TrimSpace(out[i].APIKey) != "" {
			out[i].APIKey = maskSecretTail(out[i].APIKey)
		}
	}
	return out
}

func mergeContentModerationAuditModelSecrets(existing []ContentModerationAuditModelConfig, incoming []ContentModerationAuditModelConfig) []ContentModerationAuditModelConfig {
	current := normalizeContentModerationAuditModels(existing)
	byID := make(map[string]ContentModerationAuditModelConfig, len(current))
	for _, model := range current {
		if strings.TrimSpace(model.ID) == "" {
			continue
		}
		byID[model.ID] = model
	}
	out := normalizeContentModerationAuditModels(incoming)
	for i := range out {
		if out[i].Protocol == ContentModerationAuditProtocolInternalGroup {
			continue
		}
		previous, ok := byID[out[i].ID]
		if !ok || strings.TrimSpace(previous.APIKey) == "" {
			continue
		}
		submitted := strings.TrimSpace(out[i].APIKey)
		if submitted == "" || submitted == maskSecretTail(previous.APIKey) {
			out[i].APIKey = previous.APIKey
		}
	}
	return out
}

func normalizeContentModerationKeywordRules(rules []ContentModerationKeywordRule, legacyKeywords []string) []ContentModerationKeywordRule {
	out := make([]ContentModerationKeywordRule, 0, len(rules)+1)
	for i, rule := range filterGeneratedContentModerationKeywordRules(rules) {
		rule.ID = strings.TrimSpace(rule.ID)
		if rule.ID == "" {
			rule.ID = fmt.Sprintf("rule_%d", i+1)
		}
		rule.Group = strings.TrimSpace(rule.Group)
		if rule.Group == "" {
			rule.Group = "default"
		}
		rule.MatchType = strings.TrimSpace(rule.MatchType)
		if rule.MatchType != ContentModerationKeywordMatchRegex {
			rule.MatchType = ContentModerationKeywordMatchContains
		}
		rule.Patterns = normalizeBlockedKeywords(rule.Patterns)
		if len(rule.Fields) == 0 {
			rule.Fields = []string{"input"}
		}
		if len(rule.Actions) == 0 {
			rule.Actions = []string{ContentModerationActionRecordAudit}
		}
		if !rule.Enabled && len(rule.Patterns) > 0 {
			// zero-value configs from old clients should still work unless explicitly empty.
			rule.Enabled = true
		}
		if len(rule.Patterns) > 0 {
			out = append(out, rule)
		}
	}
	legacy := normalizeBlockedKeywords(legacyKeywords)
	if len(legacy) > 0 {
		out = append(out, ContentModerationKeywordRule{
			ID:         "legacy_blocked_keywords",
			Group:      "legacy",
			MatchType:  ContentModerationKeywordMatchContains,
			Patterns:   legacy,
			Fields:     []string{"input"},
			Actions:    []string{ContentModerationActionKeywordBlock},
			Enabled:    true,
			IgnoreCase: true,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority > out[j].Priority })
	return out
}

func filterGeneratedContentModerationKeywordRules(rules []ContentModerationKeywordRule) []ContentModerationKeywordRule {
	out := make([]ContentModerationKeywordRule, 0, len(rules))
	for _, rule := range rules {
		if strings.TrimSpace(rule.ID) == "legacy_blocked_keywords" {
			continue
		}
		out = append(out, rule)
	}
	return out
}

func matchContentModerationKeywordRules(text string, rules []ContentModerationKeywordRule) []ContentModerationKeywordHit {
	text = strings.TrimSpace(text)
	if text == "" || len(rules) == 0 {
		return nil
	}
	hits := make([]ContentModerationKeywordHit, 0)
	for _, rule := range rules {
		if !rule.Enabled || len(rule.Patterns) == 0 {
			continue
		}
		searchText := text
		if rule.IgnoreCase || rule.MatchType == ContentModerationKeywordMatchContains {
			searchText = strings.ToLower(text)
		}
		for _, pattern := range rule.Patterns {
			pattern = strings.TrimSpace(pattern)
			if pattern == "" {
				continue
			}
			matched := ""
			switch rule.MatchType {
			case ContentModerationKeywordMatchRegex:
				re, err := regexp.Compile(pattern)
				if err != nil {
					continue
				}
				matched = re.FindString(text)
			default:
				needle := pattern
				if rule.IgnoreCase || rule.MatchType == ContentModerationKeywordMatchContains {
					needle = strings.ToLower(pattern)
				}
				if idx := strings.Index(searchText, needle); idx >= 0 {
					matched = substringByByteRange(text, idx, idx+len(needle))
				}
			}
			if matched == "" {
				continue
			}
			action := ContentModerationActionRecordAudit
			if len(rule.Actions) > 0 {
				action = strings.TrimSpace(rule.Actions[0])
			}
			hits = append(hits, ContentModerationKeywordHit{RuleID: rule.ID, Group: rule.Group, MatchType: rule.MatchType, Keyword: pattern, MatchedText: matched, Field: firstString(rule.Fields, "input"), Action: action, Whitelist: rule.Whitelist, Priority: rule.Priority})
		}
	}
	if keywordHitsHaveWhitelist(hits) {
		filtered := hits[:0]
		for _, hit := range hits {
			if hit.Whitelist {
				filtered = append(filtered, hit)
			}
		}
		return filtered
	}
	return hits
}

func keywordHitsHaveWhitelist(hits []ContentModerationKeywordHit) bool {
	for _, hit := range hits {
		if hit.Whitelist {
			return true
		}
	}
	return false
}

func keywordHitsRequireAction(hits []ContentModerationKeywordHit, action string) bool {
	for _, hit := range hits {
		if !hit.Whitelist && (hit.Action == action || hit.Action == "") {
			return true
		}
	}
	return false
}

func keywordHitRequiresBlocking(hits []ContentModerationKeywordHit) bool {
	for _, hit := range hits {
		if hit.Whitelist {
			return false
		}
		if hit.Action == ContentModerationActionBlock || hit.Action == ContentModerationActionBan || hit.Action == ContentModerationActionKeywordBlock {
			return true
		}
	}
	return false
}

func firstString(values []string, fallback string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return fallback
}

func substringByByteRange(s string, start, end int) string {
	if start < 0 || end > len(s) || start >= end {
		return ""
	}
	return s[start:end]
}

func cloneContentModerationKeywordHits(hits []ContentModerationKeywordHit) []ContentModerationKeywordHit {
	if len(hits) == 0 {
		return nil
	}
	out := make([]ContentModerationKeywordHit, len(hits))
	copy(out, hits)
	return out
}

func renderContentModerationAuditPrompt(template string, input string, check ContentModerationCheckInput, hits []ContentModerationKeywordHit) string {
	hitsRaw, _ := json.Marshal(hits)
	replacer := strings.NewReplacer(
		"{{input}}", input,
		"{{keyword_hits}}", string(hitsRaw),
		"{{user_id}}", fmt.Sprintf("%d", check.UserID),
		"{{endpoint}}", check.Endpoint,
		"{{model}}", check.Model,
	)
	return replacer.Replace(template)
}

func buildContentModerationRequestContext(input ContentModerationCheckInput, text string) ContentModerationRequestContext {
	return ContentModerationRequestContext{RequestID: input.RequestID, UserID: input.UserID, UserEmail: input.UserEmail, APIKeyID: input.APIKeyID, APIKeyName: input.APIKeyName, GroupID: cloneInt64Ptr(input.GroupID), GroupName: input.GroupName, Endpoint: input.Endpoint, Provider: input.Provider, Model: input.Model, Protocol: input.Protocol, Input: trimRunes(redactContentModerationSecrets(text), maxModerationExcerptRunes)}
}

func firstContentModerationMatchedKeyword(hits []ContentModerationKeywordHit) string {
	for _, hit := range hits {
		if keyword := strings.TrimSpace(hit.Keyword); keyword != "" {
			return keyword
		}
		if matched := strings.TrimSpace(hit.MatchedText); matched != "" {
			return matched
		}
	}
	return ""
}

func parseContentModerationModelTextResult(text string) ContentModerationModelResult {
	trimmed := strings.TrimSpace(text)
	var result ContentModerationModelResult
	parsedJSON := false
	if start := strings.Index(trimmed, "{"); start >= 0 {
		if end := strings.LastIndex(trimmed, "}"); end >= start {
			parsedJSON = json.Unmarshal([]byte(trimmed[start:end+1]), &result) == nil
		}
	}
	if result.RiskScore < 0 {
		result.RiskScore = 0
	}
	if result.RiskScore > 1 {
		result.RiskScore = 1
	}
	lower := strings.ToLower(trimmed)
	if !parsedJSON && !result.Violation {
		if strings.Contains(lower, "violation") || strings.Contains(trimmed, "违规") || strings.Contains(trimmed, "封禁") || strings.Contains(lower, "block") {
			result.Violation = true
		}
	}
	if result.Reason == "" {
		result.Reason = trimRunes(trimmed, 300)
	}
	if result.Violation && result.RiskScore == 0 {
		result.RiskScore = 1
	}
	return result
}

func aggregateContentModerationModelResults(details []ContentModerationModelAuditDetail, rule ContentModerationDecisionRule, models []ContentModerationAuditModelConfig) ContentModerationAggregateDecision {
	rule = normalizeContentModerationDecisionRule(rule)
	weights := map[string]float64{}
	for _, model := range models {
		weights[model.ID] = model.Weight
	}
	decision := ContentModerationAggregateDecision{RuleType: rule.Type, TotalCount: len(details)}
	for _, detail := range details {
		if strings.TrimSpace(detail.Error) != "" {
			decision.TotalCount--
			continue
		}
		weight := weights[detail.ModelID]
		if weight <= 0 {
			weight = 1
		}
		decision.TotalWeight += weight
		if detail.Result.Violation {
			decision.ViolationCount++
			decision.ViolationWeight += weight
		}
	}
	switch rule.Type {
	case ContentModerationDecisionRuleAll:
		decision.Flagged = decision.TotalCount > 0 && decision.ViolationCount == decision.TotalCount
	case ContentModerationDecisionRuleNOfM:
		decision.Flagged = decision.ViolationCount >= rule.RequiredCount
	case ContentModerationDecisionRuleWeightThreshold:
		decision.Flagged = decision.ViolationWeight >= rule.WeightThreshold
	default:
		decision.Flagged = decision.ViolationCount > 0
	}
	decision.Reason = fmt.Sprintf("%s: %d/%d models flagged, weight %.2f/%.2f", decision.RuleType, decision.ViolationCount, decision.TotalCount, decision.ViolationWeight, decision.TotalWeight)
	return decision
}

func (s *ContentModerationService) recordAuditModelCall(model ContentModerationAuditModelConfig, latencyMS int, httpStatus int, flagged bool, err error) {
	if s == nil || strings.TrimSpace(model.ID) == "" {
		return
	}
	if latencyMS < 0 {
		latencyMS = 0
	}
	s.auditModelHealthMu.Lock()
	defer s.auditModelHealthMu.Unlock()
	if s.auditModelHealth == nil {
		s.auditModelHealth = map[string]*contentModerationAuditModelHealth{}
	}
	state := s.auditModelHealth[model.ID]
	if state == nil {
		state = &contentModerationAuditModelHealth{ModelID: model.ID}
		s.auditModelHealth[model.ID] = state
	}
	state.Name = model.Name
	state.Model = model.Model
	state.TotalLatencyMS += int64(latencyMS)
	state.LastLatencyMS = latencyMS
	state.LastHTTPStatus = httpStatus
	state.LastCheckedAt = time.Now()
	if flagged {
		state.FlaggedCount++
	}
	if err != nil {
		state.FailureCount++
		state.LastError = trimRunes(contentModerationAuditModelErrorDetail(model, httpStatus, err), 180)
		return
	}
	state.SuccessCount++
	state.LastError = ""
}

func (s *ContentModerationService) recordAuditModelDisagreements(details []ContentModerationModelAuditDetail, finalFlagged bool) {
	if s == nil || len(details) == 0 {
		return
	}
	s.auditModelHealthMu.Lock()
	defer s.auditModelHealthMu.Unlock()
	for _, detail := range details {
		if detail.ModelID == "" || detail.Result.Violation == finalFlagged {
			continue
		}
		if s.auditModelHealth == nil {
			s.auditModelHealth = map[string]*contentModerationAuditModelHealth{}
		}
		state := s.auditModelHealth[detail.ModelID]
		if state == nil {
			state = &contentModerationAuditModelHealth{ModelID: detail.ModelID, Model: detail.Model}
			s.auditModelHealth[detail.ModelID] = state
		}
		state.DisagreementCount++
	}
}

func (s *ContentModerationService) auditModelStatuses(models []ContentModerationAuditModelConfig) []ContentModerationAuditModelRuntimeStatus {
	out := make([]ContentModerationAuditModelRuntimeStatus, 0, len(models))
	if s == nil {
		return out
	}
	s.auditModelHealthMu.Lock()
	defer s.auditModelHealthMu.Unlock()
	for _, model := range models {
		status := ContentModerationAuditModelRuntimeStatus{
			ModelID: model.ID,
			Name:    model.Name,
			Model:   model.Model,
			Status:  contentModerationAuditModelStatusUnknown,
		}
		state := s.auditModelHealth[model.ID]
		if state != nil {
			status.SuccessCount = state.SuccessCount
			status.FailureCount = state.FailureCount
			status.FlaggedCount = state.FlaggedCount
			status.DisagreementCount = state.DisagreementCount
			status.TotalCalls = state.SuccessCount + state.FailureCount
			status.LastLatencyMS = state.LastLatencyMS
			status.LastHTTPStatus = state.LastHTTPStatus
			status.LastError = state.LastError
			if !state.LastCheckedAt.IsZero() {
				t := state.LastCheckedAt
				status.LastCheckedAt = &t
			}
			if status.TotalCalls > 0 {
				status.AvgLatencyMS = state.TotalLatencyMS / status.TotalCalls
			}
			if state.LastError != "" && state.FailureCount > 0 {
				status.Status = contentModerationAuditModelStatusError
			} else if status.TotalCalls > 0 {
				status.Status = contentModerationAuditModelStatusOK
			}
		}
		out = append(out, status)
	}
	return out
}

func (s *ContentModerationService) nextUsableAPIKey(cfg *ContentModerationConfig) (string, bool) {
	keys := cfg.apiKeys()
	if len(keys) == 0 {
		return "", false
	}
	now := time.Now()
	for i := 0; i < len(keys); i++ {
		idx := int(s.apiKeyCursor.Add(1)-1) % len(keys)
		key := keys[idx]
		if !s.isAPIKeyFrozen(key, now) {
			return key, true
		}
	}
	return "", false
}

func (s *ContentModerationService) isAPIKeyFrozen(key string, now time.Time) bool {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return false
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.keyHealth[hash]
	return state != nil && state.FrozenUntil.After(now)
}

func (s *ContentModerationService) beginModerationAPIKeyCall(key string) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	state.SyncActive++
}

func (s *ContentModerationService) finishModerationAPIKeyCall(key string, latencyMS int, success bool) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	if latencyMS < 0 {
		latencyMS = 0
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	if state.SyncActive > 0 {
		state.SyncActive--
	}
	state.SyncTotal++
	state.SyncLatencyMS += int64(latencyMS)
	if success {
		state.SyncSuccess++
		return
	}
	state.SyncErrors++
}

func (s *ContentModerationService) markAPIKeySuccess(key string, latencyMS int, httpStatus int) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	state.FailureCount = 0
	state.SuccessCount++
	state.LastError = ""
	state.LastCheckedAt = time.Now()
	state.FrozenUntil = time.Time{}
	state.LastLatencyMS = latencyMS
	state.LastHTTPStatus = httpStatus
	state.LastTested = true
}

func (s *ContentModerationService) markAPIKeyError(key string, errText string, latencyMS int, httpStatus int) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	if contentModerationFreezeDurationForHTTPStatus(httpStatus) > 0 {
		state.FailureCount++
	}
	state.LastError = trimRunes(errText, 180)
	state.LastCheckedAt = time.Now()
	state.LastLatencyMS = latencyMS
	state.LastHTTPStatus = httpStatus
	state.LastTested = true
	if freezeDuration := contentModerationFreezeDurationForHTTPStatus(httpStatus); freezeDuration > 0 {
		state.FrozenUntil = time.Now().Add(freezeDuration)
	}
}

func contentModerationFreezeDurationForHTTPStatus(httpStatus int) time.Duration {
	switch httpStatus {
	case 0, http.StatusBadRequest:
		return 0
	case http.StatusUnauthorized, http.StatusForbidden:
		return contentModerationKeyAuthFreezeDuration
	case http.StatusTooManyRequests, 529:
		return contentModerationKeyRateLimitFreezeDuration
	default:
		return contentModerationKeyHTTPErrorFreezeDuration
	}
}

func (s *ContentModerationService) ensureAPIKeyHealthLocked(hash string, masked string) *contentModerationKeyHealth {
	if s.keyHealth == nil {
		s.keyHealth = make(map[string]*contentModerationKeyHealth)
	}
	state := s.keyHealth[hash]
	if state == nil {
		state = &contentModerationKeyHealth{Hash: hash}
		s.keyHealth[hash] = state
	}
	if strings.TrimSpace(masked) != "" {
		state.Masked = masked
	}
	return state
}

func (s *ContentModerationService) configView(cfg *ContentModerationConfig) *ContentModerationConfigView {
	keys := cfg.apiKeys()
	masks := make([]string, 0, len(keys))
	for _, key := range keys {
		masks = append(masks, maskSecretTail(key))
	}
	apiKeyMasked := ""
	if len(masks) > 0 {
		apiKeyMasked = masks[0]
	}
	return &ContentModerationConfigView{
		Enabled:                             cfg.Enabled,
		Mode:                                cfg.Mode,
		BaseURL:                             cfg.BaseURL,
		Model:                               cfg.Model,
		APIKeyConfigured:                    len(keys) > 0,
		APIKeyMasked:                        apiKeyMasked,
		APIKeyCount:                         len(keys),
		APIKeyMasks:                         masks,
		APIKeyStatuses:                      s.apiKeyStatuses(keys),
		TimeoutMS:                           cfg.TimeoutMS,
		SampleRate:                          cfg.SampleRate,
		AllGroups:                           cfg.AllGroups,
		GroupIDs:                            append([]int64(nil), cfg.GroupIDs...),
		RecordNonHits:                       cfg.RecordNonHits,
		Thresholds:                          cloneFloatMap(cfg.Thresholds),
		WorkerCount:                         cfg.WorkerCount,
		QueueSize:                           cfg.QueueSize,
		BlockStatus:                         cfg.BlockStatus,
		BlockMessage:                        cfg.BlockMessage,
		EmailOnHit:                          cfg.EmailOnHit,
		AutoBanEnabled:                      cfg.AutoBanEnabled,
		BanThreshold:                        cfg.BanThreshold,
		BanDurationMinutes:                  cfg.BanDurationMinutes,
		ViolationWindowHours:                cfg.ViolationWindowHours,
		RetryCount:                          cfg.RetryCount,
		HitRetentionDays:                    cfg.HitRetentionDays,
		NonHitRetentionDays:                 cfg.NonHitRetentionDays,
		ContextRetentionDays:                cfg.ContextRetentionDays,
		PreHashCheckEnabled:                 cfg.PreHashCheckEnabled,
		BlockedKeywords:                     append([]string(nil), cfg.BlockedKeywords...),
		KeywordBlockingMode:                 cfg.KeywordBlockingMode,
		KeywordRules:                        filterGeneratedContentModerationKeywordRules(cfg.KeywordRules),
		ModelFilter:                         cloneContentModerationModelFilter(cfg.ModelFilter),
		AuditModels:                         maskedContentModerationAuditModels(cfg.AuditModels),
		DecisionRule:                        cfg.DecisionRule,
		SelfUnban:                           cfg.SelfUnban,
		RiskWeightEnabled:                   cfg.RiskWeightEnabled,
		FlaggedWeight:                       cfg.FlaggedWeight,
		BanWeight:                           cfg.BanWeight,
		ManualSuspiciousWeight:              cfg.ManualSuspiciousWeight,
		DecayHalfLifeDays:                   cfg.DecayHalfLifeDays,
		MaxSampleRate:                       cfg.MaxSampleRate,
		BanThresholdWeightStep:              cfg.BanThresholdWeightStep,
		MinEffectiveBanThreshold:            cfg.MinEffectiveBanThreshold,
		BackgroundReviewEnabled:             cfg.BackgroundReviewEnabled,
		BackgroundReviewBatchSize:           cfg.BackgroundReviewBatchSize,
		BackgroundReviewMaxAttempts:         cfg.BackgroundReviewMaxAttempts,
		BackgroundReviewRetryBackoffSeconds: cfg.BackgroundReviewRetryBackoffSeconds,
		ContextCaptureEnabled:               cfg.ContextCaptureEnabled,
		ContextMaxBytes:                     cfg.ContextMaxBytes,
		CyberuseResponse:                    cloneContentModerationCyberuseConfig(cfg.CyberuseResponse),
		CyberPolicyExcludeFromBanCount:      cfg.CyberPolicyExcludeFromBanCount,
	}
}

func (s *ContentModerationService) apiKeyStatuses(keys []string) []ContentModerationAPIKeyStatus {
	out := make([]ContentModerationAPIKeyStatus, 0, len(keys))
	for idx, key := range keys {
		out = append(out, s.apiKeyStatusForHash(idx, moderationAPIKeyHash(key), maskSecretTail(key), true))
	}
	return out
}

func (s *ContentModerationService) preBlockAPIKeyLoads(keys []string) []ContentModerationAPIKeyLoad {
	out := make([]ContentModerationAPIKeyLoad, 0, len(keys))
	for idx, key := range keys {
		out = append(out, s.preBlockAPIKeyLoadForHash(idx, moderationAPIKeyHash(key), maskSecretTail(key)))
	}
	return out
}

func (s *ContentModerationService) preBlockAPIKeyActive(keys []string) int64 {
	var total int64
	for _, item := range s.preBlockAPIKeyLoads(keys) {
		total += item.Active
	}
	return total
}

func (s *ContentModerationService) preBlockAPIKeyAvailableCount(keys []string) int64 {
	now := time.Now()
	var count int64
	for _, key := range keys {
		if !s.isAPIKeyFrozen(key, now) {
			count++
		}
	}
	return count
}

func (s *ContentModerationService) preBlockAPIKeyTotalCalls(keys []string) int64 {
	var total int64
	for _, item := range s.preBlockAPIKeyLoads(keys) {
		total += item.Total
	}
	return total
}

func (s *ContentModerationService) preBlockAPIKeyLoadForHash(index int, hash string, masked string) ContentModerationAPIKeyLoad {
	load := ContentModerationAPIKeyLoad{
		Index:   index,
		KeyHash: hash,
		Masked:  masked,
		Status:  "unknown",
	}
	status := s.apiKeyStatusForHash(index, hash, masked, true)
	load.Status = status.Status
	load.LastLatencyMS = status.LastLatencyMS
	load.LastHTTPStatus = status.LastHTTPStatus
	if hash == "" || s == nil {
		return load
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.keyHealth[hash]
	if state == nil {
		return load
	}
	load.Active = state.SyncActive
	load.Total = state.SyncTotal
	load.Success = state.SyncSuccess
	load.Errors = state.SyncErrors
	if state.SyncTotal > 0 {
		load.AvgLatencyMS = state.SyncLatencyMS / state.SyncTotal
	}
	return load
}

func (s *ContentModerationService) apiKeyStatusForHash(index int, hash string, masked string, configured bool) ContentModerationAPIKeyStatus {
	status := ContentModerationAPIKeyStatus{
		Index:      index,
		KeyHash:    hash,
		Masked:     masked,
		Status:     "unknown",
		Configured: configured,
	}
	if hash == "" || s == nil {
		return status
	}
	now := time.Now()
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.keyHealth[hash]
	if state == nil {
		return status
	}
	status.FailureCount = state.FailureCount
	status.SuccessCount = state.SuccessCount
	status.LastError = state.LastError
	status.LastLatencyMS = state.LastLatencyMS
	status.LastHTTPStatus = state.LastHTTPStatus
	status.LastTested = state.LastTested
	if !state.LastCheckedAt.IsZero() {
		t := state.LastCheckedAt
		status.LastCheckedAt = &t
	}
	if state.FrozenUntil.After(now) {
		t := state.FrozenUntil
		status.FrozenUntil = &t
		status.Status = "frozen"
		return status
	}
	if state.LastError != "" {
		status.Status = "error"
		return status
	}
	if state.SuccessCount > 0 || state.LastTested {
		status.Status = "ok"
	}
	return status
}

func moderationAPIKeyHash(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func buildModerationTestInput(prompt string, images []string) (any, int, error) {
	prompt = trimRunes(normalizeContentModerationText(prompt), maxModerationInputRunes)
	normalizedImages := make([]string, 0, len(images))
	for _, image := range images {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if len(normalizedImages) >= maxContentModerationTestImages {
			return nil, 0, infraerrors.BadRequest("TOO_MANY_MODERATION_TEST_IMAGES", fmt.Sprintf("最多上传 %d 张测试图片", maxContentModerationTestImages))
		}
		if err := validateModerationTestImageDataURL(image); err != nil {
			return nil, 0, err
		}
		normalizedImages = append(normalizedImages, image)
	}
	if prompt == "" && len(normalizedImages) == 0 {
		return "hello", 0, nil
	}
	if len(normalizedImages) == 0 {
		return prompt, 0, nil
	}
	parts := make([]moderationAPIInputPart, 0, len(normalizedImages)+1)
	if prompt != "" {
		parts = append(parts, moderationAPIInputPart{Type: "text", Text: prompt})
	}
	for _, image := range normalizedImages {
		parts = append(parts, moderationAPIInputPart{
			Type:     "image_url",
			ImageURL: &moderationAPIImageURLRef{URL: image},
		})
	}
	return parts, len(normalizedImages), nil
}

func contentModerationTestHasAuditInput(prompt string, images []string) bool {
	if normalizeContentModerationText(prompt) != "" {
		return true
	}
	for _, image := range images {
		if strings.TrimSpace(image) != "" {
			return true
		}
	}
	return false
}

func validateModerationTestImageDataURL(value string) error {
	if len(value) > maxContentModerationTestImageDataURLBytes {
		return infraerrors.BadRequest("MODERATION_TEST_IMAGE_TOO_LARGE", "测试图片不能超过 8MB")
	}
	if !strings.HasPrefix(value, "data:image/") {
		return infraerrors.BadRequest("INVALID_MODERATION_TEST_IMAGE", "测试图片必须是 data:image/* base64")
	}
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 || !strings.Contains(parts[0], ";base64") {
		return infraerrors.BadRequest("INVALID_MODERATION_TEST_IMAGE", "测试图片必须是 base64 data URL")
	}
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return infraerrors.BadRequest("INVALID_MODERATION_TEST_IMAGE", "测试图片 base64 无效")
	}
	if len(raw) > maxContentModerationTestImageBytes {
		return infraerrors.BadRequest("MODERATION_TEST_IMAGE_TOO_LARGE", "测试图片不能超过 8MB")
	}
	return nil
}

func buildContentModerationTestAuditResult(result *moderationAPIResult, thresholds map[string]float64) *ContentModerationTestAuditResult {
	if result == nil {
		return nil
	}
	scores := make(map[string]float64, len(result.CategoryScores))
	for category, score := range result.CategoryScores {
		scores[category] = score
	}
	thresholdSnapshot := mergeContentModerationThresholds(ContentModerationDefaultThresholds(), thresholds)
	flagged, highestCategory, highestScore := evaluateModerationScores(scores, thresholdSnapshot)
	compositeScore := highestScore
	return &ContentModerationTestAuditResult{
		Flagged:         flagged,
		HighestCategory: highestCategory,
		HighestScore:    highestScore,
		CompositeScore:  compositeScore,
		CategoryScores:  scores,
		Thresholds:      thresholdSnapshot,
	}
}

type openAIChatCompletionRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIChatMessage `json:"messages"`
	Temperature float64             `json:"temperature"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatCompletionResponse struct {
	Choices []struct {
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
}

type moderationAPIRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type moderationAPIInputPart struct {
	Type     string                    `json:"type"`
	Text     string                    `json:"text,omitempty"`
	ImageURL *moderationAPIImageURLRef `json:"image_url,omitempty"`
}

type moderationAPIImageURLRef struct {
	URL string `json:"url"`
}

type moderationAPIResponse struct {
	Results []moderationAPIResult `json:"results"`
}

type moderationAPIResult struct {
	Flagged        bool               `json:"flagged"`
	CategoryScores map[string]float64 `json:"category_scores"`
}

func evaluateModerationScores(scores map[string]float64, thresholds map[string]float64) (bool, string, float64) {
	flagged := false
	highestCategory := ""
	highestScore := 0.0
	for _, category := range contentModerationCategoryOrder {
		score := scores[category]
		if score > highestScore || highestCategory == "" {
			highestScore = score
			highestCategory = category
		}
		if score >= thresholds[category] {
			flagged = true
		}
	}
	for category, score := range scores {
		if score > highestScore || highestCategory == "" {
			highestScore = score
			highestCategory = category
		}
	}
	return flagged, highestCategory, highestScore
}

func mergeContentModerationThresholds(base map[string]float64, override map[string]float64) map[string]float64 {
	out := cloneFloatMap(base)
	if out == nil {
		out = map[string]float64{}
	}
	for _, category := range contentModerationCategoryOrder {
		if v, ok := override[category]; ok {
			if v < 0 {
				v = 0
			}
			if v > 1 {
				v = 1
			}
			out[category] = v
		}
	}
	return out
}

func normalizeInt64IDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return []int64{}
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func normalizeBlockedKeywords(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		kw := strings.TrimSpace(raw)
		if kw == "" {
			continue
		}
		kw = trimRunes(kw, maxContentModerationBlockedKeywordRunes)
		key := strings.ToLower(kw)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, kw)
		if len(out) >= maxContentModerationBlockedKeywords {
			break
		}
	}
	return out
}

func normalizeKeywordBlockingMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case ContentModerationKeywordModeKeywordOnly:
		return ContentModerationKeywordModeKeywordOnly
	case ContentModerationKeywordModeAPIOnly:
		return ContentModerationKeywordModeAPIOnly
	case ContentModerationKeywordModeKeywordAndAPI:
		return ContentModerationKeywordModeKeywordAndAPI
	default:
		return ContentModerationKeywordModeKeywordAndAPI
	}
}

func normalizeContentModerationModelFilter(filter ContentModerationModelFilter) ContentModerationModelFilter {
	out := ContentModerationModelFilter{
		Type:   normalizeContentModerationModelFilterType(filter.Type),
		Models: normalizeContentModerationModelNames(filter.Models),
	}
	if out.Type == ContentModerationModelFilterAll {
		out.Models = []string{}
	}
	return out
}

func cloneContentModerationModelFilter(filter ContentModerationModelFilter) ContentModerationModelFilter {
	normalized := normalizeContentModerationModelFilter(filter)
	normalized.Models = append([]string(nil), normalized.Models...)
	return normalized
}

func cloneContentModerationCyberuseConfig(in ContentModerationCyberuseConfig) ContentModerationCyberuseConfig {
	in.normalize()
	in.UserScope.UserIDs = append([]int64(nil), in.UserScope.UserIDs...)
	return in
}

func (cfg *ContentModerationCyberuseConfig) normalize() {
	if cfg == nil {
		return
	}
	cfg.ErrorCode = normalizeContentModerationErrorCode(cfg.ErrorCode, defaultContentModerationCyberuseErrorCode)
	cfg.Message = strings.TrimSpace(cfg.Message)
	if cfg.Message == "" {
		cfg.Message = defaultContentModerationCyberuseMessage
	}
	cfg.AnnouncementTitle = trimRunes(strings.TrimSpace(cfg.AnnouncementTitle), 120)
	cfg.AnnouncementContent = trimRunes(strings.TrimSpace(cfg.AnnouncementContent), 1000)
	cfg.UserScope.normalize()
}

func (scope *ContentModerationCyberuseUserScope) normalize() {
	if scope == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(scope.Mode)) {
	case ContentModerationCyberuseUserScopeInclude:
		scope.Mode = ContentModerationCyberuseUserScopeInclude
	case ContentModerationCyberuseUserScopeExclude:
		scope.Mode = ContentModerationCyberuseUserScopeExclude
	default:
		scope.Mode = ContentModerationCyberuseUserScopeAll
	}
	scope.UserIDs = normalizeInt64IDs(scope.UserIDs)
}

func (cfg ContentModerationCyberuseConfig) appliesToUser(userID int64) bool {
	cfg.normalize()
	switch cfg.UserScope.Mode {
	case ContentModerationCyberuseUserScopeInclude:
		return int64ListContains(cfg.UserScope.UserIDs, userID)
	case ContentModerationCyberuseUserScopeExclude:
		return !int64ListContains(cfg.UserScope.UserIDs, userID)
	default:
		return true
	}
}

func int64ListContains(ids []int64, id int64) bool {
	if id <= 0 {
		return false
	}
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func normalizeContentModerationErrorCode(code string, fallback string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		code = fallback
	}
	code = strings.ToLower(code)
	var b strings.Builder
	for _, r := range code {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			_ = b.WriteByte(byte(r))
		}
	}
	out := strings.Trim(b.String(), "_-")
	if out == "" {
		return fallback
	}
	return trimRunes(out, 64)
}

func normalizeContentModerationModelFilterType(filterType string) string {
	switch strings.ToLower(strings.TrimSpace(filterType)) {
	case ContentModerationModelFilterInclude:
		return ContentModerationModelFilterInclude
	case ContentModerationModelFilterExclude:
		return ContentModerationModelFilterExclude
	case ContentModerationModelFilterAll:
		return ContentModerationModelFilterAll
	default:
		return ContentModerationModelFilterAll
	}
}

func normalizeContentModerationModelNames(models []string) []string {
	if len(models) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, raw := range models {
		model := trimRunes(strings.TrimSpace(raw), maxContentModerationModelFilterRunes)
		if model == "" {
			continue
		}
		key := strings.ToLower(model)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, model)
		if len(out) >= maxContentModerationModelFilterModels {
			break
		}
	}
	return out
}

func contentModerationModelListContains(models []string, model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}
	for _, candidate := range models {
		if strings.ToLower(strings.TrimSpace(candidate)) == model {
			return true
		}
	}
	return false
}

func matchBlockedKeyword(text string, keywords []string) (string, bool) {
	if text == "" || len(keywords) == 0 {
		return "", false
	}
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		if kw == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(kw)) {
			return kw, true
		}
	}
	return "", false
}

func normalizeModerationAPIKeys(keys []string) []string {
	if len(keys) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func deleteModerationAPIKeysByHash(keys []string, hashes []string) []string {
	keys = normalizeModerationAPIKeys(keys)
	deleteHashes := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		hash = normalizeContentModerationHash(hash)
		if hash != "" {
			deleteHashes[hash] = struct{}{}
		}
	}
	if len(deleteHashes) == 0 {
		return keys
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, ok := deleteHashes[moderationAPIKeyHash(key)]; ok {
			continue
		}
		out = append(out, key)
	}
	return out
}

func normalizeContentModerationAPIKeysMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case contentModerationAPIKeysModeReplace:
		return contentModerationAPIKeysModeReplace
	default:
		return contentModerationAPIKeysModeAppend
	}
}

func normalizeContentModerationHash(inputHash string) string {
	inputHash = strings.ToLower(strings.TrimSpace(inputHash))
	if len(inputHash) != sha256.Size*2 {
		return ""
	}
	if _, err := hex.DecodeString(inputHash); err != nil {
		return ""
	}
	return inputHash
}

func cloneFloatMap(in map[string]float64) map[string]float64 {
	if in == nil {
		return map[string]float64{}
	}
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneInt64Ptr(in *int64) *int64 {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func trimRunes(text string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}

func trimRunesFromEnd(text string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[len(runes)-max:])
}

func maskSecretTail(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return "****"
	}
	return strings.Repeat("*", 8) + secret[len(secret)-4:]
}

// CyberPolicyRecordInput 是一次 cyber_policy 硬阻断的风控记录入参。
type CyberPolicyRecordInput struct {
	RequestID       string
	UserID          int64
	UserEmail       string
	APIKeyID        int64
	APIKeyName      string
	GroupID         *int64
	GroupName       string
	Endpoint        string
	Model           string
	UpstreamMessage string
	UpstreamBody    string
	UpstreamStatus  int
	UpstreamInTok   int
	UpstreamOutTok  int
}

// RecordCyberPolicyEvent 把一次 cyber_policy 硬阻断写入风控中心日志、计入违规计数、
// 并给用户发邮件。当前请求已由 gateway 透传给用户；本方法仅做事后记录/通知/计数。
// 仅受 risk_control_enabled 总开关约束（不受内容审核 Enabled/Mode/scope/sample 约束）。
func (s *ContentModerationService) RecordCyberPolicyEvent(ctx context.Context, in CyberPolicyRecordInput) {
	if s == nil || s.repo == nil {
		return
	}
	if !s.isRiskControlEnabled(ctx) {
		return
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		slog.Warn("content_moderation.cyber_load_config_failed", "error", err)
		cfg = &ContentModerationConfig{}
	}
	var userID *int64
	if in.UserID > 0 {
		userID = &in.UserID
	}
	var apiKeyID *int64
	if in.APIKeyID > 0 {
		apiKeyID = &in.APIKeyID
	}
	errBody := strings.TrimSpace(in.UpstreamMessage)
	if b := strings.TrimSpace(in.UpstreamBody); b != "" {
		// 原始 body 不在此预脱敏；写入 log.Error 前由 redactContentModerationSecrets 统一脱敏。
		errBody = strings.TrimSpace(errBody + "\n" + b)
	}
	if in.UpstreamInTok > 0 || in.UpstreamOutTok > 0 {
		errBody = fmt.Sprintf("%s\nupstream_usage=in:%d,out:%d", errBody, in.UpstreamInTok, in.UpstreamOutTok)
	}
	log := &ContentModerationLog{
		RequestID:       in.RequestID,
		UserID:          userID,
		UserEmail:       in.UserEmail,
		APIKeyID:        apiKeyID,
		APIKeyName:      in.APIKeyName,
		GroupID:         cloneInt64Ptr(in.GroupID),
		GroupName:       in.GroupName,
		Endpoint:        in.Endpoint,
		Provider:        "openai",
		Model:           in.Model,
		Mode:            "post_upstream",
		Action:          ContentModerationActionCyberPolicy,
		Flagged:         true,
		HighestCategory: "cyber_policy",
		HighestScore:    1.0,
		Error:           trimRunes(redactContentModerationSecrets(errBody), maxModerationExcerptRunes*4),
		CreatedAt:       time.Now(),
	}
	// 开关开时 cyber_policy 不参与封号计数：当次不判定（此处跳过），
	// 历史行由 CountFlaggedByUserSince 的 excludeCyberPolicy 排除。
	autoBanned := false
	if !cfg.CyberPolicyExcludeFromBanCount {
		autoBanned = s.applyFlaggedAccountSideEffects(ctx, cfg, log)
	}
	log.EmailSent = false
	logPersisted := true
	if err := s.repo.CreateLog(ctx, log); err != nil {
		logPersisted = false
		slog.Warn("content_moderation.cyber_create_log_failed", "user_id", in.UserID, "error", err)
	}
	emailSent := false
	if s.emailService != nil && strings.TrimSpace(log.UserEmail) != "" {
		if err := s.sendCyberPolicyEmail(ctx, log); err != nil {
			slog.Warn("content_moderation.cyber_email_failed", "user_id", in.UserID, "error", err)
		} else {
			emailSent = true
		}
		if autoBanned {
			if err := s.sendAccountDisabledEmail(ctx, cfg, log); err != nil {
				slog.Warn("content_moderation.cyber_ban_email_failed", "user_id", in.UserID, "error", err)
			} else {
				emailSent = true
			}
		}
	}
	if logPersisted && emailSent {
		if err := s.repo.UpdateLogEmailSent(ctx, log.ID, true); err != nil {
			slog.Warn("content_moderation.cyber_update_email_sent_failed", "log_id", log.ID, "error", err)
		}
	}
}

func (s *ContentModerationService) sendCyberPolicyEmail(ctx context.Context, log *ContentModerationLog) error {
	siteName := s.siteName(ctx)
	if s.emailService.notificationEmailService != nil {
		variables := map[string]string{
			"triggered_at":     log.CreatedAt.UTC().Format(time.RFC3339),
			"model":            defaultContentModerationString(log.Model, "-"),
			"group_name":       defaultContentModerationString(log.GroupName, "-"),
			"upstream_message": defaultContentModerationString(log.Error, "-"),
		}
		err := s.emailService.notificationEmailService.Send(ctx, NotificationEmailSendInput{
			Event:          NotificationEmailEventCyberPolicyNotice,
			RecipientEmail: log.UserEmail,
			RecipientName:  emailRecipientName(log.UserEmail),
			UserID:         contentModerationEmailUserID(log),
			SourceType:     "content_moderation",
			SourceID:       contentModerationEmailSourceID(log),
			Variables:      variables,
		})
		if err == nil {
			return nil
		}
		if !shouldFallbackNotificationEmail(err) {
			return err
		}
		slog.Warn("template cyber policy email failed; falling back", "err", err.Error())
	}
	subject := fmt.Sprintf("[%s] 网络安全策略拦截 / Cyber Policy Notice", sanitizeEmailHeader(siteName))
	return s.emailService.SendEmail(ctx, log.UserEmail, subject, buildCyberPolicyNoticeEmailBody(siteName, log))
}
