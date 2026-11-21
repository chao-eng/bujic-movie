/**
 * RequestGate keeps the newest search request authoritative. A late response
 * from an older search must be discarded rather than overwrite fresher results
 * (BR-32 / TST-049).
 */
export class RequestGate {
  private seq = 0
  private active = ''

  /** next issues a new id and makes it the only accepted response. */
  next(now: number = Date.now()): string {
    this.seq += 1
    this.active = `r${this.seq}-${now}`
    return this.active
  }

  /** isActive reports whether a response id is still the current one. */
  isActive(id: string): boolean {
    return id !== '' && id === this.active
  }

  /** activeId exposes the current id for debugging and tests. */
  activeId(): string {
    return this.active
  }
}

/**
 * createVisibilityPoller polls only while the page is visible. It returns the
 * poller so callers can stop it on unmount and trigger one immediate refresh
 * when the tab becomes visible again (BR-42 / TST-057).
 */
export interface PollerDeps {
  intervalMs: number
  fetch: () => void
  isVisible?: () => boolean
  setIntervalFn?: (handler: () => void, ms: number) => number
  clearIntervalFn?: (id: number) => void
}

export interface Poller {
  start: () => void
  stop: () => void
  refreshNow: () => void
}

export function createVisibilityPoller(deps: PollerDeps): Poller {
  const isVisible = deps.isVisible ?? (() => document.visibilityState === 'visible')
  const setIntervalFn = deps.setIntervalFn ?? ((handler, ms) => window.setInterval(handler, ms))
  const clearIntervalFn = deps.clearIntervalFn ?? ((id) => window.clearInterval(id))

  let timer: number | null = null

  const tick = () => {
    if (isVisible()) deps.fetch()
  }

  return {
    start() {
      if (timer !== null) return
      timer = setIntervalFn(tick, deps.intervalMs)
    },
    stop() {
      if (timer === null) return
      clearIntervalFn(timer)
      timer = null
    },
    refreshNow() {
      tick()
    },
  }
}
