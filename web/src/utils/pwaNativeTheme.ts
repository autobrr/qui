/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { converter, formatHex } from "culori"
import { cachedBackground } from "@/lib/theme-storage"

function oklchToHex(oklchValue: string): string {
  const match = oklchValue.match(/oklch\(\s*([^)]+)\s*\)/)
  if (!match) return "#000000"

  const parts = match[1].trim().split(/\s+/)
  if (parts.length < 3) return "#000000"

  const [lightness, chroma, hue] = parts.map(v => parseFloat(v.trim()))

  try {
    const oklchColor = {
      mode: "oklch" as const,
      l: lightness,
      c: chroma,
      h: hue || 0,
    }

    const toRgb = converter("rgb")

    const rgbColor = toRgb(oklchColor)
    if (!rgbColor) return "#000000"

    const hexColor = formatHex(rgbColor)
    return hexColor || "#000000"
  } catch (error) {
    console.warn("Failed to convert OKLCH to hex:", oklchValue, error)
    return "#000000"
  }
}

function updateManifestThemeColor(color: string): void {
  let themeColorMeta = document.querySelector("meta[name=\"theme-color\"]")
  if (!themeColorMeta) {
    themeColorMeta = document.createElement("meta")
    themeColorMeta.setAttribute("name", "theme-color")
    document.head.appendChild(themeColorMeta)
  }
  themeColorMeta.setAttribute("content", color)

  // Also update Apple-specific meta tags for better iOS support
  let appleStatusBarMeta = document.querySelector("meta[name=\"apple-mobile-web-app-status-bar-style\"]")
  if (!appleStatusBarMeta) {
    appleStatusBarMeta = document.createElement("meta")
    appleStatusBarMeta.setAttribute("name", "apple-mobile-web-app-status-bar-style")
    document.head.appendChild(appleStatusBarMeta)
  }

  const isDarkColor = isColorDark(color)
  appleStatusBarMeta.setAttribute("content", isDarkColor ? "light-content" : "dark-content")
}

let launchImageColor = ""

function updateLaunchImage(): void {
  if ((navigator as Navigator & { standalone?: boolean }).standalone !== false) return

  const color = getComputedStyle(document.documentElement).getPropertyValue("--background").trim()
  if (color === launchImageColor) return

  const { width, height } = window.screen
  const ratio = window.devicePixelRatio || 1
  const canvas = document.createElement("canvas")
  canvas.width = Math.round(width * ratio)
  canvas.height = Math.round(height * ratio)
  const context = canvas.getContext("2d")
  if (!context) return
  context.fillStyle = color
  context.fillRect(0, 0, canvas.width, canvas.height)

  let link = document.querySelector<HTMLLinkElement>("link[rel=\"apple-touch-startup-image\"]")
  if (!link) {
    link = document.createElement("link")
    link.rel = "apple-touch-startup-image"
    document.head.appendChild(link)
  }
  link.media = `(device-width: ${width}px) and (device-height: ${height}px) and (-webkit-device-pixel-ratio: ${ratio}) and (orientation: portrait)`
  link.href = canvas.toDataURL("image/png")
  launchImageColor = color
}

function isColorDark(color: string): boolean {
  const hex = color.replace("#", "")
  const r = parseInt(hex.substr(0, 2), 16)
  const g = parseInt(hex.substr(2, 2), 16)
  const b = parseInt(hex.substr(4, 2), 16)

  const luminance = (0.299 * r + 0.587 * g + 0.114 * b) / 255

  return luminance < 0.5
}

export function initializePWANativeTheme(): void {
  const updatePWATheme = () => {
    try {
      const currentThemeId = document.documentElement.getAttribute("data-theme")
      if (!currentThemeId) return

      const isDark = document.documentElement.classList.contains("dark")

      // Prefer stored critical vars to avoid forcing a style/layout recalculation.
      // `theme.ts` writes `theme-critical-vars` on theme application.
      let backgroundColor = cachedBackground()

      if (!backgroundColor) {
        const rootStyles = getComputedStyle(document.documentElement)
        backgroundColor = rootStyles.getPropertyValue("--background").trim()
      }

      // Use background color for seamless status bar
      let themeColor = backgroundColor

      if (themeColor.includes("oklch")) {
        themeColor = oklchToHex(themeColor)
      }

      if (!themeColor || themeColor === "") {
        themeColor = isDark ? "#0f172a" : "#ffffff"
      }

      updateManifestThemeColor(themeColor)
      updateLaunchImage()
    } catch (error) {
      console.warn("Failed to update PWA theme color:", error)
    }
  }

  // Coalesce multiple rapid updates (theme application can trigger several mutations)
  let scheduled = false
  const scheduleUpdate = () => {
    if (scheduled) return
    scheduled = true
    requestAnimationFrame(() => {
      scheduled = false
      updatePWATheme()
    })
  }

  themeChangeListener = scheduleUpdate

  window.addEventListener("themechange", themeChangeListener)

  themeObserver = new MutationObserver((mutations) => {
    mutations.forEach((mutation) => {
      if (mutation.type === "attributes" &&
          (mutation.attributeName === "class" || mutation.attributeName === "data-theme")) {
        scheduleUpdate()
      }
    })
  })

  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["class", "data-theme"],
  })

  setTimeout(scheduleUpdate, 100)
}

let themeChangeListener: (() => void) | null = null
let themeObserver: MutationObserver | null = null
