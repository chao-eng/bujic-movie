<script setup lang="ts">
import { Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

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
    <DialogContent
      :class="
        cn(
          props.wide ? 'sm:max-w-4xl' : 'sm:max-w-2xl',
          // 内容可能远超视口（站点表单要滚很久）。限制高度并让整个弹层可滚动，
          // 否则 DialogContent 固定居中会把上下两端顶到屏幕外，用户点不到也滚不动。
          'max-h-[calc(100dvh-2rem)] overflow-y-auto overscroll-contain',
        )
      "
    >
      <DialogHeader>
        <DialogTitle>{{ props.title }}</DialogTitle>
        <DialogDescription v-if="props.subtitle">{{ props.subtitle }}</DialogDescription>
      </DialogHeader>
      <slot />
    </DialogContent>
  </Dialog>
</template>
