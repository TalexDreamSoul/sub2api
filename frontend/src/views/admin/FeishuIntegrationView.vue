<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-6xl space-y-6 px-4 py-6 sm:px-6">
      <header class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 pb-5 dark:border-dark-700">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ tx('飞书集成中心', 'Feishu integration') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ tx('配置、诊断、私聊与投递记录', 'Configuration, diagnostics, messaging, and delivery history') }}</p>
        </div>
        <span :class="['inline-flex items-center gap-2 text-sm font-medium', form.enabled ? 'text-green-600' : 'text-gray-500']">
          <span :class="['h-2 w-2 rounded-full', form.enabled ? 'bg-green-500' : 'bg-gray-400']"></span>
          {{ form.enabled ? tx('已启用', 'Enabled') : tx('未启用', 'Disabled') }}
        </span>
      </header>

      <div class="flex gap-1 overflow-x-auto border-b border-gray-200 dark:border-dark-700" role="tablist">
        <button v-for="item in tabs" :key="item.key" type="button" :class="tabClass(item.key)" @click="activeTab = item.key">
          <Icon :name="item.icon" size="sm" />{{ item.label }}
        </button>
      </div>

      <section v-if="activeTab === 'config'" class="space-y-5">
        <div class="flex items-center justify-between border-b border-gray-100 pb-4 dark:border-dark-700">
          <div><h2 class="section-title">{{ tx('应用配置', 'Application configuration') }}</h2></div>
          <Toggle v-model="form.enabled" />
        </div>
        <div class="grid gap-4 md:grid-cols-2">
          <label class="field"><span>App ID</span><input v-model.trim="form.appId" class="input" autocomplete="off" /></label>
          <label class="field"><span>App Secret</span><input v-model="form.appSecret" type="password" class="input" autocomplete="new-password" :placeholder="secretPlaceholder(settings?.feishu_notify_app_secret_configured)" /></label>
          <label class="field"><span>Verification Token</span><input v-model="form.verificationToken" type="password" class="input" autocomplete="new-password" :placeholder="secretPlaceholder(settings?.feishu_notify_verification_token_configured)" /></label>
          <label class="field"><span>Encrypt Key</span><input v-model="form.encryptKey" type="password" class="input" autocomplete="new-password" :placeholder="secretPlaceholder(settings?.feishu_notify_encrypt_key_configured)" /></label>
          <label class="field md:col-span-2"><span>{{ tx('事件回调地址', 'Event callback URL') }}</span><input :value="eventCallbackURL" class="input font-mono text-xs" readonly /></label>
          <label class="field md:col-span-2"><span>{{ tx('面板地址', 'Panel URL') }}</span><input v-model.trim="form.panelUrl" class="input" /></label>
        </div>
        <div class="flex justify-end"><button class="btn btn-primary" type="button" :disabled="saving" @click="saveConfig"><Icon name="check" size="sm" />{{ saving ? tx('保存中', 'Saving') : tx('保存配置', 'Save configuration') }}</button></div>
      </section>

      <section v-else-if="activeTab === 'diagnostics'" class="space-y-5">
        <h2 class="section-title">{{ tx('全链路诊断', 'End-to-end diagnostics') }}</h2>
        <UserPicker v-model="selectedUserId" :label="tx('测试用户', 'Test user')" />
        <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300"><input v-model="sendTest" type="checkbox" class="checkbox" />{{ tx('同时发送真实测试卡片', 'Also send a real test card') }}</label>
        <button class="btn btn-primary" type="button" :disabled="diagnosing" @click="runDiagnostics"><Icon name="chart" size="sm" />{{ diagnosing ? tx('诊断中', 'Running') : tx('开始诊断', 'Run diagnostics') }}</button>
        <div v-if="diagnostic" class="divide-y divide-gray-100 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700">
          <div v-for="step in diagnostic.steps" :key="step.name" class="grid gap-2 py-3 sm:grid-cols-[160px_100px_1fr_80px] sm:items-center">
            <span class="font-mono text-sm text-gray-700 dark:text-gray-300">{{ step.name }}</span>
            <span :class="statusClass(step.status)">{{ statusLabel(step.status) }}</span>
            <span class="text-sm text-gray-600 dark:text-gray-400">{{ step.message }}</span>
            <span class="text-right text-xs text-gray-400">{{ step.latency_ms }} ms</span>
          </div>
        </div>
      </section>

      <section v-else-if="activeTab === 'messages'" class="space-y-5">
        <h2 class="section-title">{{ tx('私聊用户', 'Message a user') }}</h2>
        <UserPicker v-model="selectedUserId" :label="tx('收件用户', 'Recipient')" @selected="selectedRecipient = $event" />
        <label class="field"><span>{{ tx('消息正文', 'Message') }}</span><textarea v-model="message" class="input min-h-36 resize-y" maxlength="2000"></textarea><small>{{ message.length }} / 2000</small></label>
        <div class="flex justify-end"><button class="btn btn-primary" type="button" :disabled="!canPreview" @click="showMessageConfirm = true"><Icon name="chat" size="sm" />{{ tx('预览并发送', 'Preview and send') }}</button></div>
      </section>

      <section v-else class="space-y-5">
        <div class="flex items-center justify-between"><h2 class="section-title">{{ tx('最近投递', 'Recent deliveries') }}</h2><button class="btn btn-secondary" type="button" @click="loadDeliveries"><Icon name="refresh" size="sm" />{{ tx('刷新', 'Refresh') }}</button></div>
        <div class="overflow-x-auto border-y border-gray-200 dark:border-dark-700">
          <table class="w-full text-left text-sm"><thead class="bg-gray-50 text-xs uppercase text-gray-500 dark:bg-dark-800"><tr><th class="px-3 py-2">ID</th><th class="px-3 py-2">{{ tx('用户', 'User') }}</th><th class="px-3 py-2">{{ tx('类型', 'Category') }}</th><th class="px-3 py-2">{{ tx('状态', 'Status') }}</th><th class="px-3 py-2">{{ tx('尝试', 'Attempts') }}</th><th class="px-3 py-2">{{ tx('创建时间', 'Created') }}</th></tr></thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr v-for="item in deliveries" :key="item.id"><td class="px-3 py-3 font-mono">{{ item.id }}</td><td class="px-3 py-3">{{ item.user_id || '—' }}</td><td class="px-3 py-3">{{ item.category }}</td><td class="px-3 py-3"><span :class="statusClass(item.status)">{{ item.status }}</span></td><td class="px-3 py-3">{{ item.attempts }}</td><td class="px-3 py-3">{{ formatDate(item.created_at) }}</td></tr></tbody></table>
          <div v-if="!deliveries.length" class="py-10 text-center text-sm text-gray-500">{{ tx('暂无投递记录', 'No deliveries') }}</div>
        </div>
      </section>
    </div>

    <ConfirmDialog :show="showMessageConfirm" :title="tx('确认发送私聊', 'Confirm direct message')" :message="confirmMessage" :confirm-text="tx('确认发送', 'Send')" @confirm="sendMessage" @cancel="showMessageConfirm = false">
      <div class="mb-2 text-sm font-medium text-gray-700 dark:text-gray-300">{{ selectedUserLabel }}</div>
      <div class="whitespace-pre-wrap border-y border-gray-200 py-3 text-sm text-gray-800 dark:border-dark-600 dark:text-gray-200">{{ message.trim() }}</div>
    </ConfirmDialog>
    <TotpStepUpDialog :controller="stepUp" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import UserPicker from '@/components/admin/feishu/UserPicker.vue'
