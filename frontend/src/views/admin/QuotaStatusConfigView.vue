<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <div class="card">
        <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700 sm:px-6">
          <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('admin.quotaStatus.basic.title') }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t('admin.quotaStatus.basic.description') }}
              </p>
            </div>
            <div class="flex items-center gap-3">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('admin.quotaStatus.basic.enabled') }}
              </span>
              <Toggle v-model="form.enabled" />
            </div>
          </div>
        </div>

        <div class="grid gap-5 p-5 sm:grid-cols-2 sm:p-6">
          <div>
            <label class="input-label">{{ t('admin.quotaStatus.basic.pageTitle') }}</label>
            <input v-model="form.title" class="input" maxlength="100" />
          </div>
          <div>
            <label class="input-label">{{ t('admin.quotaStatus.basic.publicUrl') }}</label>
            <div class="flex gap-2">
              <input :value="publicURL" class="input" readonly />
              <a :href="publicURL" target="_blank" rel="noopener noreferrer" class="btn btn-secondary btn-icon" :title="t('admin.quotaStatus.basic.openPage')">
                <Icon name="externalLink" size="md" />
              </a>
            </div>
          </div>
          <div class="sm:col-span-2">
            <label class="input-label">{{ t('admin.quotaStatus.basic.pageDescription') }}</label>
            <textarea v-model="form.description" class="input" rows="2" maxlength="500"></textarea>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700 sm:px-6">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('admin.quotaStatus.groups.title') }}
          </h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.quotaStatus.groups.description') }}
          </p>
        </div>

        <div class="space-y-4 p-5 sm:p-6">
          <div class="flex flex-col gap-3 sm:flex-row">
            <Select
              v-model="newGroupID"
              :options="availableGroupOptions"
              :placeholder="t('admin.quotaStatus.groups.selectPlaceholder')"
              searchable
              class="min-w-0 flex-1"
            />
            <button class="btn btn-secondary whitespace-nowrap" :disabled="!newGroupID || addingGroup" @click="addGroup">
              <Icon name="plus" size="md" class="mr-2" />
              {{ t('admin.quotaStatus.groups.add') }}
            </button>
          </div>

          <div v-if="loading" class="space-y-3" aria-live="polite">
            <div v-for="index in 3" :key="index" class="h-28 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-800"></div>
          </div>

          <EmptyState
            v-else-if="groupRows.length === 0"
            :title="t('admin.quotaStatus.groups.emptyTitle')"
            :description="t('admin.quotaStatus.groups.emptyDescription')"
          />

          <section
            v-for="(row, groupIndex) in groupRows"
            v-else
            :key="row.key"
            class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-700"
          >
            <header class="flex flex-col gap-4 bg-gray-50 px-4 py-4 dark:bg-dark-800 sm:flex-row sm:items-center sm:justify-between">
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-2">
                  <h3 class="font-semibold text-gray-900 dark:text-white">{{ row.group.name }}</h3>
                  <span class="rounded-md bg-white px-2 py-0.5 text-xs font-medium text-gray-600 ring-1 ring-inset ring-gray-200 dark:bg-dark-700 dark:text-gray-300 dark:ring-dark-600">
                    {{ platformLabel(row.group.platform) }}
                  </span>
                  <span class="text-xs text-gray-500 dark:text-gray-400">
                    {{ t('admin.quotaStatus.groups.selectedCount', { selected: selectedCount(row), total: row.accounts.length }) }}
                  </span>
                </div>
                <input
                  v-model="row.displayName"
                  class="input mt-3 max-w-md"
                  :placeholder="t('admin.quotaStatus.groups.displayNamePlaceholder', { name: row.group.name })"
                  maxlength="100"
                />
              </div>
              <div class="flex items-center gap-2 self-end sm:self-auto">
                <button class="btn btn-ghost btn-icon" :title="row.expanded ? t('common.collapse') : t('common.expand')" @click="row.expanded = !row.expanded">
                  <Icon name="chevronDown" size="md" :class="row.expanded ? 'rotate-180' : ''" />
                </button>
                <button class="btn btn-ghost btn-icon text-red-600 dark:text-red-400" :title="t('admin.quotaStatus.groups.remove')" @click="removeGroup(groupIndex)">
                  <Icon name="trash" size="md" />
                </button>
              </div>
            </header>

            <div v-if="row.expanded" class="p-4">
              <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
                <p class="text-sm text-gray-500 dark:text-gray-400">
                  {{ t('admin.quotaStatus.accounts.description') }}
                </p>
                <div class="flex gap-2">
                  <button class="btn btn-ghost text-xs" @click="setAllIncluded(row, true)">{{ t('common.selectAll') }}</button>
                  <button class="btn btn-ghost text-xs" @click="setAllIncluded(row, false)">{{ t('admin.quotaStatus.accounts.clearSelection') }}</button>
                </div>
              </div>

              <div v-if="row.loading" class="h-24 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-800"></div>
              <div v-else-if="row.accounts.length === 0" class="rounded-lg border border-dashed border-gray-300 px-4 py-8 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
                {{ t('admin.quotaStatus.accounts.empty') }}
              </div>
              <div v-else class="overflow-x-auto">
                <table class="min-w-full table-fixed">
                  <thead>
                    <tr class="text-left text-xs font-medium text-gray-500 dark:text-gray-400">
                      <th class="w-14 px-3 py-2">{{ t('admin.quotaStatus.accounts.include') }}</th>
                      <th class="min-w-52 px-3 py-2">{{ t('admin.quotaStatus.accounts.account') }}</th>
                      <th class="w-32 px-3 py-2">{{ t('admin.quotaStatus.accounts.showName') }}</th>
                      <th class="min-w-52 px-3 py-2">{{ t('admin.quotaStatus.accounts.displayName') }}</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                    <tr v-for="accountRow in row.accounts" :key="accountRow.account.id">
                      <td class="px-3 py-3">
                        <input v-model="accountRow.included" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
                      </td>
                      <td class="px-3 py-3">
                        <div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ accountRow.account.name }}</div>
                        <div class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">{{ accountRow.account.type }}</div>
                      </td>
                      <td class="px-3 py-3">
                        <Toggle v-model="accountRow.showName" />
                      </td>
                      <td class="px-3 py-3">
                        <input
                          v-model="accountRow.displayName"
                          class="input"
                          :disabled="!accountRow.included || !accountRow.showName"
                          :placeholder="accountRow.account.name"
                          maxlength="100"
                        />
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
          </section>
        </div>
      </div>

      <div class="sticky bottom-4 flex justify-end">
        <button class="btn btn-primary min-w-28 shadow-lg" :disabled="saving || loading" @click="save">
          <Icon v-if="!saving" name="check" size="md" class="mr-2" />
          <Icon v-else name="refresh" size="md" class="mr-2 animate-spin" />
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import { adminAPI } from '@/api/admin'
import type { QuotaStatusConfig, QuotaStatusGroupConfig } from '@/api/admin/quotaStatus'
import type { Account, AdminGroup } from '@/types'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

