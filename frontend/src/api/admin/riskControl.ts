import { apiClient } from '../client'

export type ModerationMode = 'off' | 'observe' | 'pre_block'
export type KeywordBlockingMode = 'keyword_only' | 'keyword_and_api' | 'api_only'
export type ContentModerationModelFilterType = 'all' | 'include' | 'exclude'

export interface ContentModerationModelFilter {
  type: ContentModerationModelFilterType
  models: string[]
}

export type ContentModerationDecisionRuleType = 'any' | 'all' | 'n_of_m' | 'weight_threshold'
export type ContentModerationKeywordMatchType = 'contains' | 'regex'

export interface ContentModerationAuditModelConfig {
  id: string
  name: string
  enabled: boolean
  protocol: 'openai_compatible' | 'internal_group'
  base_url: string
  api_key?: string
  model: string
  group_id?: number | null
  group_name?: string
  internal_api_key_id?: number | null
  temperature: number
  timeout_ms: number
  prompt_template: string
  weight: number
}

export interface ContentModerationDecisionRule {
  type: ContentModerationDecisionRuleType
  required_count: number
  weight_threshold: number
}

export interface ContentModerationSelfUnbanConfig {
  enabled: boolean
  window_minutes: number
  max_attempts: number
  second_attempt_wait_minutes: number
}

export type ContentModerationCyberuseUserScopeMode = 'all' | 'include' | 'exclude'

export interface ContentModerationCyberuseUserScope {
  mode: ContentModerationCyberuseUserScopeMode
  user_ids: number[]
}

export interface ContentModerationCyberuseConfig {
  enabled: boolean
  emit_to_client: boolean
  error_code: string
  message: string
  include_request_id: boolean
  audit_metadata_enabled: boolean
  announcement_enabled: boolean
  announcement_title: string
  announcement_content: string
  user_scope: ContentModerationCyberuseUserScope
}

export interface ContentModerationKeywordRule {
  id: string
  group: string
  match_type: ContentModerationKeywordMatchType
  patterns: string[]
  fields: string[]
  whitelist: boolean
  priority: number
  actions: string[]
  enabled: boolean
  ignore_case: boolean
}

export interface ContentModerationKeywordHit {
  rule_id: string
  group: string
  match_type: ContentModerationKeywordMatchType
  keyword: string
  matched_text: string
  field: string
  action: string
  whitelist: boolean
  priority: number
}

export interface ContentModerationBanStatus {
  user_id: number
  banned: boolean
  reason: string
  triggered_at?: string
  banned_until?: string
  remaining_seconds: number
  self_unban_available: boolean
  self_unban_attempts_used: number
  self_unban_max_attempts: number
  self_unban_wait_seconds: number
  self_unban_window_reset_at?: string
}

export interface ContentModerationSelfUnbanResponse {
  user_id: number
  unbanned: boolean
  status: string
  attempts_used: number
  max_attempts: number
  wait_seconds: number
  window_reset_at?: string
  message: string
}

export interface ContentModerationConfig {
  enabled: boolean
  mode: ModerationMode
  base_url: string
  model: string
  proxy_id: number | null
  api_key_configured: boolean
  api_key_masked: string
  api_key_count: number
  api_key_masks: string[]
  api_key_statuses: ContentModerationAPIKeyStatus[]
  timeout_ms: number
  sample_rate: number
  all_groups: boolean
  group_ids: number[]
  record_non_hits: boolean
  thresholds: Record<string, number>
  worker_count: number
  queue_size: number
  block_status: number
  block_message: string
  email_on_hit: boolean
  auto_ban_enabled: boolean
  ban_threshold: number
  ban_duration_minutes: number
  violation_window_hours: number
  retry_count: number
  hit_retention_days: number
  non_hit_retention_days: number
  context_retention_days: number
  pre_hash_check_enabled: boolean
  blocked_keywords: string[]
  keyword_blocking_mode: KeywordBlockingMode
  keyword_rules: ContentModerationKeywordRule[]
  model_filter: ContentModerationModelFilter
  audit_models: ContentModerationAuditModelConfig[]
  decision_rule: ContentModerationDecisionRule
  self_unban: ContentModerationSelfUnbanConfig
  risk_weight_enabled: boolean
  flagged_weight: number
  ban_weight: number
  manual_suspicious_weight: number
  decay_half_life_days: number
  max_sample_rate: number
  ban_threshold_weight_step: number
  min_effective_ban_threshold: number
  background_review_enabled: boolean
  background_review_batch_size: number
  background_review_max_attempts: number
  background_review_retry_backoff_seconds: number
  context_capture_enabled: boolean
  context_max_bytes: number
  cyberuse_response: ContentModerationCyberuseConfig
  cyber_policy_exclude_from_ban_count: boolean
}

