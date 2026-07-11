<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { OpenAIQuotaNotifyRule, OpenAIQuotaNotifyWindow } from '@/constants/account'

const props = defineProps<{
  enabled: boolean
  rules: OpenAIQuotaNotifyRule[]
  globalEnabled: boolean
}>()

const emit = defineEmits<{
  'update:enabled': [value: boolean]
  'update:rules': [value: OpenAIQuotaNotifyRule[]]
}>()

const { t } = useI18n()

function toggleEnabled() {
  if (!props.globalEnabled) return
  const nextEnabled = !props.enabled
  emit('update:enabled', nextEnabled)
  if (nextEnabled && props.rules.length === 0) {
    emit('update:rules', [
      { window: '5h', remaining_percent: 20 },
      { window: '7d', remaining_percent: 20 },
    ])
  }
}

function updateRule(index: number, patch: Partial<OpenAIQuotaNotifyRule>) {
  const next = props.rules.map((rule, ruleIndex) =>
    ruleIndex === index ? { ...rule, ...patch } : rule
  )
  emit('update:rules', next)
}

function addRule() {
  if (props.rules.length >= 10) return
  emit('update:rules', [...props.rules, { window: '5h', remaining_percent: 20 }])
}

function removeRule(index: number) {
  emit('update:rules', props.rules.filter((_, ruleIndex) => ruleIndex !== index))
}
</script>

<template>
  <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
    <div class="flex items-center justify-between gap-4">
      <div class="min-w-0">
        <label class="input-label mb-0">{{ t('admin.accounts.openai.quotaNotify.title') }}</label>
        <span
          v-if="!globalEnabled"
          class="mt-1 inline-block text-xs font-medium text-amber-600 dark:text-amber-400"
        >
          {{ t('admin.accounts.openai.quotaNotify.globalDisabled') }}
        </span>
      </div>
      <button
        type="button"
        role="switch"
        :aria-checked="enabled"
        :disabled="!globalEnabled"
        :title="t('admin.accounts.openai.quotaNotify.title')"
        :class="[
          'relative inline-flex h-6 w-11 shrink-0 rounded-full border-2 border-transparent transition-colors focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50',
          enabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
        ]"
        @click="toggleEnabled"
      >
        <span
          :class="[
            'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow transition',
            enabled ? 'translate-x-5' : 'translate-x-0'
          ]"
        />
      </button>
    </div>

    <div v-if="enabled" class="mt-3 space-y-2">
      <div
        v-for="(rule, index) in rules"
        :key="index"
        class="grid grid-cols-[5.5rem_minmax(0,1fr)_2.5rem] items-center gap-2"
      >
        <select
          :value="rule.window"
          class="input py-1.5 text-sm"
          :aria-label="t('admin.accounts.openai.quotaNotify.window')"
          @change="updateRule(index, { window: ($event.target as HTMLSelectElement).value as OpenAIQuotaNotifyWindow })"
        >
          <option value="5h">5h</option>
          <option value="7d">7d</option>
        </select>
        <label class="flex min-w-0 items-center gap-2">
          <span class="shrink-0 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.openai.quotaNotify.remainingAtMost') }}
          </span>
          <div class="relative min-w-0 flex-1">
            <input
              :value="rule.remaining_percent"
              type="number"
              min="1"
              max="99"
              step="1"
              class="input py-1.5 pr-7 text-sm"
              @input="updateRule(index, { remaining_percent: Number(($event.target as HTMLInputElement).value) })"
            />
            <span class="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-xs text-gray-400">%</span>
          </div>
        </label>
        <button
          type="button"
          class="inline-flex h-8 w-8 items-center justify-center rounded text-gray-400 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
          :title="t('common.delete')"
          @click="removeRule(index)"
        >
          <Icon name="x" size="xs" class="h-4 w-4" />
        </button>
      </div>

      <button
        type="button"
        class="btn btn-secondary btn-sm"
        :disabled="rules.length >= 10"
        @click="addRule"
      >
        {{ t('admin.accounts.openai.quotaNotify.addRule') }}
      </button>
    </div>
  </div>
</template>
