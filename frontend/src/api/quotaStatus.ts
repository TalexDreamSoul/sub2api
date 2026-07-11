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

export interface QuotaStatusAccount {
  name: string
  platform: string
  status: 'available' | 'limited' | 'unavailable'
  dimensions: QuotaStatusDimension[]
}

export interface QuotaStatusGroup {
  name: string
  platform: string
  accounts: QuotaStatusAccount[]
}

export interface QuotaStatusSnapshot {
  enabled: boolean
  title: string
  description: string
  updated_at: string
  groups: QuotaStatusGroup[]
}

export async function getQuotaStatus(): Promise<QuotaStatusSnapshot> {
  const { data } = await apiClient.get<QuotaStatusSnapshot>('/quota-status')
  return data
}

export default { getQuotaStatus }
