<script setup lang="ts">
import type { AlertDialogActionProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import type { ButtonVariants } from '@/components/ui/button'
import { reactiveOmit } from "@vueuse/core"
import { AlertDialogAction } from "reka-ui"
import { cn } from "@/lib/utils"
import { buttonVariants } from '@/components/ui/button'

defineOptions({
  inheritAttrs: false,
})

const props = withDefaults(
  defineProps<AlertDialogActionProps & {
    class?: HTMLAttributes["class"]
    variant?: ButtonVariants["variant"]
    size?: ButtonVariants["size"]
  }>(),
  {
    variant: "default",
    size: "default",
  },
)

const delegatedProps = reactiveOmit(props, "class", "variant", "size", "asChild")
</script>

<template>
  <AlertDialogAction
    as-child
    data-slot="alert-dialog-action"
    v-bind="delegatedProps"
  >
    <button
      v-bind="$attrs"
      :class="cn(buttonVariants({ variant, size }), props.class)"
    >
      <slot />
    </button>
  </AlertDialogAction>
</template>
