<script setup lang="ts">
import type { HealthResponse } from '../api/generated/types.gen'

const { t } = useI18n()
const { data, error } = await useAsyncData('readiness', () =>
  useBlobApi<HealthResponse>('/health/ready'),
)
const ready = computed(() => !error.value && data.value?.status === 'ok')
</script>

<template>
  <main>
    <h1>{{ t('statusTitle') }}</h1>
    <p data-test="api-status" role="status">
      {{ ready ? t('statusReady') : t('statusUnavailable') }}
    </p>
  </main>
</template>
