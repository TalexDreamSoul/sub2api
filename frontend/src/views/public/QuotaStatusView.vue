<template>
  <div class="min-h-[100dvh] bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white">
    <header class="border-b border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
      <div class="mx-auto flex h-16 max-w-7xl items-center justify-between px-4 sm:px-6 lg:px-8">
        <router-link :to="homePath" class="flex min-w-0 items-center gap-3">
          <img :src="appStore.siteLogo || '/logo.png'" alt="" class="h-9 w-9 rounded-lg object-contain" />
          <span class="truncate text-base font-semibold text-gray-900 dark:text-white">{{ appStore.siteName }}</span>
        </router-link>
        <div class="flex items-center gap-2">
          <LocaleSwitcher />
          <router-link :to="homePath" class="btn btn-secondary">
            {{ authStore.isAuthenticated ? t('quotaStatus.backToDashboard') : t('quotaStatus.signIn') }}
          </router-link>
        </div>
      </div>
    </header>

    <main class="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8 lg:py-12">
      <div class="flex flex-col gap-5 border-b border-gray-200 pb-7 dark:border-dark-700 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h1 class="text-3xl font-bold text-gray-950 dark:text-white sm:text-4xl">
            {{ snapshot?.title || t('quotaStatus.title') }}
          </h1>
          <p class="mt-2 max-w-2xl text-sm leading-6 text-gray-600 dark:text-gray-400 sm:text-base">
            {{ snapshot?.description || t('quotaStatus.description') }}
          </p>
        </div>
        <div class="flex items-center gap-3">
          <span v-if="snapshot?.updated_at" class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('quotaStatus.updatedAt', { time: formatUpdatedAt(snapshot.updated_at) }) }}
          </span>
          <button class="btn btn-secondary btn-icon" :disabled="loading" :title="t('common.refresh')" @click="load">
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </div>

      <div v-if="loading && !snapshot" class="mt-8 space-y-5" aria-live="polite">
        <div class="grid gap-4 sm:grid-cols-3">
          <div v-for="index in 3" :key="index" class="h-24 animate-pulse rounded-lg bg-gray-200 dark:bg-dark-800"></div>
        </div>
        <div v-for="index in 2" :key="`group-${index}`" class="h-64 animate-pulse rounded-lg bg-gray-200 dark:bg-dark-800"></div>
      </div>

      <div v-else-if="snapshot && !snapshot.enabled" class="py-20">
        <EmptyState :title="t('quotaStatus.disabled.title')" :description="t('quotaStatus.disabled.description')" />
      </div>

      <div v-else-if="snapshot && snapshot.groups.length === 0" class="py-20">
        <EmptyState :title="t('quotaStatus.empty.title')" :description="t('quotaStatus.empty.description')" />
      </div>

      <template v-else-if="snapshot">
        <section class="mt-8 grid gap-4 sm:grid-cols-3" :aria-label="t('quotaStatus.summary.title')">
          <div class="rounded-lg border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
            <div class="text-sm text-gray-500 dark:text-gray-400">{{ t('quotaStatus.summary.total') }}</div>
            <div class="mt-2 font-mono text-3xl font-semibold text-gray-950 dark:text-white">{{ summary.total }}</div>
          </div>
          <div class="rounded-lg border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
            <div class="text-sm text-gray-500 dark:text-gray-400">{{ t('quotaStatus.summary.available') }}</div>
            <div class="mt-2 font-mono text-3xl font-semibold text-emerald-600 dark:text-emerald-400">{{ summary.available }}</div>
          </div>
          <div class="rounded-lg border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
            <div class="text-sm text-gray-500 dark:text-gray-400">{{ t('quotaStatus.summary.attention') }}</div>
            <div class="mt-2 font-mono text-3xl font-semibold text-amber-600 dark:text-amber-400">{{ summary.attention }}</div>
          </div>
        </section>

        <section class="mt-8 space-y-6">
          <article v-for="group in snapshot.groups" :key="`${group.platform}-${group.name}`" class="overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
            <header class="flex flex-col gap-3 border-b border-gray-100 px-5 py-4 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
              <div class="flex flex-wrap items-center gap-3">
                <h2 class="text-lg font-semibold text-gray-950 dark:text-white">{{ group.name }}</h2>
                <span class="rounded-md bg-gray-100 px-2 py-1 text-xs font-medium text-gray-600 dark:bg-dark-800 dark:text-gray-300">
                  {{ platformLabel(group.platform) }}
                </span>
              </div>
              <div class="text-sm text-gray-500 dark:text-gray-400">
                {{ t('quotaStatus.groupSummary', { available: availableCount(group.accounts), total: group.accounts.length }) }}
              </div>
            </header>

            <div v-if="group.accounts.length === 0" class="px-5 py-10 text-center text-sm text-gray-500 dark:text-gray-400">
              {{ t('quotaStatus.emptyGroup') }}
            </div>
            <div v-else class="divide-y divide-gray-100 dark:divide-dark-700">
              <div v-for="account in group.accounts" :key="account.name" class="grid gap-4 px-5 py-5 lg:grid-cols-[220px_minmax(0,1fr)]">
                <div class="min-w-0">
                  <div class="truncate text-sm font-semibold text-gray-950 dark:text-white">{{ account.name }}</div>
                  <div class="mt-2 inline-flex items-center gap-2 text-xs font-medium" :class="statusTextClass(account.status)">
                    <span class="h-2 w-2 rounded-full" :class="statusDotClass(account.status)" aria-hidden="true"></span>
                    {{ statusLabel(account.status) }}
                  </div>
                </div>

                <div v-if="account.dimensions.length === 0" class="flex min-h-16 items-center rounded-lg bg-gray-50 px-4 text-sm text-gray-500 dark:bg-dark-800 dark:text-gray-400">
                  {{ t('quotaStatus.noQuotaDetails') }}
                </div>
                <div v-else class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                  <div v-for="dimension in account.dimensions" :key="dimension.key" class="rounded-lg bg-gray-50 p-4 dark:bg-dark-800">
                    <div class="flex items-start justify-between gap-3">
                      <span class="text-xs font-medium text-gray-600 dark:text-gray-300">{{ dimensionLabel(dimension) }}</span>
                      <span class="font-mono text-xs font-semibold text-gray-900 dark:text-white">{{ formatPercent(dimension.utilization) }}</span>
                    </div>
                    <div class="mt-3 h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                      <div class="h-full rounded-full transition-[width] duration-300" :class="utilizationClass(dimension.utilization)" :style="{ width: `${clamp(dimension.utilization)}%` }"></div>
                    </div>
                    <div class="mt-2 flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500 dark:text-gray-400">
                      <span>{{ formatDimensionValue(dimension) }}</span>
                      <span v-if="dimension.resets_at">{{ t('quotaStatus.resetsAt', { time: formatResetAt(dimension.resets_at) }) }}</span>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </article>
        </section>
      </template>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import quotaStatusAPI, { type QuotaStatusAccount, type QuotaStatusDimension, type QuotaStatusSnapshot } from '@/api/quotaStatus'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t, locale } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const snapshot = ref<QuotaStatusSnapshot | null>(null)
