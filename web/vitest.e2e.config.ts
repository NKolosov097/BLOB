import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    include: ['test/ssr-status.test.ts'],
  },
})
