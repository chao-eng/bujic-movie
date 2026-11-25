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
// 显式标注为 string：不加会被推断成字面量类型 '__empty__'，在模板里与
// `number | ''` 之类的值比较时 TS 会判定两者不可能相等而报错。
export const EMPTY_SELECT_VALUE: string = '__empty__'

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
 *
 * 回退分支有两个坑，都会造成「提示复制成功、剪贴板没变」：
 * 1. 弹层（reka-ui Dialog）有焦点陷阱，textarea 若挂在 document.body 上，
 *    一聚焦就被抢回弹层，选区被清空，execCommand('copy') 却仍返回 true。
 *    所以 textarea 挂进当前弹层内部。
 * 2. 不依赖选区：在 copy 事件里直接写 clipboardData，只有真正写入才算成功。
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // 非安全上下文或文档未获焦点时会被拒绝，继续走回退分支
  }

  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.setAttribute('readonly', '')
  textarea.style.position = 'fixed'
  textarea.style.top = '0'
  textarea.style.left = '0'
  textarea.style.width = '1px'
  textarea.style.height = '1px'
  textarea.style.padding = '0'
  textarea.style.border = 'none'
  textarea.style.opacity = '0'
  textarea.style.pointerEvents = 'none'

  // 弹层存在时挂到弹层里，避免焦点陷阱把焦点抢走
  const host =
    document.querySelector<HTMLElement>(
      '[data-slot="dialog-content"], [data-slot="alert-dialog-content"], [role="dialog"]',
    ) ?? document.body
  host.appendChild(textarea)

  let fired = false
  let handled = false
  const onCopy = (event: ClipboardEvent) => {
    fired = true
    if (!event.clipboardData) return
    event.clipboardData.setData('text/plain', text)
    event.preventDefault()
    handled = true
  }

  try {
    textarea.focus({ preventScroll: true })
    textarea.select()
    textarea.setSelectionRange(0, text.length)
    document.addEventListener('copy', onCopy, true)
    const ok = document.execCommand('copy')
    // 只有真把内容写进 clipboardData 才算成功：execCommand 在选区为空时也会返回 true
    return fired ? handled : ok
  } catch {
    return false
  } finally {
    document.removeEventListener('copy', onCopy, true)
    textarea.remove()
  }
}
