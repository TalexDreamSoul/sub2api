import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import OpenAIQuotaNotifyRules from '../OpenAIQuotaNotifyRules.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('OpenAIQuotaNotifyRules', () => {
  it('首次启用时创建 5h 和 7d 的 20% 默认规则', async () => {
    const wrapper = mount(OpenAIQuotaNotifyRules, {
      props: { enabled: false, rules: [], globalEnabled: true },
      global: { stubs: { Icon: true } }
    })

    await wrapper.get('[role="switch"]').trigger('click')

    expect(wrapper.emitted('update:enabled')?.[0]).toEqual([true])
    expect(wrapper.emitted('update:rules')?.[0]).toEqual([[
      { window: '5h', remaining_percent: 20 },
      { window: '7d', remaining_percent: 20 }
    ]])
  })

  it('全局开关关闭时不允许启用账号规则', async () => {
    const wrapper = mount(OpenAIQuotaNotifyRules, {
      props: { enabled: false, rules: [], globalEnabled: false },
      global: { stubs: { Icon: true } }
    })

    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:enabled')).toBeUndefined()
  })

  it('可以添加和删除多条规则', async () => {
    const wrapper = mount(OpenAIQuotaNotifyRules, {
      props: {
        enabled: true,
        rules: [{ window: '5h', remaining_percent: 20 }],
        globalEnabled: true
      },
      global: { stubs: { Icon: true } }
    })

    const buttons = wrapper.findAll('button')
    await buttons.at(-1)!.trigger('click')
    expect(wrapper.emitted('update:rules')?.[0]).toEqual([[
      { window: '5h', remaining_percent: 20 },
      { window: '5h', remaining_percent: 20 }
    ]])

    await wrapper.findAll('button')[1].trigger('click')
    expect(wrapper.emitted('update:rules')?.[1]).toEqual([[]])
  })
})
