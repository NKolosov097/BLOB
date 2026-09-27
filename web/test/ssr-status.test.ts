// @vitest-environment node

import { $fetch, setup } from '@nuxt/test-utils/e2e'
import { mkdtempSync, rmSync } from 'node:fs'
import { createServer } from 'node:http'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterAll, beforeEach, describe, expect, it } from 'vitest'

let requests = 0
const cacheDir = mkdtempSync(join(tmpdir(), 'blob-ssr-vite-'))
const api = createServer((request, response) => {
  if (request.method === 'GET' && request.url === '/api/v1/health/ready') {
    requests++
    response.writeHead(200, { 'content-type': 'application/json' })
    response.end(JSON.stringify({ status: 'ok' }))
    return
  }

  response.writeHead(404).end()
})

await new Promise<void>((resolve, reject) => {
  api.once('error', reject)
  api.listen(0, '127.0.0.1', resolve)
})

const address = api.address()
if (!address || typeof address === 'string') throw new Error('Mock API did not bind to TCP')

afterAll(() => new Promise<void>((resolve, reject) => {
  api.close(error => {
    rmSync(cacheDir, { force: true, recursive: true })
    error ? reject(error) : resolve()
  })
}))

await setup({
  browser: false,
  env: { NUXT_API_INTERNAL_BASE: `http://127.0.0.1:${address.port}/api/v1` },
  nuxtConfig: { vite: { cacheDir } },
  rootDir: process.cwd(),
})

beforeEach(() => {
  requests = 0
})

describe('production SSR status page', () => {
  it('renders readiness from the internal API', async () => {
    const html = await $fetch<string>('/')

    expect(requests).toBe(1)
    expect(html).toContain('Готов')
    expect(html).not.toContain('blobApiFetch is not defined')
  })

  it.each([
    ['/en', 'en-US', 'ltr', 'Service status', 'Ready'],
    ['/ar', 'ar', 'rtl', 'حالة الخدمة', 'جاهز'],
  ])('renders localized readiness at %s', async (path, lang, dir, heading, ready) => {
    const html = await $fetch<string>(path)

    expect(requests).toBe(1)
    const documentTag = html.match(/<html\b[^>]*>/)?.[0]
    expect(documentTag).toContain(`lang="${lang}"`)
    expect(documentTag).toContain(`dir="${dir}"`)
    expect(html.match(/<h1[^>]*>.*?<\/h1>/s)?.[0]).toContain(heading)
    expect(html.match(/<p[^>]*role="status"[^>]*>.*?<\/p>/s)?.[0]).toContain(ready)
  })
})
