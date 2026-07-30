import { apiClient } from '../client'

export interface QuotaStatusAccountConfig {
  account_id: number
  display_name: string
  show_name: boolean
}

export interface QuotaStatusGroupConfig {
  id: string
  group_id: number
  display_name: string
  accounts: QuotaStatusAccountConfig[]
}

export type QuotaStatusAccessMode = 'public' | 'authenticated' | 'group_scoped'

export interface QuotaStatusDisplayConfig {
  show_rate_multiplier: boolean
  show_model_distribution: boolean
  show_daily_curve: boolean
  show_scheduling_quota: boolean
  curve_days: number
}

export interface QuotaStatusConfig {
  enabled: boolean
  title: string
  description: string
  access_mode: QuotaStatusAccessMode
  display: QuotaStatusDisplayConfig
  groups: QuotaStatusGroupConfig[]
}

export async function getConfig(): Promise<QuotaStatusConfig> {
  const { data } = await apiClient.get<QuotaStatusConfig>('/admin/quota-status')
  return data
}

export async function updateConfig(config: QuotaStatusConfig): Promise<QuotaStatusConfig> {
  const { data } = await apiClient.put<QuotaStatusConfig>('/admin/quota-status', config)
  return data
}

export default { getConfig, updateConfig }
