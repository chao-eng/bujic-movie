<script setup lang="ts">
import type { PrimitiveProps } from "reka-ui"
import type { HTMLAttributes } from "vue"
import { SwitchRoot, SwitchThumb, useForwardPropsEmits } from "reka-ui"
import { cn } from "@/lib/utils"

interface Props extends PrimitiveProps {
  modelValue?: boolean
  disabled?: boolean
  class?: HTMLAttributes["class"]
  thumbClass?: HTMLAttributes["class"]
  id?: string
}

const props = withDefaults(defineProps<Props>(), {
  as: "button",
  modelValue: false,
})

const emits = defineEmits<{
  (e: "update:modelValue", payload: boolean): void
}>()

const forwarded = useForwardPropsEmits(props, emits)
</script>

<template>
  <SwitchRoot
    v-bind="forwarded"
    :id="id"
    data-slot="switch"
    :class="cn(
      'peer inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full border-2 border-transparent transition-colors outline-none focus-visible:ring-2 focus-visible:ring-amber-500/40 disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:bg-amber-500 data-[state=unchecked]:bg-slate-700',
      props.class
    )"
  >
    <SwitchThumb
      data-slot="switch-thumb"
      :class="cn(
        'pointer-events-none block h-4 w-4 rounded-full bg-white shadow-lg ring-0 transition-transform data-[state=checked]:translate-x-4 data-[state=unchecked]:translate-x-0 data-[state=checked]:bg-slate-950',
        props.thumbClass
      )"
    />
  </SwitchRoot>
</template>
