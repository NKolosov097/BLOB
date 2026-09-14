// @vitest-environment node

import { context, propagation, trace } from '@opentelemetry/api'
import { createRequire } from 'node:module'
import { afterEach, expect, it, vi } from 'vitest'

vi.mock('nitropack/runtime', () => ({ defineNitroPlugin: (plugin: unknown) => plugin }))
const nuxtRequire = createRequire(import.meta.resolve('nuxt/package.json'))
const { createHooks } = nuxtRequire('hookable')
const exporterRequire = createRequire(import.meta.resolve('@opentelemetry/exporter-trace-otlp-grpc'))
const grpc = exporterRequire('@grpc/grpc-js')

afterEach(() => {
  trace.disable()
  context.disable()
  propagation.disable()
  vi.unstubAllEnvs()
  vi.restoreAllMocks()
  vi.resetModules()
})

it('does not initialize telemetry when the endpoint is empty', async () => {
  vi.stubEnv('OTEL_EXPORTER_OTLP_ENDPOINT', '')
  vi.stubEnv('OTEL_EXPORTER_OTLP_TRACES_ENDPOINT', 'http://127.0.0.1:1')
  const hooks = createHooks()
  const { default: plugin } = await import('../server/plugins/otel')
  await plugin({ hooks } as Parameters<typeof plugin>[0])
  const span = trace.getTracer('test').startSpan('disabled')
  expect(span.isRecording()).toBe(false)
  span.end()
  await hooks.callHook('close')
})

it.each([false, true])('starts one SDK and closes with safe diagnostics (collector fails: %s)', async collectorFails => {
  const packets: Buffer[] = []
  const receiver = new grpc.Server()
  receiver.addService({
    export: {
      path: '/opentelemetry.proto.collector.trace.v1.TraceService/Export',
      requestStream: false,
      responseStream: false,
      requestDeserialize: (value: Buffer) => value,
      responseSerialize: (value: Buffer) => value,
    },
  }, {
    export(call: { request: Buffer }, callback: (error: Error | null, response?: Buffer) => void) {
      packets.push(call.request)
      if (collectorFails) {
        callback(Object.assign(new Error('collector-secret: https://credential-secret@collector.invalid?token=signed-secret'), { code: grpc.status.INVALID_ARGUMENT }))
        return
      }
      callback(null, Buffer.alloc(0))
    },
  })
  const port = await new Promise<number>((resolve, reject) => receiver.bindAsync('127.0.0.1:0', grpc.ServerCredentials.createInsecure(), (error: Error | null, port: number) => error ? reject(error) : resolve(port)))
  vi.stubEnv('OTEL_EXPORTER_OTLP_ENDPOINT', `http://127.0.0.1:${port}`)
  vi.stubEnv('OTEL_RESOURCE_ATTRIBUTES', 'private=resource-secret')
  vi.stubEnv('OTEL_SERVICE_NAME', 'blob-web')
  const hooks = createHooks()
  const registration = vi.spyOn(hooks, 'hook')
  const diagnostics = vi.spyOn(console, 'error').mockImplementation(() => {})
  try {
    const { default: plugin } = await import('../server/plugins/otel')
    await plugin({ hooks } as Parameters<typeof plugin>[0])
    await plugin({ hooks } as Parameters<typeof plugin>[0])
    await trace.getTracer('test').startActiveSpan('ssr', async span => {
      expect(span.isRecording()).toBe(true)
      expect(span.spanContext().traceFlags & 1).toBe(1)
      span.end()
    })
    await hooks.callHook('close')
    expect(registration.mock.calls.filter(([name]) => name === 'close')).toHaveLength(1)
    expect(packets).toHaveLength(1)
    expect(packets[0]?.includes(Buffer.from('blob-web'))).toBe(true)
    expect(packets[0]?.includes(Buffer.from('resource-secret'))).toBe(false)
    if (collectorFails) {
      expect(diagnostics).toHaveBeenCalledOnce()
      const event = JSON.parse(String(diagnostics.mock.calls[0]?.[0]))
      expect(Object.keys(event).sort()).toEqual(['event', 'level', 'service', 'timestamp'])
      expect(event).toMatchObject({ event: 'telemetry_failure', level: 'ERROR', service: 'blob-web' })
      expect(JSON.stringify(diagnostics.mock.calls)).not.toMatch(/secret|collector\.invalid/)
    } else {
      expect(diagnostics).not.toHaveBeenCalled()
    }
  } finally {
    receiver.forceShutdown()
  }
})
