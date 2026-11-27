import fs from "node:fs"
import path from "node:path"

import { converter, formatHex, inGamut, parse, wcagContrast } from "culori"

const scriptPath = fs.realpathSync(import.meta.filename)
const freeThemesDir = path.resolve(path.dirname(scriptPath), "..", "..", "internal", "themes", "assets")
const licensedThemesDir = path.join(freeThemesDir, "premium")
const appCss = fs.readFileSync(path.resolve(path.dirname(scriptPath), "..", "src", "index.css"), "utf8")
const STATUS = ["success", "warning", "info"]
const CHARTS = ["chart-1", "chart-2", "chart-3", "chart-4", "chart-5"]
const CHART_TEXT = [1, 2, 3, 4, 5].map(n => `chart-text-${n}`)

const MODES = [["light", ":root"], ["dark", ".dark"]]
const TEXT = 4.5
const GRAPHIC = 3

const toRgb = converter("rgb")
const inSrgb = inGamut("rgb")

export function declarations(css, selector) {
  const block = css.match(new RegExp(`(?:^|\\n)[ \\t]*${selector.replace(".", "\\.")}\\s*{([^}]*)}`))
  if (!block) throw new Error(`the theme has no ${selector} block`)
  return Object.fromEntries(
    [...block[1].matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)].map(([, name, value]) => [name, value.trim()])
  )
}

function variations(css) {
  const header = css.match(/@variations:\s*([^\n*]+)/)
  return header ? header[1].split(",").map(name => name.trim()).filter(Boolean) : []
}

function resolve(vars, name) {
  const value = vars[name]
  if (value === undefined) throw new Error(`${name} is not set`)
  const reference = value.match(/^var\((--[\w-]+)\)$/)
  return reference ? resolve(vars, reference[1]) : value
}

function over(top, bottom, alpha = 1) {
  const a = toRgb(top)
  const b = toRgb(bottom)
  const k = (a.alpha ?? 1) * alpha
  return { mode: "rgb", r: a.r * k + b.r * (1 - k), g: a.g * k + b.g * (1 - k), b: a.b * k + b.b * (1 - k) }
}

function pairGroups(color, mode) {
  const page = color("background")
  const surface = (name, alpha = 1, base = page) => over(color(name), base, alpha)
  const card = surface("card")
  const popover = surface("popover")
  const sidebar = surface("sidebar")
  const sidebarPrimary = surface("sidebar-primary", 1, sidebar)
  const rows = [
    ["row", page],
    ["striped row", surface("muted", 0.4)],
    ["hovered row", surface("accent", 0.4)],
    ["selected row", surface("accent")],
  ]
  const rowText = ["foreground", "muted-foreground", "primary", "destructive", ...CHART_TEXT]
  const menuTint = mode === "dark" ? 0.2 : 0.1
  const statusPairs = name => [
    [`${name} text on background`, color(name), page, TEXT],
    [`${name} text on card`, color(name), card, TEXT],
    [`${name} text on popover`, color(name), popover, TEXT],
    [`${name} text on ${name} 10% on background`, color(name), surface(name, 0.1), TEXT],
    ...rows.map(([row, background]) => [`${name} text on ${row}`, color(name), background, TEXT]),
  ]

  return [
    {
      source: "web/src/components/ui/ (card, popover, button, badge, dropdown-menu)",
      pairs: [
        ["foreground on background", color("foreground"), page, TEXT],
        ["card-foreground on card", color("card-foreground"), card, TEXT],
        ["popover-foreground on popover", color("popover-foreground"), popover, TEXT],
        ["foreground on muted", color("foreground"), surface("muted"), TEXT],
        ["secondary-foreground on secondary", color("secondary-foreground"), surface("secondary"), TEXT],
        ["accent-foreground on accent", color("accent-foreground"), surface("accent"), TEXT],
        ["primary-foreground on primary", color("primary-foreground"), surface("primary"), TEXT],
        ["destructive-foreground on destructive", color("destructive-foreground"), surface("destructive"), TEXT],
        mode === "dark"
          ? ["white on destructive 60% (destructive button)", parse("#fff"), surface("destructive", 0.6), TEXT]
          : ["white on destructive (destructive button)", parse("#fff"), surface("destructive"), TEXT],
        ...["background", "card", "popover", "muted", "secondary"].map(name => [
          `muted-foreground on ${name}`, color("muted-foreground"), surface(name), TEXT,
        ]),
        ...["primary", "destructive"].flatMap(name => [
          [`${name} text on background`, color(name), page, TEXT],
          [`${name} text on card`, color(name), card, TEXT],
          [`${name} text on popover`, color(name), popover, TEXT],
          [`${name} text on ${name} 10% on background`, color(name), surface(name, 0.1), TEXT],
          [`${name} text on ${name} 10% on card`, color(name), surface(name, 0.1, card), TEXT],
        ]),
        [
          `destructive text on destructive ${menuTint * 100}% on popover (focused menu item)`,
          color("destructive"), surface("destructive", menuTint, popover), TEXT,
        ],
      ],
    },
    {
      source: "web/src/components/torrents/table/TorrentTableRow.tsx, getRowBackgroundClass in web/src/lib/torrent-table/row-display.ts, getRatioColor in web/src/lib/utils.ts, text-chart-text-2 and text-chart-text-3 speeds",
      pairs: rows.flatMap(([row, background]) =>
        rowText.map(name => [`${name} text on ${row}`, color(name), background, TEXT])
      ),
    },
    {
      source: "web/src (text-success, text-warning, text-info, and their 10% tints)",
      pairs: STATUS.flatMap(statusPairs),
    },
    {
      source: "web/src/components/settings/LogSettingsPanel.tsx (JSON values in text-chart-text-2 and text-chart-text-3 on bg-muted/30 in the log entry dialog)",
      pairs: ["chart-text-2", "chart-text-3"].map(name => [
        `${name} text on muted 30% on background`, color(name), surface("muted", 0.3), TEXT,
      ]),
    },
    {
      source: "web/src (kind chips: foreground text on bg-chart-1/10 to bg-chart-5/10)",
      pairs: CHARTS.flatMap(name => [
        [`foreground on ${name} 10% on background`, color("foreground"), surface(name, 0.1), TEXT],
        [`foreground on ${name} 10% on card`, color("foreground"), surface(name, 0.1, card), TEXT],
      ]),
    },
    {
      source: "web/src/components/ui/ (progress, checkbox, focus border)",
      pairs: [
        ["primary fill on muted (progress bar)", color("primary"), surface("muted"), GRAPHIC],
        ["primary fill on background (checked checkbox)", color("primary"), page, GRAPHIC],
        ["ring on background", color("ring"), page, GRAPHIC],
        ["ring on card", color("ring"), card, GRAPHIC],
      ],
    },
    {
      source: "web/src/components/layout/Sidebar.tsx",
      pairs: [
        ["sidebar-foreground on sidebar", color("sidebar-foreground"), sidebar, TEXT],
        ["sidebar-foreground 70% on sidebar", over(color("sidebar-foreground"), sidebar, 0.7), sidebar, TEXT],
        ["sidebar-primary-foreground on sidebar-primary", color("sidebar-primary-foreground"), sidebarPrimary, TEXT],
        [
          "sidebar-primary-foreground 70% on sidebar-primary",
          over(color("sidebar-primary-foreground"), sidebarPrimary, 0.7), sidebarPrimary, TEXT,
        ],
        [
          "sidebar-accent-foreground on sidebar-accent",
          color("sidebar-accent-foreground"), surface("sidebar-accent", 1, sidebar), TEXT,
        ],
      ],
    },
  ]
}

