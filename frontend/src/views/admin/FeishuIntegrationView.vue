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
          <label class="field md:col-span-2">
            <span>{{ tx('事件回调地址', 'Event callback URL') }}</span>
            <input :value="eventCallbackURL" class="input font-mono text-xs" readonly />
            <small class="text-left">{{ tx('必须订阅 im.message.receive_v1，并开通“读取用户发给机器人的单聊消息”和“接收群聊中 @ 机器人消息”权限；修改后需发布应用版本。群助手另需机器人进群/退群、群解散、群信息变更和机器人禁言事件。', 'Subscribe to im.message.receive_v1 and grant “Read direct messages sent to bot” plus “Receive users’ mentions”; publish a new app version after changes. Group assistant lifecycle also uses bot added/removed, chat disbanded/updated, and bot muted events.') }}</small>
          </label>
          <label class="field md:col-span-2"><span>{{ tx('面板地址', 'Panel URL') }}</span><input v-model.trim="form.panelUrl" class="input" /></label>
        </div>
        <div class="flex justify-end"><button class="btn btn-primary" type="button" :disabled="saving" @click="saveConfig"><Icon name="check" size="sm" />{{ saving ? tx('保存中', 'Saving') : tx('保存配置', 'Save configuration') }}</button></div>
      </section>

      <section v-else-if="activeTab === 'assistant'" class="space-y-5">
        <div class="flex items-center justify-between border-b border-gray-100 pb-4 dark:border-dark-700">
          <div><h2 class="section-title">{{ tx('智能账户助手', 'AI account assistant') }}</h2><p class="mt-1 text-sm text-gray-500">{{ tx('确定性指令始终可用，模型仅用于自由文本理解和只读 Function Calling。', 'Deterministic commands remain available; the model only handles natural language and read-only Function Calling.') }}</p></div>
          <Toggle v-model="assistantForm.enabled" />
        </div>
        <div class="grid gap-4 md:grid-cols-2">
          <label class="field"><span>{{ tx('专用 API Key ID', 'Dedicated API Key ID') }}</span><input v-model.number="assistantForm.api_key_id" type="number" min="1" class="input" /><small>{{ assistantConfig?.api_key_hint || tx('请使用独立 Key，机器人用量将从用户排行中排除', 'Use a dedicated key; assistant usage is excluded from user rankings') }}</small></label>
          <label class="field"><span>{{ tx('模型', 'Model') }}</span><input v-model.trim="assistantForm.model" class="input" placeholder="gpt-5.5" /></label>
          <label class="field"><span>{{ tx('API Key 申请策略', 'API Key request policy') }}</span><select v-model="assistantForm.api_key_request_mode" class="input"><option value="disabled">{{ tx('关闭', 'Disabled') }}</option><option value="manual">{{ tx('人工审批', 'Manual approval') }}</option><option value="auto">{{ tx('自动签发', 'Automatic issuance') }}</option></select></label>
          <label class="field"><span>{{ tx('每用户最大活跃 Key', 'Maximum active keys per user') }}</span><input v-model.number="assistantForm.max_active_keys" type="number" min="1" max="100" class="input" /></label>
          <label class="field"><span>{{ tx('默认分组 ID（可选）', 'Default group ID (optional)') }}</span><input v-model.number="assistantForm.default_group_id" type="number" min="0" class="input" /></label>
          <label class="field"><span>{{ tx('日报时间', 'Daily digest time') }}</span><input v-model="assistantForm.daily_digest_time" type="time" class="input" /></label>
          <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300 md:col-span-2"><input v-model="assistantForm.daily_digest_enabled" type="checkbox" class="checkbox" />{{ tx('启用每日使用、排名、占比和剩余额度推送', 'Send daily usage, rank, share, and remaining quota digest') }}</label>
        </div>
        <div class="flex flex-wrap justify-end gap-2"><button class="btn btn-secondary" type="button" :disabled="assistantTesting" @click="testAssistant"><Icon name="chart" size="sm" />{{ assistantTesting ? tx('测试中', 'Testing') : tx('测试 Function Calling', 'Test Function Calling') }}</button><button class="btn btn-primary" type="button" :disabled="assistantSaving" @click="saveAssistant"><Icon name="check" size="sm" />{{ assistantSaving ? tx('保存中', 'Saving') : tx('保存助手配置', 'Save assistant') }}</button></div>
      </section>

      <section v-else-if="activeTab === 'groups'" class="space-y-8">
        <div class="space-y-4">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div><h2 class="section-title">{{ tx('群助手管理员', 'Group assistant administrators') }}</h2><p class="mt-1 text-sm text-gray-500">{{ tx('只有站内管理员且已绑定飞书的用户可以配置群用途和执行管理命令。', 'Only Sub2API administrators with a Feishu binding can configure chats and run management commands.') }}</p></div>
            <button class="btn btn-secondary" type="button" @click="loadGroupAssistant"><Icon name="refresh" size="sm" />{{ tx('刷新', 'Refresh') }}</button>
          </div>
          <div class="grid items-end gap-3 md:grid-cols-[1fr_auto]">
            <UserPicker v-model="assistantAdminUserId" :label="tx('添加管理员', 'Add administrator')" />
            <button class="btn btn-primary" type="button" :disabled="assistantAdminUserId <= 0 || groupAssistantSaving" @click="addAssistantAdmin"><Icon name="check" size="sm" />{{ tx('授权', 'Authorize') }}</button>
          </div>
          <div class="divide-y divide-gray-100 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700">
            <div v-for="item in assistantAdmins" :key="item.user_id" class="flex items-center justify-between gap-3 py-3">
              <div class="min-w-0"><div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ item.username || item.email }}</div><div class="text-xs text-gray-500">#{{ item.user_id }} · {{ item.email }}</div></div>
              <button class="btn btn-secondary btn-sm" type="button" :disabled="groupAssistantSaving" @click="removeAssistantAdmin(item.user_id)">{{ tx('移除', 'Remove') }}</button>
            </div>
            <div v-if="!assistantAdmins.length" class="py-8 text-center text-sm text-gray-500">{{ tx('尚未授权管理员；请先授权，再把机器人拉入群聊。', 'No administrator is authorized yet. Authorize one before adding the bot to chats.') }}</div>
          </div>
        </div>

        <div class="space-y-4">
          <div><h2 class="section-title">{{ tx('群绑定', 'Chat bindings') }}</h2><p class="mt-1 text-sm text-gray-500">{{ tx('机器人进群后会自动出现在这里。也可由已授权管理员在群内用 /绑定用户群、/绑定维护群、/绑定管理群 或 /绑定通知群 完成配置。', 'Chats appear here after the bot joins. Authorized administrators can also configure them with /绑定用户群, /绑定维护群, /绑定管理群, or /绑定通知群.') }}</p></div>
          <div class="space-y-4">
            <article v-for="chat in chatBindings" :key="chat.id" class="space-y-4 border-y border-gray-200 py-4 dark:border-dark-700">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <div><div class="font-medium text-gray-900 dark:text-white">{{ chat.chat_name || tx('未命名群聊', 'Unnamed chat') }}</div><div class="font-mono text-xs text-gray-500">{{ chat.chat_id }}</div></div>
                <span :class="statusClass(chat.status)">{{ chat.status }}</span>
              </div>
              <div class="grid gap-4 md:grid-cols-2">
                <label class="field"><span>{{ tx('群用途', 'Chat purpose') }}</span><select v-model="chat.kind" class="input"><option value="unconfigured" disabled>{{ tx('未配置', 'Unconfigured') }}</option><option value="user">{{ tx('用户使用群', 'User group') }}</option><option value="operations">{{ tx('维护群', 'Operations group') }}</option><option value="management">{{ tx('管理群', 'Management group') }}</option><option value="notifications">{{ tx('通知群', 'Notification group') }}</option></select></label>
                <label class="field"><span>{{ tx('关联 Sub2API 分组', 'Linked Sub2API group') }}</span><select v-model.number="chat.sub2api_group_id" class="input" :disabled="!chatNeedsGroup(chat)"><option :value="undefined">{{ chatNeedsGroup(chat) ? tx('请选择分组', 'Select a group') : tx('无需关联分组', 'No group required') }}</option><option v-for="group in groups" :key="group.id" :value="group.id">#{{ group.id }} · {{ group.name }}</option></select></label>
              </div>
              <div class="space-y-2">
                <div class="flex flex-wrap gap-5 text-sm text-gray-700 dark:text-gray-300">
                  <label class="flex items-center gap-2"><input v-model="chat.incident_notifications_enabled" type="checkbox" class="checkbox" :disabled="!chatSupportsIncidents(chat)" />{{ tx('渠道故障与恢复通知', 'Channel incident and recovery notifications') }}</label>
                  <label class="flex items-center gap-2"><input v-model="chat.daily_digest_enabled" type="checkbox" class="checkbox" :disabled="!chatSupportsDigest(chat)" />{{ tx('分组每日用量摘要', 'Daily group usage digest') }}</label>
                </div>
                <p class="text-xs text-gray-500">{{ tx('群日报使用“智能助手”页的全局日报开关与发送时间。', 'Group digests use the global digest switch and delivery time on the Assistant tab.') }}</p>
              </div>
              <div class="flex justify-end"><button class="btn btn-primary" type="button" :disabled="groupAssistantSaving || chat.kind === 'unconfigured' || (chatNeedsGroup(chat) && !chat.sub2api_group_id)" @click="saveChatBinding(chat)"><Icon name="check" size="sm" />{{ tx('保存群配置', 'Save chat configuration') }}</button></div>
            </article>
            <div v-if="!chatBindings.length" class="border-y border-gray-200 py-10 text-center text-sm text-gray-500 dark:border-dark-700">{{ tx('暂无群聊。请把机器人拉入群，并在飞书开放平台订阅机器人进群和群消息事件。', 'No chats yet. Add the bot to a chat and subscribe to bot-added and group-message events in Feishu Open Platform.') }}</div>
          </div>
        </div>
      </section>

      <section v-else-if="activeTab === 'requests'" class="space-y-5">
        <div class="flex items-center justify-between"><div><h2 class="section-title">{{ tx('API Key 申请', 'API Key requests') }}</h2><p class="mt-1 text-sm text-gray-500">{{ tx('完整 API Key 只在站内安全页面展示，不会发送到飞书。', 'Full API keys are only shown on the authenticated site and are never sent to Feishu.') }}</p></div><button class="btn btn-secondary" type="button" @click="loadAPIKeyRequests"><Icon name="refresh" size="sm" />{{ tx('刷新', 'Refresh') }}</button></div>
        <div class="overflow-x-auto border-y border-gray-200 dark:border-dark-700">
          <table class="w-full text-left text-sm"><thead class="bg-gray-50 text-xs uppercase text-gray-500 dark:bg-dark-800"><tr><th class="px-3 py-2">ID</th><th class="px-3 py-2">{{ tx('用户', 'User') }}</th><th class="px-3 py-2">{{ tx('分组', 'Group') }}</th><th class="px-3 py-2">{{ tx('状态', 'Status') }}</th><th class="px-3 py-2">{{ tx('创建时间', 'Created') }}</th><th class="px-3 py-2">{{ tx('操作', 'Actions') }}</th></tr></thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr v-for="item in apiKeyRequests" :key="item.id"><td class="px-3 py-3 font-mono">{{ item.id }}</td><td class="px-3 py-3">#{{ item.user_id }}</td><td class="px-3 py-3">#{{ item.requested_group_id }}</td><td class="px-3 py-3"><span :class="statusClass(item.status)">{{ item.status }}</span></td><td class="px-3 py-3">{{ formatDate(item.created_at) }}</td><td class="px-3 py-3"><div v-if="item.status === 'pending'" class="flex gap-2"><button class="btn btn-primary btn-sm" type="button" @click="decideRequest(item.id, true)">{{ tx('批准', 'Approve') }}</button><button class="btn btn-secondary btn-sm" type="button" @click="decideRequest(item.id, false)">{{ tx('拒绝', 'Reject') }}</button></div><span v-else>—</span></td></tr></tbody></table>
          <div v-if="!apiKeyRequests.length" class="py-10 text-center text-sm text-gray-500">{{ tx('暂无待处理申请', 'No pending requests') }}</div>
        </div>
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
            <div class="min-w-0 text-sm text-gray-600 dark:text-gray-400">
              <div>{{ step.message }}</div>
              <pre v-if="step.detail" class="mt-1 whitespace-pre-wrap break-all font-mono text-xs leading-relaxed text-red-600 dark:text-red-300">{{ step.detail }}</pre>
            </div>
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

      <section v-else-if="activeTab === 'deliveries'" class="space-y-5">
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
import { groupsAPI } from '@/api/admin'
import { settingsAPI, type SystemSettings, type FeishuDiagnosticReport, type FeishuDelivery, type FeishuBoundUser, type FeishuAssistantConfig, type FeishuAPIKeyRequest, type FeishuAssistantAdmin, type FeishuChatBinding, type FeishuChatKind } from '@/api/admin/settings'
import type { AdminGroup } from '@/types'
import { useAppStore } from '@/stores'
import { useStepUp, isStepUpCancelled, isStepUpBlocked } from '@/composables/useStepUp'

