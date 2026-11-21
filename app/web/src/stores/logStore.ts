import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { fetchEventSource } from '@microsoft/fetch-event-source'
import { classifyLogEntry, classifyReady } from '@/lib/logStream'

/** 与服务端 LogEntry 对应。seq 是单调递增序号，用于断线续传。 */
export interface LogEntry {
  seq: number
  timestamp: string
  level: string
  message: string
}

type ConnectionState = 'idle' | 'connecting' | 'open' | 'reconnecting' | 'closed'

/** 内存里最多保留多少行，避免长时间挂着的页面无限增长。 */
const MAX_LINES = 500
/** 服务端心跳间隔 15s；超过这个时间没收到任何字节就认为连接已死。 */
const STALE_AFTER_MS = 40_000
/** 保活探测的检查间隔。 */
const WATCH_EVERY_MS = 10_000
/** 重连退避：1s 起，逐次翻倍，封顶 30s。 */
const backoff = (attempt: number) => Math.min(1000 * 2 ** (attempt - 1), 30_000)

const STREAM_PATH = '/api/v1/logs/stream'
const RECENT_PATH = '/api/v1/logs/recent'

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

/**
 * 日志流要求登录态（日志里有服务器路径、影片名等运行细节）。
 * fetch 与 fetchEventSource 都不会自动带 axios 的拦截器，这里自己补。
 */
function authHeaders(extra: Record<string, string> = {}): Record<string, string> {
  const token = localStorage.getItem('token')
  return token ? { ...extra, Authorization: `Bearer ${token}` } : extra
}

/**
 * 控制台日志通道。
 *
 * 用 SSE（EventSource 语义）而不是 WebSocket：日志是单向的，且服务端每条消息
 * 都带自增 id，重连时带上 Last-Event-ID 就能只补断线期间缺失的行。
 * 传输层是 @microsoft/fetch-event-source，比原生 EventSource 多三项必需能力：
 * 能读响应状态、能主动 abort、能自己决定重试节奏。
 *
 * 重试完全由本 store 掌控（onerror 直接抛错让请求结束，不交给库内建重试），
 * 因此「退避重连」和「假活探测」只有一条控制路径，不会互相打架。
 */
