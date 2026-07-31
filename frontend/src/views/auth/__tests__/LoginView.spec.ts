import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import LoginView from '@/views/auth/LoginView.vue'

const { getPublicSettingsMock } = vi.hoisted(() => ({
  getPublicSettingsMock: vi.fn()
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() })
}))

vi.mock('vue-i18n', () => ({
  createI18n: () => ({
    global: { t: (key: string) => key }
  }),
  useI18n: () => ({ t: (key: string) => key })
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({ login: vi.fn(), loginWithPasskey: vi.fn() }),
  useAppStore: () => ({ showError: vi.fn(), showWarning: vi.fn() })
}))

vi.mock('@/api/auth', async () => {
  const actual = await vi.importActual<typeof import('@/api/auth')>('@/api/auth')
  return {
    ...actual,
    getPublicSettings: (...args: unknown[]) => getPublicSettingsMock(...args)
  }
})

function mountLogin() {
  return mount(LoginView, {
    global: {
      stubs: {
        AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
        Icon: true,
        TurnstileWidget: true,
        LoginAgreementPrompt: true,
        TotpLoginModal: true,
        EmailOAuthButtons: { template: '<div data-testid="email-oauth" />' },
        LinuxDoOAuthSection: { template: '<div data-testid="linuxdo-oauth" />' },
        DingTalkOAuthSection: { template: '<div data-testid="dingtalk-oauth" />' },
        FeishuOAuthSection: { template: '<div data-testid="feishu-oauth" />' },
        WechatOAuthSection: { template: '<div data-testid="wechat-oauth" />' },
        OidcOAuthSection: { template: '<div data-testid="oidc-oauth" />' },
        RouterLink: { template: '<a><slot /></a>' }
      }
    }
  })
}

describe('LoginView OAuth visibility', () => {
  beforeEach(() => {
    getPublicSettingsMock.mockReset()
    getPublicSettingsMock.mockResolvedValue({
      turnstile_enabled: false,
      turnstile_site_key: '',
      linuxdo_oauth_enabled: true,
      dingtalk_oauth_enabled: true,
      feishu_oauth_enabled: true,
      wechat_oauth_enabled: false,
      oidc_oauth_enabled: true,
      oidc_oauth_provider_name: 'Company SSO',
      github_oauth_enabled: false,
      google_oauth_enabled: false,
      backend_mode_enabled: true,
      password_reset_enabled: true,
      passkey_enabled: false,
      login_agreement_enabled: false,
      login_agreement_documents: []
    })
  })

  it('keeps configured OAuth methods visible in backend mode', async () => {
    const wrapper = mountLogin()
    await flushPromises()

    expect(wrapper.find('[data-testid="linuxdo-oauth"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="dingtalk-oauth"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="feishu-oauth"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="oidc-oauth"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('auth.forgotPassword')
    expect(wrapper.text()).not.toContain('auth.signUp')
  })
})
