/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { THEME_NAME_PATTERN } from "@/utils/themeParser"

const MAX_THEME_ID_LENGTH = 64

export function themeIdFromName(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+/, "")
    .slice(0, MAX_THEME_ID_LENGTH)
    .replace(/-+$/, "")
}

export function readThemeName(css: string): string | null {
  return css.match(THEME_NAME_PATTERN)?.[1].trim() ?? null
}

export function writeThemeName(css: string, name: string): string {
  const safeName = name.replace(/\*\/|\/\*|[\r\n]/g, " ").replace(/\s+/g, " ").trim()
  const match = css.match(THEME_NAME_PATTERN)
  if (!match || match.index === undefined) {
    return `/* @name: ${safeName} */\n${css}`
  }
  const start = match.index + match[0].lastIndexOf(match[1])
  return css.slice(0, start) + safeName + css.slice(start + match[1].length)
}
