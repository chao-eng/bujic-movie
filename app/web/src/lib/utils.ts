import type { ClassValue } from "clsx"
import { clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * reka-ui 的 SelectItem 不接受空字符串 value：空串是「清空选择、显示占位符」
 * 的保留值，传进去会直接抛错。但很多下拉里「不指定 / 自动识别 / 全部」本身就是
 * 一个有意义的选项，所以统一用这个哨兵值表示「空」。
 */
export const EMPTY_SELECT_VALUE = '__empty__'

/** 下拉回填：把哨兵值还原成空字符串。 */
export function fromSelectValue(value: unknown): string {
  return value === EMPTY_SELECT_VALUE ? '' : String(value ?? '')
}

/** 下拉存储：把空字符串换成哨兵值。 */
export function toSelectValue(value: string | null | undefined): string {
  return value ? value : EMPTY_SELECT_VALUE
}

/**
 * 复制文本到剪贴板。优先使用异步 Clipboard API（需安全上下文 HTTPS/localhost），
 * 不可用或失败时回退到隐藏 textarea + execCommand（兼容 http://IP 等非安全来源）。
 * 返回是否成功。
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // fall through to legacy path
  }

  try {
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.setAttribute('readonly', '')
    textarea.style.position = 'fixed'
    textarea.style.top = '-9999px'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(textarea)
    return ok
  } catch {
    return false
  }
}
