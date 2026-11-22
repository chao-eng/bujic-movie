import { describe, expect, it } from 'vitest'

/**
 * 主题色对比度回归。
 *
 * 背景：`--primary` 在亮色主题里曾被写成 `oklch(0.205 0 0)`（近黑），
 * 导致 logo、当前菜单项、各种图标全部退化成黑色，品牌琥珀色只在暗色生效。
 * 另外琥珀底配白字的组合容易被设成对比度不足的亮度。
 *
 * 这里用 WCAG 相对亮度公式校验：正文级文字需要 >= 4.5:1。
 */

/** oklch -> sRGB（0..1），用于计算相对亮度。 */
function oklchToSrgb(L: number, C: number, hueDeg: number): [number, number, number] {
  const h = (hueDeg * Math.PI) / 180
  const a = C * Math.cos(h)
  const b = C * Math.sin(h)
  const l_ = L + 0.3963377774 * a + 0.2158037573 * b
  const m_ = L - 0.1055613458 * a - 0.0638541728 * b
  const s_ = L - 0.0894841775 * a - 1.291485548 * b
  const l = l_ ** 3
  const m = m_ ** 3
  const s = s_ ** 3
  const r = 4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s
  const g = -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s
  const bl = -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s
  const encode = (x: number) => {
    const clamped = Math.max(0, Math.min(1, x))
    return clamped <= 0.0031308 ? 12.92 * clamped : 1.055 * clamped ** (1 / 2.4) - 0.055
  }
  return [encode(r), encode(g), encode(bl)]
}

function relativeLuminance([r, g, b]: [number, number, number]): number {
  const lin = (c: number) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4)
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
}

function contrast(a: [number, number, number], b: [number, number, number]): number {
  const la = relativeLuminance(a)
  const lb = relativeLuminance(b)
  const hi = Math.max(la, lb)
  const lo = Math.min(la, lb)
  return (hi + 0.05) / (lo + 0.05)
}

const WHITE: [number, number, number] = [1, 1, 1]

// 与 style.css 的 :root 保持一致。
const LIGHT = {
  background: oklchToSrgb(1, 0, 0),
  primary: oklchToSrgb(0.55, 0.14, 62),
  primaryForeground: oklchToSrgb(0.99, 0.01, 80),
  success: oklchToSrgb(0.52, 0.15, 152),
  warning: oklchToSrgb(0.56, 0.14, 62),
  info: oklchToSrgb(0.52, 0.14, 240),
  destructive: oklchToSrgb(0.577, 0.245, 27.325),
  mutedForeground: oklchToSrgb(0.556, 0, 0),
}

// 与 style.css 的 .dark 保持一致。
const DARK = {
  background: oklchToSrgb(0.145, 0, 0),
  primary: oklchToSrgb(0.77, 0.16, 74),
  primaryForeground: oklchToSrgb(0.2, 0.02, 250),
  success: oklchToSrgb(0.72, 0.15, 155),
  warning: oklchToSrgb(0.8, 0.14, 80),
  info: oklchToSrgb(0.7, 0.12, 235),
  destructive: oklchToSrgb(0.704, 0.191, 22.216),
  mutedForeground: oklchToSrgb(0.708, 0, 0),
}

const AA_NORMAL = 4.5

describe('light theme contrast', () => {
  it('primary is a brand colour, not near-black', () => {
    // 曾经的 bug：亮色 --primary 是 oklch(0.205 0 0)，几乎全黑。
    const nearBlack = oklchToSrgb(0.205, 0, 0)
    expect(LIGHT.primary).not.toEqual(nearBlack)
    // 琥珀色应当明显偏暖（红通道高于蓝通道）。
    expect(LIGHT.primary[0]).toBeGreaterThan(LIGHT.primary[2])
  })

  it('primary text on the page background is readable', () => {
    expect(contrast(LIGHT.primary, LIGHT.background)).toBeGreaterThanOrEqual(AA_NORMAL)
  })

  it('button label on a primary fill is readable', () => {
    expect(contrast(LIGHT.primaryForeground, LIGHT.primary)).toBeGreaterThanOrEqual(AA_NORMAL)
  })

  it.each([
    ['success', LIGHT.success],
    ['warning', LIGHT.warning],
    ['info', LIGHT.info],
    ['destructive', LIGHT.destructive],
    ['muted-foreground', LIGHT.mutedForeground],
  ])('%s on the page background meets AA', (_name, colour) => {
    expect(contrast(colour as [number, number, number], WHITE)).toBeGreaterThanOrEqual(AA_NORMAL)
  })
})

describe('dark theme contrast', () => {
  it('primary text on the dark background is readable', () => {
    expect(contrast(DARK.primary, DARK.background)).toBeGreaterThanOrEqual(AA_NORMAL)
  })

  it('button label on a primary fill is readable', () => {
    expect(contrast(DARK.primaryForeground, DARK.primary)).toBeGreaterThanOrEqual(AA_NORMAL)
  })

  it.each([
    ['success', DARK.success],
    ['warning', DARK.warning],
    ['info', DARK.info],
    ['destructive', DARK.destructive],
  ])('%s on the dark background meets AA', (_name, colour) => {
    expect(contrast(colour as [number, number, number], DARK.background)).toBeGreaterThanOrEqual(AA_NORMAL)
  })
})
