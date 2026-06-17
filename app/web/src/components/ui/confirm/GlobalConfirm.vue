<script setup lang="ts">
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { confirmState } from '@/composables/useConfirm'
</script>

<template>
  <AlertDialog :open="!!confirmState?.open" @update:open="(val) => { if (!val) confirmState?.resolve(false) }">
    <AlertDialogContent v-if="confirmState" class="bg-slate-900 border-slate-800">
      <AlertDialogHeader>
        <AlertDialogTitle class="text-slate-100">{{ confirmState.title }}</AlertDialogTitle>
        <AlertDialogDescription class="text-slate-400">
          {{ confirmState.message }}
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <Button @click="confirmState.resolve(false)" variant="outline" class="border-slate-800 text-slate-300 hover:bg-slate-800">
          {{ confirmState.cancelText }}
        </Button>
        <Button @click="confirmState.resolve(true)" class="bg-amber-500 hover:bg-amber-600 text-slate-950 font-bold">
          {{ confirmState.confirmText }}
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
