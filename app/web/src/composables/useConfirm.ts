import { ref } from 'vue'

interface ConfirmOptions {
  title?: string
  message: string
  confirmText?: string
  cancelText?: string
}

interface ConfirmState {
  open: boolean
  title: string
  message: string
  confirmText: string
  cancelText: string
  resolve: (value: boolean) => void
}

export const confirmState = ref<ConfirmState | null>(null)

export function useConfirm() {
  const confirm = (options: ConfirmOptions | string) => {
    return new Promise<boolean>((resolve) => {
      const message = typeof options === 'string' ? options : options.message
      const title = typeof options === 'object' && options.title ? options.title : '确认操作'
      const confirmText = typeof options === 'object' && options.confirmText ? options.confirmText : '确定'
      const cancelText = typeof options === 'object' && options.cancelText ? options.cancelText : '取消'

      confirmState.value = {
        open: true,
        title,
        message,
        confirmText,
        cancelText,
        resolve: (value: boolean) => {
          if (confirmState.value) {
            confirmState.value.open = false
          }
          resolve(value)
        }
      }
    })
  }

  return { confirm }
}
