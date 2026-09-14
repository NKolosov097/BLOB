export default defineNuxtConfig({
  compatibilityDate: '2026-09-14',
  css: ['~/assets/css/main.css'],
  modules: ['@nuxtjs/i18n'],
  runtimeConfig: {
    apiInternalBase: process.env.API_INTERNAL_BASE ?? 'http://127.0.0.1:8080/api/v1',
    public: { apiBase: process.env.NUXT_PUBLIC_API_BASE ?? '/api/v1' },
  },
  i18n: {
    defaultLocale: 'ru',
    strategy: 'prefix_except_default',
    locales: [
      { code: 'ru', language: 'ru-RU', file: 'ru.json', dir: 'ltr' },
      { code: 'en', language: 'en-US', file: 'en.json', dir: 'ltr' },
      { code: 'ar', language: 'ar', file: 'ar.json', dir: 'rtl' },
    ],
  },
})
