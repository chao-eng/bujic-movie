<script setup lang="ts">
/**
 * 设置页每个分区的标题行：左边标题与说明，右边放主操作。
 * 只是重复结构的一次收口，不引入额外状态。
 */
defineProps<{ title: string; description?: string }>()
</script>

<template>
  <div class="flex flex-wrap items-center justify-between gap-4">
    <div class="min-w-0">
      <h2 class="text-lg font-semibold">{{ title }}</h2>
      <p v-if="description" class="text-muted-foreground truncate text-sm">{{ description }}</p>
    </div>
    <div class="flex shrink-0 flex-wrap items-center gap-2">
      <slot />
    </div>
  </div>
</template>
