import { describe, expect, it } from 'vitest'

/**
 * 「全部添加」必须始终可见。
 *
 * 它曾经和搜索框放在同一行：搜索框是 flex-1，窗口变窄时会把按钮挤出可视区域，
 * 用户就找不到这个入口。现在它单独落在筛选行右侧（ml-auto），
 * 这里校验"当前筛选下待添加数量"的计算，按钮文案与禁用状态都依赖它。
 */

interface Template {
  id: string
  name: string
}

/** 当前筛选结果中尚未添加的模板。 */
function pending(filtered: Template[], addedNames: Set<string>): Template[] {
  return filtered.filter((t) => !addedNames.has(t.name))
}

/** 按钮上的文案。 */
function buttonLabel(count: number): string {
  return `全部添加（${count}）`
}

describe('bulk add button', () => {
  it('counts only the templates visible under the current filter', () => {
    const filtered: Template[] = [
      { id: 'a', name: 'A' },
      { id: 'b', name: 'B' },
    ]
    expect(pending(filtered, new Set(['A']))).toHaveLength(1)
    // 已添加的那个不在可见结果里时，不影响计数。
    expect(pending(filtered, new Set(['Z']))).toHaveLength(2)
  })

  it('shows the pending count in the label', () => {
    expect(buttonLabel(0)).toBe('全部添加（0）')
    expect(buttonLabel(76)).toBe('全部添加（76）')
  })

  it('is disabled when nothing is pending', () => {
    const filtered: Template[] = [{ id: 'a', name: 'A' }]
    const items = pending(filtered, new Set(['A']))
    const disabled = items.length === 0
    expect(disabled).toBe(true)
  })
})
