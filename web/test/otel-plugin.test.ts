// @vitest-environment node

import { context, diag, propagation, trace } from '@opentelemetry/api'
import { createRequire } from 'node:module'
import { inspect } from 'node:util'
import { afterEach, expect, it, vi } from 'vitest'

vi.mock('nitropack/runtime', () => ({ defineNitroPlugin: (plugin: unknown) => plugin }))
const nuxtRequire = createRequire(import.meta.resolve('nuxt/package.json'))
const { createHooks } = nuxtRequire('hookable')
const exporterRequire = createRequire(import.meta.resolve('@opentelemetry/exporter-trace-otlp-grpc'))
const grpc = exporterRequire('@grpc/grpc-js')
const transformerRequire = createRequire(exporterRequire.resolve('@opentelemetry/otlp-transformer'))
const exportResponse = transformerRequire('./generated/root').opentelemetry.proto.collector.trace.v1.ExportTraceServiceResponse
const sdkRequire = createRequire(import.meta.resolve('@opentelemetry/sdk-node'))
const { logs } = sdkRequire('@opentelemetry/api-logs')

afterEach(() => {
  trace.disable()
  context.disable()
  propagation.disable()
  logs.disable()
  diag.disable()
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

it.each(['success', 'error', 'partial'])('starts one SDK and closes with safe diagnostics (collector: %s)', async mode => {
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
      if (mode === 'error') {
        callback(Object.assign(new Error('collector-secret: https://credential-secret@collector.invalid?token=signed-secret'), { code: grpc.status.INVALID_ARGUMENT }))
        return
      }
      if (mode === 'partial') {
        callback(null, Buffer.from(exportResponse.encode({ partialSuccess: { rejectedSpans: 1, errorMessage: 'partial-secret: https://credential-secret@collector.invalid?token=query-secret' } }).finish()))
        return
      }
      callback(null, Buffer.alloc(0))
    },
  })
  const port = await new Promise<number>((resolve, reject) => receiver.bindAsync('127.0.0.1:0', grpc.ServerCredentials.createInsecure(), (error: Error | null, port: number) => error ? reject(error) : resolve(port)))
  vi.stubEnv('OTEL_EXPORTER_OTLP_ENDPOINT', `http://127.0.0.1:${port}`)
  vi.stubEnv('OTEL_RESOURCE_ATTRIBUTES', 'private=resource-secret')
  vi.stubEnv('OTEL_SERVICE_NAME', 'blob-web')
  if (mode === 'partial') vi.stubEnv('OTEL_LOG_LEVEL', 'DEBUG')
  const hooks = createHooks()
  const registration = vi.spyOn(hooks, 'hook')
  const diagnostics = ['error', 'warn', 'info', 'debug', 'log'].map(method => vi.spyOn(console, method as 'error').mockImplementation(() => {}))
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
    const output = diagnostics.flatMap(logger => logger.mock.calls)
    expect(inspect(output)).not.toMatch(/secret|collector\.invalid/)
    if (mode !== 'success') {
      expect(output).toHaveLength(1)
      const event = JSON.parse(String(output[0]?.[0]))
      expect(Object.keys(event).sort()).toEqual(['event', 'level', 'service', 'timestamp'])
      expect(event).toMatchObject({ event: 'telemetry_failure', level: 'ERROR', service: 'blob-web' })
    } else {
      expect(output).toEqual([])
    }
  } finally {
    receiver.forceShutdown()
  }
})

it.each([
  ['exporter authority', 'https://credential-secret@[]/?token=query-secret'],
  ['exporter port', 'https://credential-secret@collector.invalid:bad/?token=query-secret'],
  ['SDK start', 'http://127.0.0.1:1'],
])('disables telemetry safely for malformed endpoint in %s', async (stage, endpoint) => {
  vi.stubEnv('OTEL_EXPORTER_OTLP_ENDPOINT', endpoint)
  vi.stubEnv('OTEL_LOG_LEVEL', 'DEBUG')
  if (stage === 'SDK start') {
    vi.stubEnv('OTEL_METRICS_EXPORTER', 'otlp')
    vi.stubEnv('OTEL_EXPORTER_OTLP_METRICS_PROTOCOL', 'grpc')
    vi.stubEnv('OTEL_EXPORTER_OTLP_METRICS_ENDPOINT', 'https://credential-secret@[]/?token=query-secret')
  }
  const diagnostics = ['error', 'warn', 'info', 'debug', 'log'].map(method => vi.spyOn(console, method as 'error').mockImplementation(() => {}))
  const hooks = createHooks()
  const { default: plugin } = await import('../server/plugins/otel')
  await expect(Promise.resolve().then(() => plugin({ hooks } as Parameters<typeof plugin>[0]))).resolves.toBeUndefined()
  const span = trace.getTracer('test').startSpan('disabled')
  expect(span.isRecording()).toBe(false)
  span.end()
  await hooks.callHook('close')
  const output = diagnostics.flatMap(logger => logger.mock.calls)
  expect(JSON.stringify(output)).not.toMatch(/secret|collector\.invalid|Error:|stack/)
  expect(output).toHaveLength(1)
  const event = JSON.parse(String(output[0]?.[0]))
  expect(Object.keys(event).sort()).toEqual(['event', 'level', 'service', 'timestamp'])
  expect(event).toMatchObject({ event: 'telemetry_failure', level: 'ERROR', service: 'blob-web' })
})
