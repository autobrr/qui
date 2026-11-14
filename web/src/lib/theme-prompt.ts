/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { THEME_AREAS } from "@/lib/theme-areas"

// The design token table of documentation/docs/features/custom-themes.md.
export const THEME_TOKEN_GROUPS: ReadonlyArray<{ group: string; tokens: readonly string[] }> = [
  { group: "Surfaces", tokens: ["--background", "--foreground", "--card", "--card-foreground", "--popover", "--popover-foreground"] },
  {
    group: "Semantic colors",
    tokens: [
      "--primary", "--primary-foreground", "--secondary", "--secondary-foreground", "--muted", "--muted-foreground",
      "--accent", "--accent-foreground", "--destructive", "--destructive-foreground",
    ],
  },
  { group: "Controls", tokens: ["--border", "--input", "--ring"] },
  {
    group: "Sidebar",
    tokens: [
      "--sidebar", "--sidebar-foreground", "--sidebar-primary", "--sidebar-primary-foreground",
      "--sidebar-accent", "--sidebar-accent-foreground", "--sidebar-border", "--sidebar-ring",
    ],
  },
  { group: "Charts", tokens: ["--chart-1", "--chart-2", "--chart-3", "--chart-4", "--chart-5"] },
  { group: "Ratio colors", tokens: ["--ratio-bad", "--ratio-almost", "--ratio-good", "--ratio-best"] },
  { group: "Typography", tokens: ["--font-sans", "--font-serif", "--font-mono"] },
  { group: "Shape and spacing", tokens: ["--radius", "--spacing", "--tracking-normal"] },
  {
    group: "Shadows",
    tokens: ["--shadow-2xs", "--shadow-xs", "--shadow-sm", "--shadow", "--shadow-md", "--shadow-lg", "--shadow-xl", "--shadow-2xl"],
  },
]

export const COMMON_DATA_SLOTS = [
  "button", "input", "textarea", "badge", "card", "card-header", "card-title", "dialog-content", "sheet-content",
  "dropdown-menu-content", "dropdown-menu-item", "select-trigger", "select-content", "table", "table-row",
  "table-head", "table-cell", "tabs-list", "tabs-trigger", "switch", "checkbox", "separator", "scroll-area",
] as const

/** The English text that tells an AI assistant how to write a qui theme. */
export function buildThemePrompt(): string {
  const tokens = THEME_TOKEN_GROUPS.map(({ group, tokens }) => `- ${group}: ${tokens.join(", ")}`).join("\n")
  const areas = THEME_AREAS.map(({ name, purpose }) => `- [data-qui-area="${name}"]: ${purpose}`).join("\n")
  const slots = COMMON_DATA_SLOTS.map(slot => `[data-slot="${slot}"]`).join(", ")

  return `You write a custom theme for qui, a web interface for qBittorrent. qui adds the theme to the page as one CSS stylesheet. Ask me how the theme must look if I did not tell you. Then return the theme as one CSS file in one code block, with no other text in the block.

The file must have these parts:

1. A comment header first:
/* @name: <Theme name>
 * @description: <one optional line>
 */
2. A :root block for light mode and a .dark block for dark mode. Each block must set at least one design token.
3. Optional rules that change more than colors.

Set colors and other values through the design tokens. Any valid CSS color works. qui uses the default value for each token that the theme does not set. The design tokens:
${tokens}

If you add rules beyond the tokens, use only these selectors, because qui keeps them the same across releases. Do not use class names: they change between releases, and the theme then breaks.

Theme areas:
${areas}

Common parts, by their data-slot attribute: ${slots}.

Rules:
- Do not use @import.
- Do not use image-set().
- Do not use url() for a file. url() can only hold a data: address (for example a small image or a font as base64) or a #fragment.
- Do not use backslash escapes outside a quoted string.
- Keep the file at 1 MiB or less.
- Do not change the height of a torrent row, its padding, its border width, or its font size. qui computes the position of each row from a fixed height.
- Keep the text readable: each foreground token must have enough contrast with its background token, in light mode and in dark mode.
- If you set a font token, use a font that the browser has, or embed the font with @font-face and a data: address.

qui refuses to save a theme that breaks one of the rules about files, and it names the line.`
}
