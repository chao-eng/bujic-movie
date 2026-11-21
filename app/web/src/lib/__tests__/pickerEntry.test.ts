import { describe, expect, it } from 'vitest'

/**
 * 模板选择器的两种入口。
 *
 * 1. 从「添加站点」进入：先看模板列表（带搜索与筛选），选中后填表。
 * 2. 从某个模板的「添加」进入：模板已确定，应直接显示填写表单，
 *    不能再显示搜索框——用户已经选好了，再让他搜一次只会困惑。
 *
 * 这里固化"什么时候显示列表、什么时候显示表单"的判定。
 */

interface PickerState {
  /** 进入时是否已经指定了模板 */
  hasPreset: boolean
  /** 用户是否已选中某个模板 */
  hasSelection: boolean
}

/** 是否显示模板列表（搜索框 + 筛选 + 结果）。 */
function showsList(state: PickerState): boolean {
  return !state.hasPreset && !state.hasSelection
}

/** 是否显示填写表单。 */
function showsForm(state: PickerState): boolean {
  return state.hasPreset || state.hasSelection
}

describe('template picker entry', () => {
  it('shows only the form when opened from a template row', () => {
    const state = { hasPreset: true, hasSelection: false }
    expect(showsList(state)).toBe(false)
    expect(showsForm(state)).toBe(true)
  })

  it('shows the list when opened from "add site"', () => {
    const state = { hasPreset: false, hasSelection: false }
    expect(showsList(state)).toBe(true)
    expect(showsForm(state)).toBe(false)
  })

  it('switches to the form after picking from the list', () => {
    const state = { hasPreset: false, hasSelection: true }
    expect(showsList(state)).toBe(false)
    expect(showsForm(state)).toBe(true)
  })

  it('never shows both at once', () => {
    for (const hasPreset of [true, false]) {
      for (const hasSelection of [true, false]) {
        const state = { hasPreset, hasSelection }
        expect(showsList(state) && showsForm(state)).toBe(false)
      }
    }
  })
})
