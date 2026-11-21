import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * A component used in a template without a matching import fails only at
 * runtime (Vue logs "Failed to resolve component"), which is easy to miss.
 * This guard walks the SFCs under src/ and checks every local component used in
 * a template is imported, so the mistake fails CI instead of the browser.
 */

// vitest runs from the web/ directory; avoid import.meta.url because Vite
// rewrites it to a /@fs/ path that fs cannot read.
const ROOT = process.cwd()

function listSFC(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry)
    const stat = statSync(full)
    if (stat.isDirectory()) {
      listSFC(full, out)
    } else if (entry.endsWith('.vue')) {
      out.push(full)
    }
  }
  return out
}

/** Vue's built-in components need no import. */
const BUILTINS = new Set([
  'Transition',
  'TransitionGroup',
  'KeepAlive',
  'Teleport',
  'Suspense',
  'Component',
])

/** Local component names are PascalCase and multi-word. */
function localComponentTags(template: string): string[] {
  const tags = new Set<string>()
  for (const match of template.matchAll(/<([A-Z][A-Za-z0-9]+)[\s>]/g)) {
    if (BUILTINS.has(match[1])) continue
    tags.add(match[1])
  }
  return [...tags]
}

describe('component imports', () => {
  it('every local component used in a template is imported', () => {
    const files = listSFC(join(ROOT, 'src'))
    const missing: string[] = []

    for (const file of files) {
      const source = readFileSync(file, 'utf8')
      const templateMatch = source.match(/<template>([\s\S]*)<\/template>/)
      if (!templateMatch) continue
      const script = source.match(/<script[^>]*>([\s\S]*?)<\/script>/)?.[1] ?? ''

      for (const tag of localComponentTags(templateMatch[1])) {
        const imported = new RegExp(
          `import\\s+${tag}\\s+from|import\\s*\\{[^}]*\\b${tag}\\b[^}]*\\}\\s*from`,
        ).test(script)
        if (!imported) {
          missing.push(`${relative(ROOT, file)}: <${tag}> is used but not imported`)
        }
      }
    }

    expect(missing).toEqual([])
  })
})
