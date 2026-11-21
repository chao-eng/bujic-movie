import { describe, expect, it, vi } from 'vitest'
import { RequestGate, createVisibilityPoller } from '../searchRequest'

// TST-049 / BR-32: a late response from an older search is discarded.
describe('RequestGate', () => {
  it('accepts only the newest request id', () => {
    const gate = new RequestGate()
    const first = gate.next(1)
    expect(gate.isActive(first)).toBe(true)

    const second = gate.next(2)
    expect(gate.isActive(second)).toBe(true)
    expect(gate.isActive(first)).toBe(false)
  })

  it('ignores empty and unknown ids', () => {
    const gate = new RequestGate()
    gate.next(1)
    expect(gate.isActive('')).toBe(false)
    expect(gate.isActive('r99-1')).toBe(false)
  })

  it('issues monotonically increasing ids', () => {
    const gate = new RequestGate()
    const a = gate.next(1)
    const b = gate.next(2)
    expect(a).not.toBe(b)
    expect(b.startsWith('r2-')).toBe(true)
  })
})

// TST-057 / BR-42: polling only runs while the tab is visible, and becoming
// visible triggers one immediate refresh.
describe('createVisibilityPoller', () => {
  function harness(visible: () => boolean) {
    let handler: (() => void) | null = null
    const setIntervalFn = vi.fn((fn: () => void, _ms: number) => {
      handler = fn
      return 7
    })
    const clearIntervalFn = vi.fn()
    const fetch = vi.fn()
    const poller = createVisibilityPoller({
      intervalMs: 5000,
      fetch,
      isVisible: visible,
      setIntervalFn,
      clearIntervalFn,
    })
    return { poller, fetch, setIntervalFn, clearIntervalFn, tick: () => handler?.() }
  }

  it('does not fetch on tick while hidden', () => {
    const h = harness(() => false)
    h.poller.start()
    h.tick()
    expect(h.fetch).not.toHaveBeenCalled()
  })

  it('fetches on tick while visible', () => {
    const h = harness(() => true)
    h.poller.start()
    h.tick()
    expect(h.fetch).toHaveBeenCalledTimes(1)
  })

  it('refreshes immediately when asked', () => {
    const h = harness(() => true)
    h.poller.refreshNow()
    expect(h.fetch).toHaveBeenCalledTimes(1)
  })

  it('stop clears the timer and start is idempotent', () => {
    const h = harness(() => true)
    h.poller.start()
    h.poller.start()
    expect(h.setIntervalFn).toHaveBeenCalledTimes(1)

    h.poller.stop()
    expect(h.clearIntervalFn).toHaveBeenCalledWith(7)
    h.poller.stop()
    expect(h.clearIntervalFn).toHaveBeenCalledTimes(1)
  })
})
