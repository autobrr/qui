import { resources } from "./web/src/i18n/resources"
import { es419Overrides, frOverrides, koOverrides } from "./web/src/i18n/localeOverridesResources"

function leafPaths(obj: unknown, prefix = ""): string[] {
  if (obj === null || obj === undefined) return []
  if (typeof obj !== "object" || Array.isArray(obj)) return [prefix]
  const out: string[] = []
  for (const [k, v] of Object.entries(obj)) {
    const next = prefix ? `${prefix}.${k}` : k
    const child = leafPaths(v, next)
    if (child.length === 0) out.push(next)
    else out.push(...child)
  }
  return out
}

const enLeaf = leafPaths(resources.en.common as Record<string, unknown>)
const locales: Array<[string, Record<string, unknown>]> = [
  ["es-419", (es419Overrides.common ?? {}) as Record<string, unknown>],
  ["fr", (frOverrides.common ?? {}) as Record<string, unknown>],
  ["ko", (koOverrides.common ?? {}) as Record<string, unknown>],
]

for (const [name, ov] of locales) {
  const ovSet = new Set(leafPaths(ov))
  const missing = enLeaf.filter((p) => !ovSet.has(p))
  const byTop = new Map<string, number>()
  for (const path of missing) {
    const top = path.split(".")[0]
    byTop.set(top, (byTop.get(top) ?? 0) + 1)
  }
  const tops = [...byTop.entries()].sort((a, b) => b[1] - a[1])
  console.log(`\\n${name}`)
  for (const [k,v] of tops) console.log(`${k} ${v}`)
}