import { buildApiUrl } from '@/api/client'
import { settingsAPI, type SystemSettings, type FeishuDiagnosticReport, type FeishuDelivery, type FeishuBoundUser } from '@/api/admin/settings'
import { useAppStore } from '@/stores'
import { useStepUp, isStepUpCancelled, isStepUpBlocked } from '@/composables/useStepUp'

const { locale } = useI18n()
const appStore = useAppStore()
const stepUp = useStepUp()
const tx = (zh: string, en: string) => locale.value.startsWith('zh') ? zh : en
const activeTab = ref<'config' | 'diagnostics' | 'messages' | 'deliveries'>('config')
const settings = ref<SystemSettings | null>(null)
const saving = ref(false)
const diagnosing = ref(false)
const selectedUserId = ref(0)
const selectedRecipient = ref<FeishuBoundUser | null>(null)
const sendTest = ref(false)
const diagnostic = ref<FeishuDiagnosticReport | null>(null)
const message = ref('')
const messageIdempotencyKey = ref('')
const showMessageConfirm = ref(false)
const deliveries = ref<FeishuDelivery[]>([])
const form = reactive({ enabled: false, appId: '', appSecret: '', verificationToken: '', encryptKey: '', panelUrl: '/feishu/panel' })

const tabs = computed(() => [
  { key: 'config' as const, icon: 'cog' as const, label: tx('配置', 'Configuration') },
  { key: 'diagnostics' as const, icon: 'chart' as const, label: tx('诊断', 'Diagnostics') },
  { key: 'messages' as const, icon: 'chat' as const, label: tx('私聊', 'Messages') },
  { key: 'deliveries' as const, icon: 'inbox' as const, label: tx('投递记录', 'Deliveries') },
])
const eventCallbackURL = computed(() => new URL(buildApiUrl('/integrations/feishu/events'), window.location.origin).toString())
const canPreview = computed(() => selectedUserId.value > 0 && selectedRecipient.value?.id === selectedUserId.value && message.value.trim().length > 0)
const selectedUserLabel = computed(() => {
  const user = selectedRecipient.value
  if (!user || user.id !== selectedUserId.value) return tx('未选择有效收件人', 'No valid recipient selected')
  return `#${user.id} · ${user.username || user.email}`
})
const confirmMessage = computed(() => `${tx('消息将进入可靠投递队列，收件人为：', 'The message will be queued for:')} ${selectedUserLabel.value}`)
const secretPlaceholder = (configured?: boolean) => configured ? tx('已配置，留空保留', 'Configured; leave blank to keep') : tx('尚未配置', 'Not configured')

