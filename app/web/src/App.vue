<script setup lang="ts">
import { Toaster } from '@/components/ui/sonner'
import GlobalConfirm from '@/components/ui/confirm/GlobalConfirm.vue'
</script>

<template>
  <router-view />
  <Toaster richColors />
  <GlobalConfirm />
</template>
