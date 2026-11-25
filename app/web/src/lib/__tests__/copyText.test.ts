import { afterEach, describe, expect, it, vi } from 'vitest'
import { copyText } from '@/lib/utils'

/**
 * 复制到剪贴板。
 *
 * 背景：MCP 弹窗里两个复制按钮「提示成功但剪贴板没变」。原因是回退分支
 * （隐藏 textarea + execCommand）在弹层焦点陷阱下选区被清空，而
 * execCommand('copy') 依然返回 true——只看返回值就会误报成功。
 * 这里锁住两件事：内容真的写进 clipboardData 才算成功；
 * textarea 必须挂在弹层内部，避免焦点陷阱抢焦点。
 */

function stubExecCommand(impl: (el: HTMLTextAreaElement | null) => boolean) {
  let target: HTMLTextAreaElement | null = null
  const exec = vi.fn(() => {
    target = document.querySelector('textarea')
    return impl(target)
  })
  Object.defineProperty(document, 'execCommand', { value: exec, configurable: true, writable: true })
  return { exec, getTarget: () => target }
}

afterEach(() => {
  document.body.innerHTML = ''
  vi.restoreAllMocks()
  // @ts-expect-error jsdom 默认没有 execCommand
  delete document.execCommand
})

describe('copyText', () => {
  it('回退分支把内容写进 clipboardData 并返回 true', async () => {
    let written = ''
    stubExecCommand(() => {
      const event = new Event('copy', { bubbles: true, cancelable: true }) as ClipboardEvent
      Object.defineProperty(event, 'clipboardData', {
        value: { setData: (_type: string, value: string) => { written = value } },
      })
      document.dispatchEvent(event)
      return true
    })

    // jsdom 是非安全上下文，navigator.clipboard 不存在，会走回退分支
    await expect(copyText('bmk_test')).resolves.toBe(true)
    expect(written).toBe('bmk_test')
  })

  it('execCommand 返回 true 但没有 copy 事件时不算成功', async () => {
    stubExecCommand(() => true)
    await expect(copyText('bmk_test')).resolves.toBe(false)
  })

  it('弹层打开时 textarea 挂在弹层内部', async () => {
    document.body.innerHTML = '<div data-slot="dialog-content"><input /></div>'
    const dialog = document.querySelector<HTMLElement>('[data-slot="dialog-content"]')!
    let parentTag = ''
    stubExecCommand(() => {
      parentTag = document.querySelector('textarea')?.parentElement?.getAttribute('data-slot') ?? ''
      return false
    })

    await copyText('x')
    expect(parentTag).toBe('dialog-content')
    // 用完必须移除，避免弹层里残留节点
    expect(dialog.querySelector('textarea')).toBeNull()
  })
})
