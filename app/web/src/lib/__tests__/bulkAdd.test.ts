import { describe, expect, it } from 'vitest'

/**
 * 连续添加站点时的行为约定。
 *
 * 添加一个站点后不刷新列表、也不关闭弹层，否则每加一个站点就要重新搜索、
 * 重新滚到原来的位置。这里把"是否已添加"与"待添加数量"的判定固化下来，
 * 因为「全部添加」的按钮文案与可用性都依赖它们。
 */

interface Template {
  id: string
  name: string
}

function pendingOf(templates: Template[], addedNames: Set<string>): Template[] {
  return templates.filter((t) => !addedNames.has(t.name))
}

describe('bulk add', () => {
  it('excludes templates already added in this session', () => {
    const templates: Template[] = [
      { id: 'a', name: 'A' },
      { id: 'b', name: 'B' },
      { id: 'c', name: 'C' },
    ]
    const added = new Set(['B'])
    expect(pendingOf(templates, added).map((t) => t.name)).toEqual(['A', 'C'])
  })

  it('returns nothing when everything is added', () => {
    const templates: Template[] = [{ id: 'a', name: 'A' }]
    expect(pendingOf(templates, new Set(['A']))).toHaveLength(0)
  })

  it('keeps templates with duplicate names distinguishable by name only', () => {
    // 站点名唯一是后端约束，因此按名字判断已添加与后端一致。
    const templates: Template[] = [
      { id: 'a', name: 'A' },
      { id: 'a2', name: 'A' },
    ]
    expect(pendingOf(templates, new Set(['A']))).toHaveLength(0)
  })
})