export const useLogStore = defineStore('logStore', () => {
  const logs = ref<LogEntry[]>([])
  const state = ref<ConnectionState>('idle')
  /** 最近一次收到任何字节（含心跳）的时间，用来判断连接是否假活。 */
  const lastBeatAt = ref(0)
  const reconnectAttempt = ref(0)

  const isConnected = computed(() => state.value === 'open')

  let abort: AbortController | null = null
  let watchTimer: ReturnType<typeof setInterval> | null = null
  /** 正在跑的重连循环；null 表示没有活动连接。 */
  let loop: Promise<void> | null = null
  /**
   * 每一轮循环的世代号。
   *
   * `stopped` 是共享变量，init() 会把它重置成 false，于是旧循环醒来后会
   * 以为自己仍然有效，与新循环并行跑下去（手动重连 N 次就留下 N+1 条连接）。
   * 世代号只增不减，循环判断它是否还是当前世代，过期就自行退出。
   */
  let generation = 0
  /** 本次连接是否为掉线后的重连；ready 判定服务端重启时要用。 */
  let isReconnect = false
  let lastSeq = 0
  /** 主动断开时置位，让重连循环退出。 */
  let stopped = false

  function pushLocal(level: string, message: string, seq = 0) {
    logs.value.push({
      seq,
      timestamp: new Date().toLocaleTimeString('zh-CN', { hour12: false }),
      level,
      message,
    })
    if (logs.value.length > MAX_LINES) logs.value.splice(0, logs.value.length - MAX_LINES)
  }

  function append(entry: LogEntry) {
    // 去重与「服务端重启导致序号重置」的判定都在纯函数里，有单测覆盖。
    const outcome = classifyLogEntry(lastSeq, entry.seq)
    lastSeq = outcome.nextSeq
    if (!outcome.accept) return
    if (outcome.restarted) {
      pushLocal('INFO', '检测到服务端已重启，日志序号已重置')
    }
    logs.value.push(entry)
    if (logs.value.length > MAX_LINES) logs.value.splice(0, logs.value.length - MAX_LINES)
  }

  /**
   * 拉取服务端当前的重放窗口。
   *
   * force=true 用于服务端重启后：旧进程留下的行已经没有意义，而且新旧进程
   * 的序号会从同一个值重新开始（都是 seq=1），只靠序号无法区分，必须整段换掉。
   */
  async function loadRecent(force = false) {
    try {
      const resp = await fetch(RECENT_PATH, { headers: authHeaders({ Accept: 'application/json' }) })
      if (!resp.ok) return
      const payload = await resp.json()
      // 已有内容（例如重连）就不重复灌入。
      if (!force && logs.value.length > 0) return
      const items = (payload?.data?.items ?? []) as LogEntry[]
      logs.value = []
      lastSeq = 0
      for (const item of items) append(item)
    } catch {
      // 忽略：SSE 连上后仍会补发历史。
    }
  }

  /**
   * 单次连接。正常返回表示流被服务端结束；抛错表示网络/握手失败。
   * onerror 里直接抛错，是为了绕过库内建重试，让我们自己的退避成为唯一路径。
   */
  async function openOnce(ctrl: AbortController) {
    await fetchEventSource(STREAM_PATH, {
      signal: ctrl.signal,
      headers: authHeaders(lastSeq > 0 ? { 'Last-Event-ID': String(lastSeq) } : {}),
      // 标签页在后台时也继续收，否则切回来会缺一段。
      openWhenHidden: true,

      async onopen(response) {
        if (response.ok && response.headers.get('content-type')?.includes('text/event-stream')) {
          state.value = 'open'
          lastBeatAt.value = Date.now()
          reconnectAttempt.value = 0
          return
        }
        throw new Error(`日志通道握手失败（HTTP ${response.status}）`)
      },

      onmessage(event) {
        lastBeatAt.value = Date.now()
        if (event.event === 'heartbeat') return
        if (event.event === 'ready') {
          state.value = 'open'
          // ready 带的是服务端当前最新序号；它大幅小于本地记录说明服务端重启过，
          // 必须重置本地序号，否则后续所有新日志都会被当成重复而丢掉。
          try {
            const ready = JSON.parse(event.data) as { last_seq?: number }
            const outcome = classifyReady(lastSeq, ready.last_seq ?? lastSeq, isReconnect)
            if (outcome.restarted) {
              lastSeq = outcome.nextSeq
              pushLocal('INFO', '检测到服务端已重启，已重新加载日志')
              // 旧进程的行与序号一并作废，从当前进程的快照重建。
              void loadRecent(true)
            }
          } catch {
            // ready 解析失败不影响日志接收。
          }
          return
        }
        if (event.event === 'log') {
          try {
            append(JSON.parse(event.data) as LogEntry)
          } catch {
            // 单行解析失败不影响后续。
          }
        }
      },

      onclose() {
        throw new Error('日志通道被服务端关闭')
      },

      onerror(err) {
        // 交给外层循环处理退避；return 一个数字会启用地内建重试，这里不要。
        throw err instanceof Error ? err : new Error(String(err))
      },
    })
  }

  /** 主循环：连上就不动，断了按退避重连，直到 disconnect()。 */
  async function runLoop(myGeneration: number) {
    // 只要世代号变了，本循环就作废：不重连、不改状态、直接退出。
    const stale = () => stopped || myGeneration !== generation
    while (!stale()) {
      const ctrl = new AbortController()
      abort = ctrl
      isReconnect = reconnectAttempt.value > 0
      try {
        await openOnce(ctrl)
      } catch {
        // 连接结束（服务端重启、网络中断、或被保活探测中止）。
      }
      abort = null
      if (stale()) break

      reconnectAttempt.value += 1
      state.value = 'reconnecting'
      if (reconnectAttempt.value === 1) {
        pushLocal('WARN', '日志通道连接断开，正在自动重连…')
      }
      await sleep(backoff(reconnectAttempt.value))
    }
    // 只有当前世代才有权把状态改成 closed；已被取代的循环保持沉默。
    if (!stale()) state.value = 'closed'
  }

  /**
   * 保活探测：服务端每 15s 发一次 heartbeat，长时间静默说明连接假活
   * （常见于休眠唤醒、VPN 切换后），主动断开让主循环重连。
   */
  function startWatch() {
    stopWatch()
    watchTimer = setInterval(() => {
      if (state.value !== 'open') return
      if (Date.now() - lastBeatAt.value <= STALE_AFTER_MS) return
      pushLocal('WARN', '日志通道长时间无响应，正在重新连接…')
      state.value = 'reconnecting'
      abort?.abort()
    }, WATCH_EVERY_MS)
  }

  function stopWatch() {
    if (watchTimer) clearInterval(watchTimer)
    watchTimer = null
  }

  /** 页面挂载时调用：先拉历史，再开流。重复调用是安全的。 */
  async function init() {
    if (loop) return
    // 先占位再 await：loadRecent 是异步的，若等它完成才赋值 loop，
    // 并发调用 init() 会各自通过上面的判断，建立起多条连接。
    const myGeneration = (generation += 1)
    stopped = false
    state.value = 'connecting'
    startWatch()
    loop = (async () => {
      await loadRecent()
      await runLoop(myGeneration)
    })().finally(() => {
      loop = null
    })
  }

  /** 页面卸载或登出时调用。 */
  function disconnect() {
    stopped = true
    // 换世代，保证在途循环即便是 init() 立刻重置 stopped 也不会复活。
    generation += 1
    stopWatch()
    abort?.abort()
    abort = null
    loop = null
    state.value = 'idle'
    reconnectAttempt.value = 0
  }

  /** 用户点「重连」：立即重开，不等退避。 */
  async function reconnect() {
    pushLocal('INFO', '正在手动重新连接日志通道…')
    stopped = true
    generation += 1
    abort?.abort()
    abort = null
    // 等旧循环真正退出再开新的：init 里先 await loadRecent()，如果不等，
    // 这段时间会同时存在新旧两条连接。
    await loop
    loop = null
    reconnectAttempt.value = 0
    await init()
  }

  return {
    logs,
    state,
    isConnected,
    reconnectAttempt,
    init,
    disconnect,
    reconnect,
  }
})
