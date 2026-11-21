import { describe, expect, it } from 'vitest'
import { classifyLogEntry, classifyReady } from '../logStream'

describe('日志流序号守卫', () => {
  describe('收到日志时', () => {
    it('接受更新的序号并前移记录', () => {
      expect(classifyLogEntry(5, 6)).toEqual({ accept: true, nextSeq: 6, restarted: false })
    })

    it('丢弃重连时重发的边界行', () => {
      expect(classifyLogEntry(6, 6)).toEqual({ accept: false, nextSeq: 6, restarted: false })
      // 小幅倒退是重发的旧行，不能当成重启。
      expect(classifyLogEntry(6, 5)).toEqual({ accept: false, nextSeq: 6, restarted: false })
    })

    it('本地提示行不推进服务端序号', () => {
      expect(classifyLogEntry(6, 0)).toEqual({ accept: false, nextSeq: 6, restarted: false })
    })

    it('服务端重启后序号大幅倒退时接受新日志并重置', () => {
      expect(classifyLogEntry(120, 2)).toEqual({ accept: true, nextSeq: 2, restarted: true })
      expect(classifyLogEntry(120, 1)).toEqual({ accept: true, nextSeq: 1, restarted: true })
    })
  })

  describe('收到 ready 时', () => {
    it('首次连接不清空任何东西', () => {
      expect(classifyReady(0, 1, false)).toEqual({ restarted: false, nextSeq: 0 })
    })

    it('同一次连接里 ready 序号更大时保持本地记录', () => {
      expect(classifyReady(5, 9, false)).toEqual({ restarted: false, nextSeq: 5 })
      expect(classifyReady(5, 5, false)).toEqual({ restarted: false, nextSeq: 5 })
    })

    it('序号大幅倒退说明重启过', () => {
      expect(classifyReady(120, 1, true)).toEqual({ restarted: true, nextSeq: 0 })
    })

    it('小幅倒退但已重连也按重启处理（新进程可能停在同样的序号）', () => {
      // 真实场景：上一轮收到 seq=1，重启后新进程第一条也是 seq=1。
      expect(classifyReady(1, 1, true)).toEqual({ restarted: true, nextSeq: 0 })
      expect(classifyReady(6, 5, true)).toEqual({ restarted: true, nextSeq: 0 })
    })

    it('重连后服务端序号更大，不是重启', () => {
      expect(classifyReady(6, 8, true)).toEqual({ restarted: false, nextSeq: 6 })
    })
  })
})
