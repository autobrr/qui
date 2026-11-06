/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { readRaw, writeRaw } from "@/lib/client-settings"
import { syncDesktopWindowMaterial } from "@/lib/desktop-window-material"

/** Stored client setting. Glass is a frost on the active theme, not a theme id. */
export const GLASS_STORAGE_KEY = "qui-glassmorphism"

/** Theme-color amount used when the row is turned on from solid. */
export const GLASS_DEFAULT_PERCENT = 60

/** 100% of the theme color. The row is unchecked and the page is solid. */
export const GLASS_SOLID_PERCENT = 100

/** Lowest theme-color amount the slider offers. The rest is the desktop. */
export const GLASS_MIN_PERCENT = 20

const LEGACY_GLASS_THEME_ID = "glassmorphism"
const GLASS_BLUR_STYLE_ID = "qui-glass-blur"

// The stylesheet copy is rewritten to a -webkit prefix that Electron drops.
// This standard property is what actually blurs the desktop behind the page.
const GLASS_BLUR_CSS = "html[data-glass=\"on\"],html[data-glass=\"on\"] body,html[data-glass=\"on\"] .bg-background,html[data-glass=\"on\"] .bg-sidebar,html[data-glass=\"on\"] .bg-card,html[data-glass=\"on\"] .bg-popover,html[data-glass=\"on\"] .bg-muted{backdrop-filter:blur(24px) saturate(160%)}"

function syncGlassBlurStyle(enabled: boolean): void {
  const existing = document.getElementById(GLASS_BLUR_STYLE_ID)
  if (!enabled) {
    existing?.remove()
    return
  }
  const style = existing ?? document.createElement("style")
  style.id = GLASS_BLUR_STYLE_ID
  style.textContent = GLASS_BLUR_CSS
  if (!existing) {
    document.head.appendChild(style)
  }
}

export function glassIsOn(percent: number): boolean {
  return percent < GLASS_SOLID_PERCENT
}

export function clampGlassPercent(value: number): number {
  if (!Number.isFinite(value) || value >= GLASS_SOLID_PERCENT) {
    return GLASS_SOLID_PERCENT
  }
  return Math.min(GLASS_SOLID_PERCENT, Math.max(GLASS_MIN_PERCENT, Math.round(value)))
}

/** `true` is the old on switch. `false` and an empty value are solid. */
export function parseGlassPercent(raw: string | null): number {
  if (raw == null || raw === "" || raw === "false") {
    return GLASS_SOLID_PERCENT
  }
  if (raw === "true") {
    return GLASS_DEFAULT_PERCENT
  }
  return clampGlassPercent(Number(raw))
}

export function serializeGlassPercent(percent: number): string {
  return String(clampGlassPercent(percent))
}

export function glassSurface(color: string, percent: number): string {
  return `color-mix(in oklab, ${color} ${clampGlassPercent(percent)}%, transparent)`
}

/** The page shell uses the frosted theme color. The body stays clear so the blur is the desktop. */
export function pageBackground(
  solid: string,
  percent: number
): { html: string; body: string } {
  if (!glassIsOn(percent)) {
    return { html: solid, body: solid }
  }
  return { html: glassSurface(solid, percent), body: "transparent" }
}

export function readGlassOpacity(): number {
  return parseGlassPercent(readRaw(GLASS_STORAGE_KEY))
}

/**
 * A saved `glassmorphism` theme id becomes the glass slider, then Minimal.
 * An explicit glass setting is left alone.
 */
export function migrateLegacyGlassTheme(
  storedThemeId: string | null,
  writeThemeId: (id: string) => void
): void {
  if (storedThemeId !== LEGACY_GLASS_THEME_ID) {
    return
  }
  if (readRaw(GLASS_STORAGE_KEY) == null) {
    writeRaw(GLASS_STORAGE_KEY, serializeGlassPercent(GLASS_DEFAULT_PERCENT))
  }
  writeThemeId("minimal")
}

export function applyGlassOpacity(percent: number): void {
  const amount = clampGlassPercent(percent)
  const enabled = glassIsOn(amount)
  const root = document.documentElement
  if (enabled) {
    root.dataset.glass = "on"
    root.style.setProperty("--glass-surface", `${amount}%`)
  } else {
    delete root.dataset.glass
    root.style.removeProperty("--glass-surface")
  }
  syncGlassBlurStyle(enabled)
  const solid = root.style.getPropertyValue("--background").trim()
  if (solid) {
    const painted = pageBackground(solid, amount)
    root.style.backgroundColor = painted.html
    if (document.body) {
      document.body.style.backgroundColor = painted.body
    }
  }
  void syncDesktopWindowMaterial(enabled)
}
