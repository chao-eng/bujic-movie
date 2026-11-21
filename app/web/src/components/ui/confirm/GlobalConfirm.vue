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
    <AlertDialogContent v-if="confirmState">
      <AlertDialogHeader>
        <AlertDialogTitle>{{ confirmState.title }}</AlertDialogTitle>
        <AlertDialogDescription>
          {{ confirmState.message }}
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <Button variant="outline" @click="confirmState.resolve(false)">
          {{ confirmState.cancelText }}
        </Button>
        <Button @click="confirmState.resolve(true)">
          {{ confirmState.confirmText }}
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