interface AccountRow {
  account: Account
  included: boolean
  showName: boolean
  displayName: string
}

interface GroupRow {
  key: string
  group: AdminGroup
  displayName: string
  accounts: AccountRow[]
  expanded: boolean
  loading: boolean
}

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
const saving = ref(false)
const addingGroup = ref(false)
const groups = ref<AdminGroup[]>([])
const groupRows = ref<GroupRow[]>([])
const newGroupID = ref<number | null>(null)
const form = reactive({ enabled: false, title: '', description: '' })

const publicURL = computed(() => `${window.location.origin}/quota-status`)
const usedGroupIDs = computed(() => new Set(groupRows.value.map(row => row.group.id)))
const availableGroupOptions = computed(() => groups.value
  .filter(group => !usedGroupIDs.value.has(group.id))
  .map(group => ({ value: group.id, label: `${group.name} (${platformLabel(group.platform)})` })))

function platformLabel(platform: string): string {
  return t(`admin.groups.platforms.${platform}`, platform)
}

function selectedCount(row: GroupRow): number {
  return row.accounts.filter(account => account.included).length
}

async function listAllGroupAccounts(groupID: number): Promise<Account[]> {
  const pageSize = 100
  const accounts: Account[] = []
  let page = 1
  while (true) {
    const response = await adminAPI.accounts.list(page, pageSize, { group: String(groupID), sort_by: 'name', sort_order: 'asc' })
    accounts.push(...(response.items || []))
    if (accounts.length >= response.total || (response.items || []).length === 0) break
    page += 1
  }
  return accounts
}

async function buildGroupRow(group: AdminGroup, saved?: QuotaStatusGroupConfig, includeAll = false): Promise<GroupRow> {
  const row: GroupRow = reactive({
    key: saved?.id || `group-${group.id}`,
    group,
    displayName: saved?.display_name || '',
    accounts: [],
    expanded: true,
    loading: true,
  })
  const savedByID = new Map((saved?.accounts || []).map(item => [item.account_id, item]))
  try {
    const accounts = await listAllGroupAccounts(group.id)
    row.accounts = accounts.map(account => {
      const item = savedByID.get(account.id)
      return {
        account,
        included: includeAll || Boolean(item),
        showName: item?.show_name ?? false,
        displayName: item?.display_name || '',
      }
    })
  } finally {
    row.loading = false
  }
  return row
}

async function load() {
  loading.value = true
  try {
    const [config, allGroups] = await Promise.all([
      adminAPI.quotaStatus.getConfig(),
      adminAPI.groups.getAllIncludingInactive(),
    ])
    groups.value = allGroups
    form.enabled = config.enabled
    form.title = config.title
    form.description = config.description
    const groupByID = new Map(allGroups.map(group => [group.id, group]))
    groupRows.value = await Promise.all(config.groups
      .map(saved => ({ saved, group: groupByID.get(saved.group_id) }))
      .filter((item): item is { saved: QuotaStatusGroupConfig; group: AdminGroup } => Boolean(item.group))
      .map(item => buildGroupRow(item.group, item.saved)))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.quotaStatus.loadError')))
  } finally {
    loading.value = false
  }
}

async function addGroup() {
  const group = groups.value.find(item => item.id === Number(newGroupID.value))
  if (!group) return
  addingGroup.value = true
  try {
    groupRows.value.push(await buildGroupRow(group, undefined, true))
    newGroupID.value = null
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.quotaStatus.loadAccountsError')))
  } finally {
    addingGroup.value = false
  }
}

function removeGroup(index: number) {
  groupRows.value.splice(index, 1)
}

function setAllIncluded(row: GroupRow, included: boolean) {
  row.accounts.forEach(account => { account.included = included })
}

function toConfig(): QuotaStatusConfig {
  return {
    enabled: form.enabled,
    title: form.title.trim(),
    description: form.description.trim(),
    groups: groupRows.value.map(row => ({
      id: row.key,
      group_id: row.group.id,
      display_name: row.displayName.trim(),
      accounts: row.accounts
        .filter(item => item.included)
        .map(item => ({
          account_id: item.account.id,
          display_name: item.displayName.trim(),
          show_name: item.showName,
        })),
    })),
  }
}

async function save() {
  saving.value = true
  try {
    await adminAPI.quotaStatus.updateConfig(toConfig())
    appStore.showSuccess(t('admin.quotaStatus.saveSuccess'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.quotaStatus.saveError')))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
