import { describe, expect, it } from 'vitest'

/**
 * 启停站点只补丁受影响的这一行，不重新拉整张列表。
 *
 * 背景：原来 `toggleEnabled` 成功后调用 `load()` 全量重载，导致列表滚动位置、
 * 展开状态、当前筛选条件全部回到初始。连点几行就得重新找位置，体验很差。
 *
 * 这里用一个最小的响应式数组模拟 `indexers`，验证补丁逻辑只改动目标行、
 * 且整体替换对象（Vue 3 的 ref 数组靠引用变化触发重渲染）。
 */

interface Row {
  id: number
  name: string
  enabled: boolean
  other: string
}

/** 与 IndexerPanel 的 applyEnabledState 等价。 */
function applyEnabledState(rows: Row[], id: number, enabled: boolean): Row[] {
  const next = rows.slice()
  const at = next.findIndex((r) => r.id === id)
  if (at < 0) return next
  next.splice(at, 1, { ...next[at], enabled })
  return next
}

/** 与 IndexerPanel 的 patchIndexer 等价。 */
function patchIndexer(rows: Row[], id: number, patch: Partial<Row>): Row[] {
  const next = rows.slice()
  const at = next.findIndex((r) => r.id === id)
  if (at < 0) return next
  next.splice(at, 1, { ...next[at], ...patch })
  return next
}

const rows: Row[] = [
  { id: 1, name: 'A', enabled: true, other: 'keep' },
  { id: 2, name: 'B', enabled: true, other: 'keep' },
  { id: 3, name: 'C', enabled: false, other: 'keep' },
]

describe('启停就地更新', () => {
  it('只改动目标行的 enabled', () => {
    const next = applyEnabledState(rows, 2, false)
    expect(next[1]).toEqual({ id: 2, name: 'B', enabled: false, other: 'keep' })
  })

  it('其它行保持原引用，未受影响的项不重建', () => {
    const next = applyEnabledState(rows, 2, false)
    // 行数与顺序不变
    expect(next.map((r) => r.id)).toEqual([1, 2, 3])
    // 未改动的行是同一个对象
    expect(next[0]).toBe(rows[0])
    expect(next[2]).toBe(rows[2])
    // 目标行是新对象（引用变化才会触发 Vue 重渲染）
    expect(next[1]).not.toBe(rows[1])
  })

  it('不存在的 id 不产生任何改动', () => {
    const next = applyEnabledState(rows, 99, false)
    expect(next).toEqual(rows)
  })

  it('服务端返回的字段只合并到目标行', () => {
    const next = patchIndexer(rows, 1, { enabled: false, name: 'A-renamed' })
    expect(next[0].enabled).toBe(false)
    expect(next[0].name).toBe('A-renamed')
    expect(next[1]).toBe(rows[1])
  })

  it('补丁不会丢字段', () => {
    const next = patchIndexer(rows, 3, { enabled: true })
    expect(next[2]).toEqual({ id: 3, name: 'C', enabled: true, other: 'keep' })
  })
})
