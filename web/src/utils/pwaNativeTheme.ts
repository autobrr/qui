/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { converter, formatHex } from "culori"

/**
 * Converts OKLCH color string to hex using proper culori library
 * Handles the OKLCH CSS function format: oklch(L C H)
 */
function oklchToHex(oklchValue: string): string {
  // Parse OKLCH string like "oklch(0.1450 0 0)" or "oklch(0.7 0.15 240)"
  const match = oklchValue.match(/oklch\(\s*([^)]+)\s*\)/)
  if (!match) return "#000000"

  const parts = match[1].trim().split(/\s+/)
  if (parts.length < 3) return "#000000"

  const [lightness, chroma, hue] = parts.map(v => parseFloat(v.trim()))

  try {
    // Create OKLCH color object with proper type
    const oklchColor = {
      mode: "oklch" as const,
      l: lightness,
      c: chroma,
      h: hue || 0,
    }

    // Create converter function for OKLCH to RGB
    const toRgb = converter("rgb")

    // Convert OKLCH to RGB
    const rgbColor = toRgb(oklchColor)
    if (!rgbColor) return "#000000"

    // Convert RGB to hex
    const hexColor = formatHex(rgbColor)
    return hexColor || "#000000"
  } catch (error) {
    console.warn("Failed to convert OKLCH to hex:", oklchValue, error)
    return "#000000"
  }
}

/**
 * Update the PWA manifest theme-color meta tag
 */
function updateManifestThemeColor(color: string): void {
  // Update theme-color meta tag
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

  // Determine if we should use light or dark status bar content
  // For dark theme colors, use light content; for light theme colors, use dark content
  const isDarkColor = isColorDark(color)
  appleStatusBarMeta.setAttribute("content", isDarkColor ? "light-content" : "dark-content")
}

/**
 * Check if a color is dark (for determining status bar content style)
 */
function isColorDark(color: string): boolean {
  // Convert hex to RGB and calculate luminance
  const hex = color.replace("#", "")
  const r = parseInt(hex.substr(0, 2), 16)
  const g = parseInt(hex.substr(2, 2), 16)
  const b = parseInt(hex.substr(4, 2), 16)

  // Calculate luminance using sRGB formula
  const luminance = (0.299 * r + 0.587 * g + 0.114 * b) / 255

  return luminance < 0.5
}

/**
 * Initialize PWA native theme support
 * This sets up listeners for theme changes and applies the initial theme
 */
export function initializePWANativeTheme(): void {
  // Function to update theme based on current state
  const updatePWATheme = () => {
    try {
      // Get current theme from DOM data attribute
      const currentThemeId = document.documentElement.getAttribute("data-theme")
      if (!currentThemeId) return

      // Determine if we're in dark mode
      const isDark = document.documentElement.classList.contains("dark")

      // Prefer stored critical vars to avoid forcing a style/layout recalculation.
      // `theme.ts` writes `theme-critical-vars` on theme application.
      let backgroundColor = ""
      try {
        const criticalVarsRaw = localStorage.getItem("theme-critical-vars")
        if (criticalVarsRaw) {
          const criticalVars = JSON.parse(criticalVarsRaw) as { background?: string }
          backgroundColor = (criticalVars.background || "").trim()
        }
      } catch {
        // ignore localStorage/JSON errors
      }

      // Fallback to computed CSS variables from the root element
      if (!backgroundColor) {
        const rootStyles = getComputedStyle(document.documentElement)
        backgroundColor = rootStyles.getPropertyValue("--background").trim()
      }

      // Use background color for seamless status bar
      let themeColor = backgroundColor

      // Convert OKLCH to hex if needed
      if (themeColor.includes("oklch")) {
        themeColor = oklchToHex(themeColor)
      }

      // Apply a default if we couldn't get a color
      if (!themeColor || themeColor === "") {
        themeColor = isDark ? "#0f172a" : "#ffffff"
      }

      updateManifestThemeColor(themeColor)
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

  // Store the listener reference for cleanup
  themeChangeListener = scheduleUpdate

  // Listen for theme change events
  window.addEventListener("themechange", themeChangeListener)

  // Also listen for class changes on documentElement (for dark mode toggles)
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

  // Apply initial theme after a short delay to ensure CSS variables are loaded
  setTimeout(scheduleUpdate, 100)
}

// Store references for cleanup
let themeChangeListener: (() => void) | null = null
let themeObserver: MutationObserver | null = null
