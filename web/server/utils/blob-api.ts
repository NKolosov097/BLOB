import { context, propagation, SpanKind, trace } from '@opentelemetry/api'
import { useRuntimeConfig } from 'nitropack/runtime'

export function blobApiFetch<T>(path: string): Promise<T> {
  return trace.getTracer('blob/web').startActiveSpan('blob.api.fetch', { kind: SpanKind.CLIENT }, async span => {
    span.setAttribute('operation', 'fetch')
    try {
      const headers = new Headers()
      propagation.inject(context.active(), headers, {
        set(carrier, key, value) {
          if (key === 'traceparent') carrier.set(key, value)
        },
      })
      const result = await $fetch<T>(path, { baseURL: useRuntimeConfig().apiInternalBase, headers })
      span.setAttribute('success', true)
      return result
    } catch (error) {
      span.setAttribute('success', false)
      throw error
    } finally {
      span.end()
    }
  })
}
