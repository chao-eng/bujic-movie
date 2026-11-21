<script setup lang="ts">
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

/**
 * 通用弹层：模板选择、添加站点这类需要集中注意力的流程放进弹层。
 * 关闭按钮、ESC、点遮罩都由 Dialog 组件提供，背景滚动会被锁住。
 */
const props = defineProps<{ title: string; subtitle?: string; wide?: boolean }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const open = defineModel<boolean>('open', { default: true })
</script>

<template>
  <Dialog
    :open="open"
    @update:open="
      (value) => {
        open = value
        if (!value) emit('close')
      }
    "
  >
    <DialogContent :class="props.wide ? 'sm:max-w-4xl' : 'sm:max-w-2xl'">
      <DialogHeader>
        <DialogTitle>{{ props.title }}</DialogTitle>
        <DialogDescription v-if="props.subtitle">{{ props.subtitle }}</DialogDescription>
      </DialogHeader>
      <slot />
    </DialogContent>
  </Dialog>
</template>
