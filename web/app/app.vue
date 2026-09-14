<script setup lang="ts">
const { localeProperties, t } = useI18n()

useHead(() => ({
  htmlAttrs: {
    dir: localeProperties.value.dir,
    lang: localeProperties.value.language,
  },
  title: `${t('appName')} — ${t('statusTitle')}`,
}))
</script>

<template>
  <NuxtPage />
</template>
