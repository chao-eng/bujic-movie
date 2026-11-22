import { computed, ref } from 'vue'

export type ThemeMode = 'light' | 'dark' | 'system'

const STORAGE_KEY = 'bujic-theme'

/**
 * useTheme 管理明暗主题。
 *
 * 主题类挂到 <html> 上，与 style.css 里 `@custom-variant dark (&:is(.dark *))`
 * 和 `.dark { ... }` 的定义对应。选择「跟随系统」时监听 prefers-color-scheme，
 * 系统切换立即生效。
 */
const mode = ref<ThemeMode>(readStoredMode())
const systemDark = ref(prefersDark())
let listenerBound = false

function readStoredMode(): ThemeMode {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'light' || stored === 'dark' || stored === 'system') return stored
  } catch {
    // localStorage 不可用（隐私模式）时退回跟随系统。
  }
  return 'system'
}

function prefersDark(): boolean {
  if (typeof window === 'undefined' || !window.matchMedia) return true
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

function resolveDark(): boolean {
  return mode.value === 'system' ? systemDark.value : mode.value === 'dark'
}

function apply() {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  root.classList.toggle('dark', resolveDark())
  // 让浏览器原生控件（滚动条、输入框）跟着变。
  root.style.colorScheme = resolveDark() ? 'dark' : 'light'
}

function bindSystemListener() {
  if (listenerBound || typeof window === 'undefined' || !window.matchMedia) return
  listenerBound = true
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (event) => {
    systemDark.value = event.matches
    if (mode.value === 'system') apply()
  })
}

/** initTheme 在应用挂载前调用，避免首屏闪一下错误配色。 */
export function initTheme() {
  bindSystemListener()
  apply()
}

export function useTheme() {
  const isDark = computed(() => resolveDark())

  const setMode = (next: ThemeMode) => {
    mode.value = next
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // 存不了也不影响本次会话。
    }
    apply()
  }

  const toggle = () => setMode(isDark.value ? 'light' : 'dark')

  return { mode, isDark, setMode, toggle }
}
