import client from '@/api/client'

/**
 * Tracking for the four NFR-11 events. Events are best-effort: a failure must
 * never break the page, so errors are swallowed.
 *
 *  search_submit       用户发起搜索
 *  search_first_render 首个结果渲染（duration_ms = 从提交到渲染）
 *  grab_confirm        用户确认推送
 *  grab_result         推送结果（success = 是否创建成功）
 */
export type SearchEventName = 'search_submit' | 'search_first_render' | 'grab_confirm' | 'grab_result'

const queue: Array<{ name: SearchEventName; duration_ms: number; success: boolean }> = []
let flushing = false

function flush() {
  if (flushing || queue.length === 0) return
  flushing = true
  const batch = queue.splice(0, queue.length)
  Promise.all(
    batch.map((event) =>
      client.post('/api/v1/search/events', event).catch(() => undefined),
    ),
  ).finally(() => {
    flushing = false
    if (queue.length) flush()
  })
}

export function trackSearch(name: SearchEventName, options: { durationMS?: number; success?: boolean } = {}) {
  queue.push({
    name,
    duration_ms: Math.max(0, Math.round(options.durationMS ?? 0)),
    success: Boolean(options.success),
  })
  // Batch within a tick so a search does not produce four separate requests.
  if (queue.length >= 8) {
    flush()
  } else {
    window.setTimeout(flush, 400)
  }
}
