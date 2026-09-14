export function blobApiFetch<T>(path: string): Promise<T> {
  return $fetch<T>(path, { baseURL: useRuntimeConfig().apiInternalBase })
}