export type ContentModerationAPIKeyStatusValue = 'unknown' | 'ok' | 'error' | 'frozen'

export interface ContentModerationAPIKeyStatus {
  index: number
  key_hash: string
  masked: string
  status: ContentModerationAPIKeyStatusValue
  failure_count: number
  success_count: number
  last_error: string
  last_checked_at?: string
  frozen_until?: string
  last_latency_ms: number
  last_http_status: number
  last_tested: boolean
  configured: boolean
}

export interface TestContentModerationAPIKeysPayload {
  api_keys?: string[]
  base_url?: string
  model?: string
  timeout_ms?: number
  // null/undefined 沿用已保存配置的代理；0 强制直连；>0 指定代理
  proxy_id?: number
  prompt?: string
  images?: string[]
}

export interface TestContentModerationAPIKeysResponse {
  items: ContentModerationAPIKeyStatus[]
  audit_result?: ContentModerationTestAuditResult
  image_count: number
}

export interface ContentModerationTestAuditResult {
  flagged: boolean
  highest_category: string
  highest_score: number
  composite_score: number
  category_scores: Record<string, number>
  thresholds: Record<string, number>
}

export interface UpdateContentModerationConfig {
  enabled?: boolean
  mode?: ModerationMode
  base_url?: string
  model?: string
  // undefined 不修改；0 清除（直连）；>0 指定代理
  proxy_id?: number
  api_key?: string
  api_keys?: string[]
  api_keys_mode?: 'append' | 'replace'
  delete_api_key_hashes?: string[]
  clear_api_key?: boolean
  timeout_ms?: number
  sample_rate?: number
  all_groups?: boolean
  group_ids?: number[]
  record_non_hits?: boolean
  thresholds?: Record<string, number>
  worker_count?: number
  queue_size?: number
  block_status?: number
  block_message?: string
  email_on_hit?: boolean
  auto_ban_enabled?: boolean
  ban_threshold?: number
  ban_duration_minutes?: number
  violation_window_hours?: number
  retry_count?: number
  hit_retention_days?: number
  non_hit_retention_days?: number
  context_retention_days?: number
  pre_hash_check_enabled?: boolean
  blocked_keywords?: string[]
  keyword_blocking_mode?: KeywordBlockingMode
  keyword_rules?: ContentModerationKeywordRule[]
  model_filter?: ContentModerationModelFilter
  audit_models?: ContentModerationAuditModelConfig[]
  decision_rule?: ContentModerationDecisionRule
  self_unban?: ContentModerationSelfUnbanConfig
  risk_weight_enabled?: boolean
  flagged_weight?: number
  ban_weight?: number
  manual_suspicious_weight?: number
  decay_half_life_days?: number
  max_sample_rate?: number
  ban_threshold_weight_step?: number
  min_effective_ban_threshold?: number
  background_review_enabled?: boolean
  background_review_batch_size?: number
  background_review_max_attempts?: number
  background_review_retry_backoff_seconds?: number
  context_capture_enabled?: boolean
  context_max_bytes?: number
  cyberuse_response?: ContentModerationCyberuseConfig
  cyber_policy_exclude_from_ban_count?: boolean
}

