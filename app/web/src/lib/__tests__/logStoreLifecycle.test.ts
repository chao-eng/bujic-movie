import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * 日志通道的连接生命周期。
 *
 * 这里模拟 fetchEventSource：每次调用代表一条长连接，只有 signal 被 abort
 * 才算结束。用「当前活跃连接数」判断有没有泄漏—— 手动重连若干次后如果
 * 活跃连接数超过 1，就说明旧的重连循环没有退出。
 */

const live = new Set<number>()
let opened = 0

vi.mock('@microsoft/fetch-event-source', () => ({
  fetchEventSource: async (_url: string, opts: { signal: AbortSignal }) => {
    const id = ++opened
    live.add(id)
    await new Promise<void>((resolve) => {
      if (opts.signal.aborted) return resolve()
      opts.signal.addEventListener('abort', () => resolve())
    })
    live.delete(id)
  },
}))

import { useLogStore } from '@/stores/logStore'

const settle = () => new Promise((resolve) => setTimeout(resolve, 20))

describe('日志通道生命周期', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    live.clear()
    opened = 0
    // loadRecent 走原生 fetch，这里让它失败即可（历史只是锦上添花）。
    vi.stubGlobal('fetch', async () => ({ ok: false, json: async () => ({}) }))
  })

  it('init 只建立一条连接', async () => {
    const store = useLogStore()
    void store.init()
    await settle()
    expect(live.size).toBe(1)
    store.disconnect()
  })

  it('init 重复调用不会叠加连接', async () => {
    const store = useLogStore()
    void store.init()
    void store.init()
    void store.init()
    await settle()
    expect(live.size).toBe(1)
    store.disconnect()
  })

  it('手动重连不会留下并行的旧连接', async () => {
    const store = useLogStore()
    void store.init()
    await settle()

    await store.reconnect()
    await settle()
    await store.reconnect()
    await settle()

    // 关键断言：无论重连多少次，同时只有一条连接。
    expect(live.size).toBe(1)
    store.disconnect()
  })

  it('disconnect 之后不再重连，也不残留连接', async () => {
    const store = useLogStore()
    void store.init()
    await settle()
    store.disconnect()
    await settle()

    expect(live.size).toBe(0)
    // 断线后不应自行恢复连接。
    await new Promise((resolve) => setTimeout(resolve, 1200))
    expect(live.size).toBe(0)
  })
})