const { locale } = useI18n()
const appStore = useAppStore()
const stepUp = useStepUp()
const tx = (zh: string, en: string) => locale.value.startsWith('zh') ? zh : en
const activeTab = ref<'config' | 'assistant' | 'groups' | 'requests' | 'diagnostics' | 'messages' | 'deliveries'>('config')
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
const assistantConfig = ref<FeishuAssistantConfig | null>(null)
const assistantSaving = ref(false)
const assistantTesting = ref(false)
const apiKeyRequests = ref<FeishuAPIKeyRequest[]>([])
const assistantForm = reactive<FeishuAssistantConfig>({ enabled: false, api_key_id: 0, model: '', daily_digest_enabled: false, daily_digest_time: '00:05', api_key_request_mode: 'manual', default_group_id: 0, max_active_keys: 5 })
const assistantAdminUserId = ref(0)
const assistantAdmins = ref<FeishuAssistantAdmin[]>([])
const chatBindings = ref<FeishuChatBinding[]>([])
const groups = ref<AdminGroup[]>([])
const groupAssistantSaving = ref(false)

const tabs = computed(() => [
  { key: 'config' as const, icon: 'cog' as const, label: tx('配置', 'Configuration') },
  { key: 'assistant' as const, icon: 'chat' as const, label: tx('智能助手', 'Assistant') },
  { key: 'groups' as const, icon: 'users' as const, label: tx('群助手', 'Group assistant') },
  { key: 'requests' as const, icon: 'key' as const, label: tx('Key 申请', 'Key requests') },
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
  if (['passed', 'sent', 'processed', 'issued', 'active'].includes(status)) return `${base} bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300`
  if (['failed', 'dead', 'rejected', 'cancelled'].includes(status)) return `${base} bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300`
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

function chatNeedsGroup(chat: FeishuChatBinding) {
  return chat.kind === 'user' || chat.kind === 'operations'
}
function chatSupportsIncidents(chat: FeishuChatBinding) {
  return ['operations', 'management', 'notifications'].includes(chat.kind)
}
function chatSupportsDigest(chat: FeishuChatBinding) {
  return chat.kind === 'user' || chat.kind === 'operations'
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
async function loadAssistant() {
  try {
    const value = await settingsAPI.getFeishuAssistantConfig()
    assistantConfig.value = value
    Object.assign(assistantForm, value)
  } catch (error) { reportError(error) }
}
async function saveAssistant() {
  assistantSaving.value = true
  try {
    const value = await stepUp.run(() => settingsAPI.updateFeishuAssistantConfig({ ...assistantForm, api_key_hint: undefined }))
    assistantConfig.value = value
    Object.assign(assistantForm, value)
    appStore.showSuccess(tx('飞书助手配置已保存', 'Feishu assistant configuration saved'))
  } catch (error) { reportError(error) } finally { assistantSaving.value = false }
}
async function testAssistant() {
  assistantTesting.value = true
  try {
    await stepUp.run(() => settingsAPI.testFeishuAssistantModel())
    appStore.showSuccess(tx('模型连接和 Function Calling 正常', 'Model connection and Function Calling succeeded'))
  } catch (error) { reportError(error) } finally { assistantTesting.value = false }
}
async function loadAPIKeyRequests() {
  try { apiKeyRequests.value = await settingsAPI.listFeishuAPIKeyRequests('pending', 100) }
  catch (error) { reportError(error) }
}
async function decideRequest(id: number, approve: boolean) {
  const message = approve ? tx('确认批准并创建 API Key？', 'Approve and create this API key?') : tx('确认拒绝该申请？', 'Reject this request?')
  if (!window.confirm(message)) return
  try {
    await stepUp.run(() => settingsAPI.decideFeishuAPIKeyRequest(id, approve))
    appStore.showSuccess(approve ? tx('已批准并安全通知用户', 'Approved and securely notified the user') : tx('申请已拒绝', 'Request rejected'))
    await loadAPIKeyRequests()
  } catch (error) { reportError(error) }
}
async function loadGroupAssistant() {
  try {
    const [admins, chats, availableGroups] = await Promise.all([
      settingsAPI.listFeishuAssistantAdmins(),
      settingsAPI.listFeishuChatBindings(),
      groupsAPI.getAll()
    ])
    assistantAdmins.value = admins
    chatBindings.value = chats
    groups.value = availableGroups
  } catch (error) { reportError(error) }
}
async function addAssistantAdmin() {
  if (assistantAdminUserId.value <= 0) return
  groupAssistantSaving.value = true
  try {
    await stepUp.run(() => settingsAPI.addFeishuAssistantAdmin(assistantAdminUserId.value))
    assistantAdminUserId.value = 0
    appStore.showSuccess(tx('群助手管理员已授权', 'Group assistant administrator authorized'))
    await loadGroupAssistant()
  } catch (error) { reportError(error) } finally { groupAssistantSaving.value = false }
}
async function removeAssistantAdmin(userId: number) {
  if (!window.confirm(tx('确认移除该群助手管理员？', 'Remove this group assistant administrator?'))) return
  groupAssistantSaving.value = true
  try {
    await stepUp.run(() => settingsAPI.removeFeishuAssistantAdmin(userId))
    appStore.showSuccess(tx('群助手管理员已移除', 'Group assistant administrator removed'))
    await loadGroupAssistant()
  } catch (error) { reportError(error) } finally { groupAssistantSaving.value = false }
}
async function saveChatBinding(chat: FeishuChatBinding) {
  if (chat.kind === 'unconfigured') return
  groupAssistantSaving.value = true
  try {
    const updated = await stepUp.run(() => settingsAPI.updateFeishuChatBinding(chat.id, {
      chat_name: chat.chat_name,
      kind: chat.kind as FeishuChatKind,
      sub2api_group_id: chatNeedsGroup(chat) ? (chat.sub2api_group_id || 0) : 0,
      incident_notifications_enabled: chatSupportsIncidents(chat) && chat.incident_notifications_enabled,
      daily_digest_enabled: chatSupportsDigest(chat) && chat.daily_digest_enabled
    }))
    Object.assign(chat, updated)
    appStore.showSuccess(tx('群配置已保存', 'Chat configuration saved'))
  } catch (error) { reportError(error) } finally { groupAssistantSaving.value = false }
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
watch(activeTab, value => {
  if (value === 'deliveries') void loadDeliveries()
  if (value === 'requests') void loadAPIKeyRequests()
  if (value === 'groups') void loadGroupAssistant()
})
onMounted(() => { void Promise.all([loadSettings(), loadAssistant()]) })
</script>

<style scoped>
.section-title { @apply text-lg font-semibold text-gray-900 dark:text-white; }
.field { @apply flex flex-col gap-1.5 text-sm font-medium text-gray-700 dark:text-gray-300; }
.field small { @apply text-right text-xs font-normal text-gray-400; }
.btn { @apply inline-flex items-center gap-2; }
</style>
