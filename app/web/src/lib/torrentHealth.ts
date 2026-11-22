/**
 * 种子热度与发布时间的小工具。
 *
 * 搜索结果里的做种数单独一个数字很难读：0 到底表示「没人做种」还是「站点没给」，
 * 需要和下载人数、完成数放在一起看。这里把判读口径统一，界面只负责展示。
 */

/** 做种数的健康档位。muted 表示站点没有提供热度数据。 */
export type SeedTone = 'success' | 'warning' | 'error' | 'muted'

export interface SeedHealth {
  tone: SeedTone
  /** 短标签，用作徽章文案；空字符串表示只显示数字。 */
  label: string
  /** 悬停说明，解释这个数字意味着什么。 */
  hint: string
}

/**
 * seedHealth 把做种数翻译成一眼能懂的档位。
 *
 * 阈值按公开种子站的经验值：个位数做种基本只有几十 KB/s，20 以上才谈得上
 * 「下载有速度」，0 做种的老种大概率已经死了。known=false 表示站点根本没给
 * 这个数字，此时不能判定为死种，只能提示「站点未提供」。
 */
export function seedHealth(seeders: number, known = true): SeedHealth {
  if (!known) {
    return {
      tone: 'muted',
      label: '做种未知',
      hint: '该站点没有返回做种数，无法判断是否还有速度；可看完成数或打详情页确认。',
    }
  }
  if (!Number.isFinite(seeders) || seeders <= 0) {
    return {
      tone: 'error',
      label: '无人做种',
      hint: '当前没有做种者，可能已失效；完成数高说明历史上有人下载过。',
    }
  }
  if (seeders < 5) {
    return {
      tone: 'warning',
      label: '做种少',
      hint: '做种者很少，下载速度通常很慢，甚至长时间没有进度。',
    }
  }
  return {
    tone: 'success',
    label: '',
    hint: '做种者足够，通常能正常下载。',
  }
}

/** formatCount 把大数字缩成 1.2k / 3.4M，避免徽章被撑开。 */
export function formatCount(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0'
  if (value < 1000) return String(Math.round(value))
  if (value < 1_000_000) return `${(value / 1000).toFixed(value < 10_000 ? 1 : 0)}k`
  return `${(value / 1_000_000).toFixed(1)}M`
}

/**
 * formatAge 返回「3 天前」这类相对时间。无法解析的发布时间（部分站点直接返回
 * “now” 或一段页面文本）返回空字符串，界面据此不显示。
 */
export function formatAge(raw: string, now: number = Date.now()): string {
  const text = (raw ?? '').trim()
  if (!text) return ''
  // 站点常见的原样文案，先直接映射，避免掉进 Date 的非标准解析。
  const lower = text.toLowerCase()
  if (lower === 'now' || lower === 'today') return '今天'
  if (lower === 'yesterday') return '昨天'

  const date = new Date(text)
  const stamp = date.getTime()
  if (Number.isNaN(stamp)) return ''

  const diff = now - stamp
  if (diff < 0) return '刚刚'
  const minute = 60 * 1000
  const hour = 60 * minute
  const day = 24 * hour
  if (diff < 5 * minute) return '刚刚'
  if (diff < hour) return `${Math.floor(diff / minute)} 分钟前`
  if (diff < day) return `${Math.floor(diff / hour)} 小时前`
  if (diff < 30 * day) return `${Math.floor(diff / day)} 天前`
  if (diff < 365 * day) return `${Math.floor(diff / (30 * day))} 个月前`
  return `${Math.floor(diff / (365 * day))} 年前`
}
