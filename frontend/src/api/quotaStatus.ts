import { apiClient } from './client'

export interface QuotaStatusDimension {
  key: string
  label: string
  used?: number
  limit?: number
  utilization?: number
  resets_at?: string
  unit: 'USD' | 'percent' | 'requests' | 'tokens' | string
}

export interface QuotaStatusCurvePoint {
  date: string
  label: string
  cost: number
  requests: number
  tokens: number
}

export interface QuotaStatusModelStat {
  model: string
  requests: number
  tokens: number
  actual_cost: number
}

export interface QuotaStatusAccount {
  name: string
  platform: string
  type: string
  status: 'available' | 'limited' | 'unavailable'
  rate_multiplier: number
  schedulable: boolean
  priority: number
  dimensions: QuotaStatusDimension[]
  daily_curve?: QuotaStatusCurvePoint[]
  model_distribution?: QuotaStatusModelStat[]
}

export interface QuotaStatusGroup {
  name: string
  platform: string
  accounts: QuotaStatusAccount[]
}

export type QuotaStatusAccessMode = 'public' | 'authenticated' | 'group_scoped'

export interface QuotaStatusDisplayConfig {
  show_rate_multiplier: boolean
  show_model_distribution: boolean
  show_daily_curve: boolean
  show_scheduling_quota: boolean
  curve_days: number
}

export interface QuotaStatusSnapshot {
  enabled: boolean
  title: string
  description: string
  access_mode: QuotaStatusAccessMode
  display: QuotaStatusDisplayConfig
  updated_at: string
  groups: QuotaStatusGroup[]
}

export async function getQuotaStatus(): Promise<QuotaStatusSnapshot> {
  const { data } = await apiClient.get<QuotaStatusSnapshot>('/quota-status')
  return data
}

export default { getQuotaStatus }