export interface ContentModerationAuditModelRuntimeStatus {
  model_id: string
  name: string
  model: string
  status: 'unknown' | 'ok' | 'error'
  success_count: number
  failure_count: number
  flagged_count: number
  disagreement_count: number
  total_calls: number
  avg_latency_ms: number
  last_latency_ms: number
  last_http_status: number
  last_error: string
  last_checked_at?: string}

export interface ContentModerationRuntimeStatus {
  enabled: boolean
  risk_control_enabled: boolean
  mode: ModerationMode
  worker_count: number
  max_workers: number
  active_workers: number
  idle_workers: number
  queue_size: number
  queue_length: number
  queue_usage_percent: number
  enqueued: number
  dropped: number
  processed: number
  errors: number
  pre_block_active: number
  pre_block_checked: number
  pre_block_allowed: number
  pre_block_blocked: number
  pre_block_errors: number
  pre_block_avg_latency_ms: number
  pre_block_api_key_active: number
  pre_block_api_key_available_count: number
  pre_block_api_key_total_calls: number
  pre_block_api_key_loads: ContentModerationAPIKeyLoad[]
  api_key_statuses: ContentModerationAPIKeyStatus[]
  audit_model_statuses: ContentModerationAuditModelRuntimeStatus[]
  flagged_hash_count: number
  pending_context_count: number
  processing_context_count: number
  failed_context_count: number
  last_background_review_at?: string
  context_total_count: number
  context_total_bytes: number
  context_avg_bytes: number
  context_drop_count: number
  context_capture_error: string
  last_context_capture_error_at?: string
  last_cleanup_at?: string
  last_cleanup_deleted_hit: number
  last_cleanup_deleted_non_hit: number
  last_cleanup_deleted_context: number
}

export interface ContentModerationAPIKeyLoad {
  index: number
  key_hash: string
  masked: string
  status: ContentModerationAPIKeyStatusValue
  active: number
  total: number
  success: number
  errors: number
  avg_latency_ms: number
  last_latency_ms: number
  last_http_status: number
}

export interface ContentModerationLog {
  id: number
  request_id: string
  user_id: number | null
  user_email: string
  api_key_id: number | null
  api_key_name: string
  group_id: number | null
  group_name: string
  endpoint: string
  provider: string
  model: string
  mode: string
  action: string
  flagged: boolean
  highest_category: string
  highest_score: number
  matched_keyword: string
  category_scores: Record<string, number>
  threshold_snapshot: Record<string, number>
  input_excerpt: string
  keyword_hits?: ContentModerationKeywordHit[]
  audit_context?: unknown
  context_id?: number | null
  upstream_latency_ms: number | null
  error: string
  violation_count: number
  auto_banned: boolean
  email_sent: boolean
  risk_weight_snapshot: number
  effective_sample_rate: number
  effective_ban_threshold: number
  risk_event_source: string
  review_stage: string
  user_status: string
  queue_delay_ms: number | null
  created_at: string
}

export interface ListContentModerationLogsParams {
  page?: number
  page_size?: number
  result?: string
  group_id?: number
  endpoint?: string
  search?: string
  from?: string
  to?: string
}

