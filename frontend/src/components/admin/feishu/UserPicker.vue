<template>
  <div class="space-y-2">
    <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">{{ label }}</label>
    <div class="flex gap-2">
      <div class="relative flex-1">
        <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-2.5 text-gray-400" />
        <input v-model.trim="query" class="input w-full pl-9" :placeholder="placeholder" @keydown.enter.prevent="search" />
      </div>
      <button type="button" class="btn btn-secondary" :disabled="loading" @click="search">{{ loading ? tx('查询中', 'Searching') : tx('查询', 'Search') }}</button>
    </div>
    <select :value="modelValue || ''" class="input w-full" @change="selectUser">
      <option value="">{{ tx('请选择已绑定飞书的用户', 'Select a Feishu-bound user') }}</option>
      <option v-for="user in users" :key="user.id" :value="user.id">#{{ user.id }} · {{ user.username || user.email }}</option>
    </select>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { settingsAPI, type FeishuBoundUser } from '@/api/admin/settings'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{ modelValue?: number; label: string; placeholder?: string }>()
const emit = defineEmits<{
  'update:modelValue': [value: number]
  selected: [value: FeishuBoundUser | null]
}>()
const { locale } = useI18n()
const users = ref<FeishuBoundUser[]>([])
const query = ref('')
const loading = ref(false)
const tx = (zh: string, en: string) => locale.value.startsWith('zh') ? zh : en

async function search() {
  loading.value = true
  try {
    users.value = await settingsAPI.listFeishuBoundUsers(query.value, 30)
    const selected = users.value.find(user => user.id === props.modelValue) || null
    if (props.modelValue && !selected) {
      emit('update:modelValue', 0)
      emit('selected', null)
    } else if (selected) {
      emit('selected', selected)
    }
  } finally {
    loading.value = false
  }
}

function selectUser(event: Event) {
  const value = Number((event.target as HTMLSelectElement).value) || 0
  emit('update:modelValue', value)
  emit('selected', users.value.find(user => user.id === value) || null)
}

onMounted(search)
</script>