const loading = ref(false)
let refreshTimer: ReturnType<typeof setInterval> | null = null

const homePath = computed(() => {
  if (!authStore.isAuthenticated) return '/login'
  return authStore.isAdmin ? '/admin/dashboard' : '/dashboard'
})

const summary = computed(() => {
  const accounts = snapshot.value?.groups.flatMap(group => group.accounts) || []
  const available = accounts.filter(account => account.status === 'available').length
  return { total: accounts.length, available, attention: accounts.length - available }
})

async function load() {
  loading.value = true
  try {
    snapshot.value = await quotaStatusAPI.getQuotaStatus()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('quotaStatus.loadError')))
  } finally {
    loading.value = false
  }
}

function availableCount(accounts: QuotaStatusAccount[]): number {
  return accounts.filter(account => account.status === 'available').length
}

function platformLabel(platform: string): string {
  return t(`admin.groups.platforms.${platform}`, platform)
}

function statusLabel(status: QuotaStatusAccount['status']): string {
  return t(`quotaStatus.status.${status}`)
}

function statusTextClass(status: QuotaStatusAccount['status']): string {
  if (status === 'available') return 'text-emerald-700 dark:text-emerald-400'
  if (status === 'limited') return 'text-amber-700 dark:text-amber-400'
  return 'text-red-700 dark:text-red-400'
}

function statusDotClass(status: QuotaStatusAccount['status']): string {
  if (status === 'available') return 'bg-emerald-500'
  if (status === 'limited') return 'bg-amber-500'
  return 'bg-red-500'
}

function clamp(value?: number): number {
  if (value == null || !Number.isFinite(value)) return 0
  return Math.min(100, Math.max(0, value))
}

function utilizationClass(value?: number): string {
  const normalized = clamp(value)
  if (normalized >= 90) return 'bg-red-500'
  if (normalized >= 75) return 'bg-amber-500'
  return 'bg-emerald-500'
}

function formatPercent(value?: number): string {
  return value == null ? t('quotaStatus.unknown') : `${Math.round(clamp(value))}%`
}

function dimensionLabel(dimension: QuotaStatusDimension): string {
  const translated = t(`quotaStatus.dimensions.${dimension.key}`)
  return translated === `quotaStatus.dimensions.${dimension.key}` ? dimension.label : translated
}

function compactNumber(value: number): string {
  return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1, notation: value >= 10000 ? 'compact' : 'standard' }).format(value)
}

function formatDimensionValue(dimension: QuotaStatusDimension): string {
  if (dimension.used == null || dimension.limit == null) return t('quotaStatus.upstreamSample')
  if (dimension.unit === 'USD') return `$${compactNumber(dimension.used)} / $${compactNumber(dimension.limit)}`
  return `${compactNumber(dimension.used)} / ${compactNumber(dimension.limit)}`
}

function formatUpdatedAt(value: string): string {
  return new Intl.DateTimeFormat(locale.value, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(value))
}

function formatResetAt(value: string): string {
  return new Intl.DateTimeFormat(locale.value, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(value))
}

onMounted(() => {
  void load()
  refreshTimer = setInterval(() => {
    if (document.visibilityState === 'visible') void load()
  }, 60_000)
})

onBeforeUnmount(() => {
  if (refreshTimer) clearInterval(refreshTimer)
})
</script>
