import { context, diag, DiagLogLevel, propagation, trace } from '@opentelemetry/api'
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-grpc'
import { NodeSDK, core, resources, tracing } from '@opentelemetry/sdk-node'
import { defineNitroPlugin } from 'nitropack/runtime'

let sdk: NodeSDK | undefined

export default defineNitroPlugin(async nitro => {
  const endpoint = process.env.OTEL_EXPORTER_OTLP_ENDPOINT
  if (!endpoint || sdk) return

  const service = process.env.OTEL_SERVICE_NAME || 'blob-web'
  const reportFailure = () => console.error(JSON.stringify({ timestamp: new Date().toISOString(), level: 'ERROR', service, event: 'telemetry_failure' }))
  core.setGlobalErrorHandler(reportFailure)
  // NodeSDK otherwise replaces our safe logger with a raw console logger.
  delete process.env.OTEL_LOG_LEVEL
  diag.setLogger({ error: reportFailure, warn: reportFailure, info: reportFailure, debug: reportFailure, verbose: reportFailure }, { logLevel: DiagLogLevel.WARN, suppressOverrideMessage: true })
  let exporter: OTLPTraceExporter | undefined
  const shutdown = async () => {
    let timeout: ReturnType<typeof setTimeout> | undefined
    try {
      await Promise.race([
        sdk ? sdk.shutdown().catch(async () => {
          reportFailure()
          await exporter?.shutdown().catch(reportFailure)
        }) : exporter?.shutdown().catch(reportFailure),
        new Promise<void>(resolve => { timeout = setTimeout(() => { reportFailure(); resolve() }, 5000) }),
      ])
    } finally {
      clearTimeout(timeout)
    }
  }
  try {
    exporter = new OTLPTraceExporter({ url: endpoint, timeoutMillis: 5000 })
    sdk = new NodeSDK({
      autoDetectResources: false,
      resource: resources.resourceFromAttributes({ 'service.name': service }),
      logRecordProcessors: [],
      sampler: new tracing.AlwaysOnSampler(),
      textMapPropagator: new core.W3CTraceContextPropagator(),
      spanProcessors: [new tracing.BatchSpanProcessor(exporter, { exportTimeoutMillis: 5000 })],
    })
    sdk.start()
  } catch {
    reportFailure()
    trace.disable()
    context.disable()
    propagation.disable()
    await shutdown()
    sdk = undefined
    return
  }
  nitro.hooks.hook('close', shutdown)
})
