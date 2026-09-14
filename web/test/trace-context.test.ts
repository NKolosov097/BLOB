// @vitest-environment node

import { context, propagation, trace } from '@opentelemetry/api'
import { NodeSDK, core, tracing } from '@opentelemetry/sdk-node'
import { createServer, type IncomingHttpHeaders } from 'node:http'
import { createRequire } from 'node:module'
import { afterAll, expect, it, vi } from 'vitest'
import { blobApiFetch } from '../server/utils/blob-api'

const config = vi.hoisted(() => ({ apiInternalBase: '' }))
vi.mock('nitropack/runtime', () => ({ useRuntimeConfig: () => config }))

// Use the same installed ofetch that Nuxt provides as its server $fetch global.
const nuxtRequire = createRequire(import.meta.resolve('nuxt/package.json'))
vi.stubGlobal('$fetch', nuxtRequire('ofetch').$fetch)
const exporter = new tracing.InMemorySpanExporter()
const sdk = new NodeSDK({
  autoDetectResources: false,
  logRecordProcessors: [],
  spanProcessors: [new tracing.SimpleSpanProcessor(exporter)],
  sampler: new tracing.AlwaysOnSampler(),
  textMapPropagator: new core.CompositePropagator({ propagators: [new core.W3CTraceContextPropagator(), new core.W3CBaggagePropagator()] }),
})
sdk.start()
let received: { url?: string; headers: IncomingHttpHeaders } | undefined
const receiver = createServer((request, response) => {
  received = { url: request.url, headers: request.headers }
  response.writeHead(200, { 'content-type': 'application/json' }).end('{"status":"ok"}')
})
await new Promise<void>(resolve => receiver.listen(0, '127.0.0.1', resolve))
const address = receiver.address()
if (!address || typeof address === 'string') throw new Error('Receiver did not bind')
config.apiInternalBase = `http://127.0.0.1:${address.port}/api/v1`

afterAll(async () => {
  await sdk.shutdown()
  trace.disable()
  propagation.disable()
  context.disable()
  vi.unstubAllGlobals()
  await new Promise<void>((resolve, reject) => receiver.close(error => error ? reject(error) : resolve()))
})

it('sends the active child trace to the internal API without cookies, query additions, or baggage', async () => {
  let parentTrace = ''
  let parentSpan = ''
  const baggageContext = propagation.setBaggage(context.active(), propagation.createBaggage({ email: { value: 'baggage-secret' } }))
  await context.with(baggageContext, () => trace.getTracer('test').startActiveSpan('ssr', async parent => {
    try {
      expect(parent.isRecording()).toBe(true)
      expect(parent.spanContext().traceFlags & 1).toBe(1)
      parentTrace = parent.spanContext().traceId
      parentSpan = parent.spanContext().spanId
      expect(await blobApiFetch('/health/ready')).toEqual({ status: 'ok' })
    } finally {
      parent.end()
    }
  }))

  expect(received?.url).toBe('/api/v1/health/ready')
  expect(received?.headers.cookie).toBeUndefined()
  expect(received?.headers.authorization).toBeUndefined()
  expect(received?.headers.baggage).toBeUndefined()
  expect(received?.headers.traceparent).toMatch(/^00-[a-f0-9]{32}-[a-f0-9]{16}-01$/)
  const [, traceID, spanID] = String(received?.headers.traceparent).split('-')
  expect(traceID).toBe(parentTrace)
  expect(spanID).not.toBe(parentSpan)
  const child = exporter.getFinishedSpans().find(span => span.name === 'blob.api.fetch')
  expect(child?.spanContext().spanId).toBe(spanID)
  expect(child?.parentSpanContext?.spanId).toBe(parentSpan)
  expect(child?.attributes).toEqual({ operation: 'fetch', success: true })
  expect(child?.events).toEqual([])
  expect(JSON.stringify(exporter.getFinishedSpans().map(span => ({ attributes: span.attributes, events: span.events })))).not.toContain('baggage-secret')
})
