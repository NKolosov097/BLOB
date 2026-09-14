import { mountSuspended } from '@nuxt/test-utils/runtime'
import { clearNuxtData } from '#imports'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { afterAll, afterEach, describe, expect, it, vi } from 'vitest'
import App from '../app/app.vue'
import IndexPage from '../app/pages/index.vue'

const readyResponse = () => new Response(
  JSON.stringify({ status: 'ok' }),
  { headers: { 'content-type': 'application/json' }, status: 200 },
)
const readyFetch = vi.fn(async () => readyResponse())
const style = document.createElement('style')
style.textContent = readFileSync(resolve('app/assets/css/main.css'), 'utf8')

afterAll(() => style.remove())

afterEach(() => {
  clearNuxtData()
  readyFetch.mockClear()
  vi.unstubAllGlobals()
})

describe('status page', () => {
  it('renders the API readiness result', async () => {
    vi.stubGlobal('fetch', readyFetch)
    const wrapper = await mountSuspended(IndexPage)

    expect(wrapper.get('[data-test="api-status"]').text()).toContain('Готов')
    expect(readyFetch).toHaveBeenCalledOnce()
    const request = readyFetch.mock.calls[0]![0] as Request
    expect(request.url).toBe('http://localhost:3000/api/v1/health/ready')
    expect(request.method).toBe('GET')
  })

  it.each([
    { dir: 'ltr', heading: 'Состояние сервиса', lang: 'ru-RU', route: '/', status: 'Готов' },
    { dir: 'ltr', heading: 'Service status', lang: 'en-US', route: '/en', status: 'Ready' },
    { dir: 'rtl', heading: 'حالة الخدمة', lang: 'ar', route: '/ar', status: 'جاهز' },
  ])('renders $route with localized document direction', async ({ dir, heading, lang, route, status }) => {
    vi.stubGlobal('fetch', readyFetch)
    const wrapper = await mountSuspended(App, { route })

    expect(wrapper.findAll('main')).toHaveLength(1)
    expect(wrapper.findAll('h1')).toHaveLength(1)
    expect(wrapper.get('h1').text()).toBe(heading)
    expect(wrapper.get('[role="status"]').text()).toBe(status)
    await vi.waitFor(() => {
      expect(document.documentElement.lang).toBe(lang)
      expect(document.documentElement.dir).toBe(dir)
    })
  })

  it('renders a safe localized unavailable state', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: {
        code: 'SERVICE_UNAVAILABLE',
        message: 'database unavailable',
        request_id: 'request-123',
      },
    }), { headers: { 'content-type': 'application/json' }, status: 503 })))
    const wrapper = await mountSuspended(IndexPage)
    const status = wrapper.get('[role="status"]')

    expect(status.text()).toBe('Недоступен')
    expect(wrapper.text()).not.toContain('database unavailable')
    expect(wrapper.text()).not.toContain('request-123')
  })

  it('uses system color schemes and logical spacing', async () => {
    vi.stubGlobal('fetch', readyFetch)
    await mountSuspended(App, { route: '/ar' })
    document.head.append(style)
    const rules = [...(style.sheet as CSSStyleSheet).cssRules] as CSSStyleRule[]
    const rootStyle = rules.find(rule => rule.selectorText === ':root')!.style
    const mainStyle = rules.find(rule => rule.selectorText === 'main')!.style

    expect(rootStyle.getPropertyValue('color-scheme')).toBe('light dark')
    expect(mainStyle.getPropertyValue('margin-inline')).toBe('auto')
    expect(mainStyle.getPropertyValue('padding-inline')).toBe('1rem')
  })
})
