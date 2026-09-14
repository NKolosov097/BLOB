import { client } from '../api/generated/client.gen'

export async function useBlobApi<T>(path: string): Promise<T> {
  if (import.meta.server) {
    return blobApiFetch<T>(path)
  }

  return client.get<{ 200: T }, unknown, true, 'data'>({
    baseUrl: useRuntimeConfig().public.apiBase,
    responseStyle: 'data',
    throwOnError: true,
    url: path,
  })
}
