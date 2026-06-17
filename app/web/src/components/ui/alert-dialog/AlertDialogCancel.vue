<script setup lang="ts">
import type { AlertDialogCancelProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import type { ButtonVariants } from '@/components/ui/button'
import { reactiveOmit } from "@vueuse/core"
import { AlertDialogCancel } from "reka-ui"
import { cn } from "@/lib/utils"
import { buttonVariants } from '@/components/ui/button'

defineOptions({
  inheritAttrs: false,
})

const props = withDefaults(
  defineProps<AlertDialogCancelProps & {
    class?: HTMLAttributes["class"]
    variant?: ButtonVariants["variant"]
    size?: ButtonVariants["size"]
  }>(),
  {
    variant: "outline",
    size: "default",
  },
)

const delegatedProps = reactiveOmit(props, "class", "variant", "size", "asChild")
</script>

<template>
  <AlertDialogCancel
    as-child
    data-slot="alert-dialog-cancel"
    v-bind="delegatedProps"
  >
    <button
      v-bind="$attrs"
      :class="cn(buttonVariants({ variant, size }), props.class)"
    >
      <slot />
    </button>
  </AlertDialogCancel>
</template>