function tabClass(key: string) {
  return ['flex shrink-0 items-center gap-2 border-b-2 px-4 py-3 text-sm font-medium', activeTab.value === key ? 'border-primary-500 text-primary-600 dark:text-primary-400' : 'border-transparent text-gray-500 hover:text-gray-800 dark:hover:text-gray-200']
}
function statusClass(status: string) {
  const base = 'inline-flex w-fit rounded px-2 py-0.5 text-xs font-medium'
  if (['passed', 'sent', 'processed'].includes(status)) return `${base} bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300`
  if (['failed', 'dead'].includes(status)) return `${base} bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300`
  if (['warning', 'pending'].includes(status)) return `${base} bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300`
  return `${base} bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300`
}
function statusLabel(status: string) {
  return ({ passed: tx('通过', 'Passed'), warning: tx('警告', 'Warning'), failed: tx('失败', 'Failed') } as Record<string, string>)[status] || status
}
function formatDate(value: string) { return new Date(value).toLocaleString() }
function reportError(error: unknown) {
  if (isStepUpCancelled(error)) return
  if (isStepUpBlocked(error)) appStore.showError(tx('当前管理员无法完成二次验证', 'Step-up verification is unavailable'))
  else appStore.showError((error as { message?: string })?.message || tx('请求失败', 'Request failed'))
}

async function loadSettings() {
  try {
    const value = await settingsAPI.getSettings()
    settings.value = value
    Object.assign(form, { enabled: value.feishu_notify_enabled, appId: value.feishu_notify_app_id || '', appSecret: '', verificationToken: '', encryptKey: '', panelUrl: value.feishu_notify_panel_url || '/feishu/panel' })
  } catch (error) { reportError(error) }
}
async function saveConfig() {
  saving.value = true
  try {
    const payload: Record<string, unknown> = { feishu_notify_enabled: form.enabled, feishu_notify_app_id: form.appId, feishu_notify_panel_url: form.panelUrl }
    if (form.appSecret) payload.feishu_notify_app_secret = form.appSecret
    if (form.verificationToken) payload.feishu_notify_verification_token = form.verificationToken
    if (form.encryptKey) payload.feishu_notify_encrypt_key = form.encryptKey
    await stepUp.run(() => settingsAPI.updateSettings(payload))
    appStore.showSuccess(tx('飞书配置已保存', 'Feishu configuration saved'))
    await loadSettings()
  } catch (error) { reportError(error) } finally { saving.value = false }
}
async function runDiagnostics() {
  diagnosing.value = true
  try {
    const execute = () => settingsAPI.diagnoseFeishuNotification(selectedUserId.value || undefined, sendTest.value)
    diagnostic.value = sendTest.value ? await stepUp.run(execute) : await execute()
  } catch (error) { reportError(error) } finally { diagnosing.value = false }
}
async function sendMessage() {
  showMessageConfirm.value = false
  if (!canPreview.value) return
  try {
    if (!messageIdempotencyKey.value) {
      messageIdempotencyKey.value = typeof crypto.randomUUID === 'function' ? crypto.randomUUID() : `msg-${Date.now()}`
    }
    await stepUp.run(() => settingsAPI.sendFeishuAdminMessage(selectedUserId.value, message.value.trim(), messageIdempotencyKey.value))
    messageIdempotencyKey.value = ''
    message.value = ''
    appStore.showSuccess(tx('消息已进入投递队列', 'Message queued'))
    await loadDeliveries()
  } catch (error) { reportError(error) }
}
async function loadDeliveries() {
  try { deliveries.value = await settingsAPI.listFeishuDeliveries(100) }
  catch (error) { reportError(error) }
}
watch([selectedUserId, message], () => { messageIdempotencyKey.value = '' })
watch(activeTab, value => { if (value === 'deliveries') void loadDeliveries() })
onMounted(loadSettings)
</script>

<style scoped>
.section-title { @apply text-lg font-semibold text-gray-900 dark:text-white; }
.field { @apply flex flex-col gap-1.5 text-sm font-medium text-gray-700 dark:text-gray-300; }
.field small { @apply text-right text-xs font-normal text-gray-400; }
.btn { @apply inline-flex items-center gap-2; }
</style>