export function checkTheme(css) {
  const outsideSrgb = []
  const pairs = []

  for (const [mode, selector] of MODES) {
    const base = declarations(css, selector)
    const app = declarations(appCss, selector)
    const defaults = {
      ...Object.fromEntries(STATUS.map(name => [`--${name}`, app[`--${name}`]])),
      ...Object.fromEntries(CHART_TEXT.map(name => [`--${name}`, `var(--${name.replace("text-", "")})`])),
    }

    for (const [name, value] of Object.entries(base)) {
      const literal = parse(value)
      if (literal && !inSrgb(literal)) outsideSrgb.push(`${mode} ${name}: ${value}`)
    }

    const names = variations(css)
    for (const variation of names.length > 0 ? names : [null]) {
      const vars = { ...defaults, ...base, ...(variation && { "--variation-color": `var(--variation-${variation})` }) }
      const color = name => {
        const value = resolve(vars, `--${name}`)
        const parsed = parse(value)
        if (!parsed) throw new Error(`${mode} --${name} is not a color: ${value}`)
        return parsed
      }

      for (const { source, pairs: group } of pairGroups(color, mode)) {
        for (const [label, foreground, background, minimum] of group) {
          const text = over(foreground, background)
          const ratio = wcagContrast(text, background)
          pairs.push({
            mode, variation, source, label, minimum, ratio,
            ok: ratio >= minimum,
            colors: `${formatHex(text)} on ${formatHex(background)}`,
          })
        }
      }
    }
  }

  return { outsideSrgb, pairs }
}

export function builtInThemeFiles() {
  return [freeThemesDir, licensedThemesDir].flatMap(dir =>
    fs.readdirSync(dir).filter(name => name.endsWith(".css")).sort().map(name => path.join(dir, name))
  )
}

function report(file) {
  const { outsideSrgb, pairs } = checkTheme(fs.readFileSync(file, "utf8"))
  const failed = pairs.filter(pair => !pair.ok)

  console.log(`${file}: ${outsideSrgb.length} colors outside sRGB, ${failed.length} of ${pairs.length} pairs below the minimum`)
  for (const line of outsideSrgb) console.log(`  outside sRGB  ${line}`)

  let heading = ""
  for (const pair of failed) {
    const next = `${pair.mode}${pair.variation ? `, variation ${pair.variation}` : ""}: ${pair.source}`
    if (next !== heading) console.log(`  ${next}`)
    heading = next
    console.log(`    ${pair.ratio.toFixed(2).padStart(5)} (minimum ${pair.minimum})  ${pair.label}  ${pair.colors}`)
  }

  return outsideSrgb.length + failed.length
}

if (fs.existsSync(process.argv[1] ?? "") && fs.realpathSync(process.argv[1]) === scriptPath) {
  const files = process.argv.length > 2 ? process.argv.slice(2) : builtInThemeFiles()
  const problems = files.reduce((total, file) => total + report(file), 0)
  process.exit(problems > 0 ? 1 : 0)
}