export interface ContentModerationLogsResponse {
  items: ContentModerationLog[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface ContentModerationUnbanUserResponse {
  user_id: number
  status: string
}

export interface DeleteFlaggedHashResponse {
  input_hash: string
  deleted: boolean
}

export interface ClearFlaggedHashesResponse {
  deleted: number
}

export interface ContentModerationUserRiskProfile {
  user_id: number
  current_weight: number
  effective_weight: number
  manual_suspicious: boolean
  cumulative_flagged_count: number
  cumulative_ban_count: number
  last_event_at?: string
  last_decay_at?: string
  created_at: string
  updated_at: string
}

export interface ContentModerationUserRiskEvent {
  id: number
  user_id: number
  event_type: string
  source: string
  review_stage: string
  weight_delta: number
  effective_weight_before: number
  effective_weight_after: number
  reason: string
  log_id?: number
  context_id?: number
  created_at: string
}

export interface ContentModerationContext {
  id: number
  request_id: string
  user_id?: number | null
  user_email: string
  api_key_id?: number | null
  api_key_name: string
  group_id?: number | null
  group_name: string
  endpoint: string
  provider: string
  model: string
  protocol: string
  input_hash: string
  context_hash: string
  plain_context?: string
  context_summary: string
  context_bytes: number
  status: string
  review_stage: string
  review_attempts: number
  max_review_attempts: number
  next_review_at: string
  processing_started_at?: string
  reviewed_at?: string
  last_review_log_id?: number
  last_review_flagged: boolean
  last_review_error: string
  last_capture_error: string
  created_at: string
  updated_at: string
}

export interface ContentModerationUserRiskDetail {
  profile: ContentModerationUserRiskProfile | null
  events: ContentModerationUserRiskEvent[]
  ban_status: ContentModerationBanStatus
  effective_sample_rate: number
  effective_ban_threshold: number
}

export type RequestRiskControlMode = 'off' | 'observe' | 'enforce'

export interface RequestRiskControlConfig {
  enabled: boolean
  mode: RequestRiskControlMode
  windows_enhanced: boolean
  denied_timezones: string[]
  chinese_high_threshold: number
  event_retention_days: number
  capture_raw_headers: boolean
  ua_ban_scope: string
  session_ban_ttl_seconds: number
  ua_ban_ttl_seconds: number
}

export type UpdateRequestRiskControlConfig = Partial<RequestRiskControlConfig>

export interface RequestRiskEvent {
  id: number
  created_at: string
  expires_at: string
  user_id?: number | null
  api_key_id?: number | null
  account_id?: number | null
  request_id: string
  session_id: string
  session_id_hash: string
  user_agent: string
  user_agent_hash: string
  inference_geo: string
  timezone: string
  platform: string
  language_signals: Record<string, unknown>
  chinese_intensity: number
  matched_rules: string[]
  action: string
  reason_code: string
  raw_headers?: Record<string, string[]>
  raw_headers_json?: string
  x_foo_raw: string
  request_path: string
  model: string
}

export interface ListRequestRiskEventsParams {
  page?: number
  page_size?: number
  action?: string
  rule?: string
  q?: string
  api_key_id?: number
  user_id?: number
  from?: string
  to?: string
}

export interface RequestRiskEventsResponse {
  items: RequestRiskEvent[]
  total: number
  page: number
  page_size: number
  pages: number
}

export async function getConfig(): Promise<ContentModerationConfig> {
  const { data } = await apiClient.get<ContentModerationConfig>('/admin/risk-control/config')
  return data
}

export async function updateConfig(
  payload: UpdateContentModerationConfig
): Promise<ContentModerationConfig> {
  const { data } = await apiClient.put<ContentModerationConfig>('/admin/risk-control/config', payload)
  return data
}

export async function getStatus(): Promise<ContentModerationRuntimeStatus> {
  const { data } = await apiClient.get<ContentModerationRuntimeStatus>('/admin/risk-control/status')
  return data
}

export async function testAPIKeys(
  payload: TestContentModerationAPIKeysPayload = {}
): Promise<TestContentModerationAPIKeysResponse> {
  const { data } = await apiClient.post<TestContentModerationAPIKeysResponse>('/admin/risk-control/api-keys/test', payload)
  return data
}

export async function listLogs(
  params: ListContentModerationLogsParams = {}
): Promise<ContentModerationLogsResponse> {
  const { data } = await apiClient.get<ContentModerationLogsResponse>('/admin/risk-control/logs', {
    params,
  })
  return data
}

export async function getUserBanStatus(userID: number): Promise<ContentModerationBanStatus> {
  const { data } = await apiClient.get<ContentModerationBanStatus>(
    `/admin/risk-control/users/${userID}/ban-status`
  )
  return data
}

export async function getUserRiskProfile(userID: number): Promise<ContentModerationUserRiskDetail> {
  const { data } = await apiClient.get<ContentModerationUserRiskDetail>(
    `/admin/risk-control/users/${userID}/profile`
  )
  return data
}

export async function setUserSuspicion(
  userID: number,
  payload: { suspicious: boolean; reason?: string }
): Promise<ContentModerationUserRiskDetail> {
  const { data } = await apiClient.post<ContentModerationUserRiskDetail>(
    `/admin/risk-control/users/${userID}/suspicion`,
    payload
  )
  return data
}

export async function listUserContexts(userID: number): Promise<ContentModerationContext[]> {
  const { data } = await apiClient.get<ContentModerationContext[]>(
    `/admin/risk-control/users/${userID}/contexts`
  )
  return data
}

export async function getContextDetail(contextID: number): Promise<ContentModerationContext> {
  const { data } = await apiClient.get<ContentModerationContext>(
    `/admin/risk-control/contexts/${contextID}`
  )
  return data
}

export async function getRequestRiskConfig(): Promise<RequestRiskControlConfig> {
  const { data } = await apiClient.get<RequestRiskControlConfig>('/admin/risk-control/request-risk/config')
  return data
}

export async function updateRequestRiskConfig(
  payload: UpdateRequestRiskControlConfig
): Promise<RequestRiskControlConfig> {
  const { data } = await apiClient.put<RequestRiskControlConfig>('/admin/risk-control/request-risk/config', payload)
  return data
}

export async function listRequestRiskEvents(
  params: ListRequestRiskEventsParams = {}
): Promise<RequestRiskEventsResponse> {
  const { data } = await apiClient.get<RequestRiskEventsResponse>('/admin/risk-control/request-risk/events', {
    params,
  })
  return data
}

export async function getRequestRiskEvent(id: number): Promise<RequestRiskEvent> {
  const { data } = await apiClient.get<RequestRiskEvent>(`/admin/risk-control/request-risk/events/${id}`)
  return data
}

export async function selfUnbanUser(userID: number): Promise<ContentModerationSelfUnbanResponse> {
  const { data } = await apiClient.post<ContentModerationSelfUnbanResponse>(
    `/admin/risk-control/users/${userID}/self-unban`
  )
  return data
}

export async function unbanUser(userID: number): Promise<ContentModerationUnbanUserResponse> {
  const { data } = await apiClient.post<ContentModerationUnbanUserResponse>(
    `/admin/risk-control/users/${userID}/unban`
  )
  return data
}

export async function deleteFlaggedHash(inputHash: string): Promise<DeleteFlaggedHashResponse> {
  const { data } = await apiClient.delete<DeleteFlaggedHashResponse>('/admin/risk-control/hashes', {
    data: { input_hash: inputHash },
  })
  return data
}

export async function clearFlaggedHashes(): Promise<ClearFlaggedHashesResponse> {
  const { data } = await apiClient.delete<ClearFlaggedHashesResponse>('/admin/risk-control/hashes/all')
  return data
}

export const riskControlAPI = {
  getConfig,
  updateConfig,
  getStatus,
  testAPIKeys,
  listLogs,
  getUserBanStatus,
  getUserRiskProfile,
  setUserSuspicion,
  listUserContexts,
  getContextDetail,
  getRequestRiskConfig,
  updateRequestRiskConfig,
  listRequestRiskEvents,
  getRequestRiskEvent,
  selfUnbanUser,
  unbanUser,
  deleteFlaggedHash,
  clearFlaggedHashes,
}

export default riskControlAPI
