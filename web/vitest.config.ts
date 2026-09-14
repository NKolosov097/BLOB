import { defineVitestConfig } from '@nuxt/test-utils/config'
import { configDefaults } from 'vitest/config'

export default defineVitestConfig({
  test: {
    environment: 'nuxt',
    exclude: [...configDefaults.exclude, 'test/ssr-status.test.ts'],
  },
})
