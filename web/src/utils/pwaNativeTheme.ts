/*
 * Copyright (c) 2024-2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/**
 * Converts OKLCH color to hex for manifest compatibility
 * Basic conversion for theme colors - handles common OKLCH formats
 */
function oklchToHex(oklchValue: string): string {
  // Handle basic OKLCH values like "oklch(0.1450 0 0)" (grayscale)
  const match = oklchValue.match(/oklch\(([^)]+)\)/)
  if (!match) return '#000000'

  const [lightness, chroma, hue] = match[1].split(' ').map(v => parseFloat(v.trim()))
  
  // For grayscale colors (chroma = 0), convert lightness to hex
  if (chroma === 0) {
    const gray = Math.round(lightness * 255)
    const hex = gray.toString(16).padStart(2, '0')
    return `#${hex}${hex}${hex}`
  }
  
  // For colors with chroma, approximate conversion
  // This is a simplified conversion - for production you might want a proper OKLCH->sRGB conversion
  const c = chroma * lightness
  const x = c * (1 - Math.abs(((hue || 0) / 60) % 2 - 1))
  const m = lightness - c
  
  let r = 0, g = 0, b = 0
  const h = hue || 0
  if (h >= 0 && h < 60) {
    r = c; g = x; b = 0
  } else if (h >= 60 && h < 120) {
    r = x; g = c; b = 0
  } else if (h >= 120 && h < 180) {
    r = 0; g = c; b = x
  } else if (h >= 180 && h < 240) {
    r = 0; g = x; b = c
  } else if (h >= 240 && h < 300) {
    r = x; g = 0; b = c
  } else if (h >= 300 && h < 360) {
    r = c; g = 0; b = x
  }
  
  r = Math.round((r + m) * 255)
  g = Math.round((g + m) * 255)
  b = Math.round((b + m) * 255)
  
  return `#${r.toString(16).padStart(2, '0')}${g.toString(16).padStart(2, '0')}${b.toString(16).padStart(2, '0')}`
}

// Removed unused getThemeColor function - theme colors are now read directly from computed styles

/**
 * Update the PWA manifest theme-color meta tag
 */
function updateManifestThemeColor(color: string): void {
  // Update theme-color meta tag
  let themeColorMeta = document.querySelector('meta[name="theme-color"]')
  if (!themeColorMeta) {
    themeColorMeta = document.createElement('meta')
    themeColorMeta.setAttribute('name', 'theme-color')
    document.head.appendChild(themeColorMeta)
  }
  themeColorMeta.setAttribute('content', color)

  // Also update Apple-specific meta tags for better iOS support
  let appleStatusBarMeta = document.querySelector('meta[name="apple-mobile-web-app-status-bar-style"]')
  if (!appleStatusBarMeta) {
    appleStatusBarMeta = document.createElement('meta')
    appleStatusBarMeta.setAttribute('name', 'apple-mobile-web-app-status-bar-style')
    document.head.appendChild(appleStatusBarMeta)
  }
  
  // Determine if we should use light or dark status bar content
  // For dark theme colors, use light content; for light theme colors, use dark content
  const isDarkColor = isColorDark(color)
  appleStatusBarMeta.setAttribute('content', isDarkColor ? 'light-content' : 'dark-content')
}

/**
 * Check if a color is dark (for determining status bar content style)
 */
function isColorDark(color: string): boolean {
  // Convert hex to RGB and calculate luminance
  const hex = color.replace('#', '')
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
      const currentThemeId = document.documentElement.getAttribute('data-theme')
      if (!currentThemeId) return
      
      // Determine if we're in dark mode
      const isDark = document.documentElement.classList.contains('dark')
      
      // Get computed CSS variables from the root element
      const rootStyles = getComputedStyle(document.documentElement)
      const primaryColor = rootStyles.getPropertyValue('--primary').trim()
      const backgroundColor = rootStyles.getPropertyValue('--background').trim()
      
      // Use primary color, fall back to background
      let themeColor = primaryColor || backgroundColor
      
      // Convert OKLCH to hex if needed
      if (themeColor.includes('oklch')) {
        themeColor = oklchToHex(themeColor)
      }
      
      // Apply a default if we couldn't get a color
      if (!themeColor || themeColor === '') {
        themeColor = isDark ? '#0f172a' : '#ffffff'
      }
      
      updateManifestThemeColor(themeColor)
    } catch (error) {
      console.warn('Failed to update PWA theme color:', error)
    }
  }
  
  // Store the listener reference for cleanup
  themeChangeListener = updatePWATheme
  
  // Listen for theme change events
  window.addEventListener('themechange', themeChangeListener)
  
  // Also listen for class changes on documentElement (for dark mode toggles)
  themeObserver = new MutationObserver((mutations) => {
    mutations.forEach((mutation) => {
      if (mutation.type === 'attributes' && 
          (mutation.attributeName === 'class' || mutation.attributeName === 'data-theme')) {
        updatePWATheme()
      }
    })
  })
  
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['class', 'data-theme']
  })
  
  // Apply initial theme after a short delay to ensure CSS variables are loaded
  setTimeout(updatePWATheme, 100)
}

// Store references for cleanup
let themeChangeListener: (() => void) | null = null
let themeObserver: MutationObserver | null = null

/**
 * Clean up PWA native theme listeners
 */
export function cleanupPWANativeTheme(): void {
  if (themeChangeListener) {
    window.removeEventListener('themechange', themeChangeListener)
    themeChangeListener = null
  }
  
  if (themeObserver) {
    themeObserver.disconnect()
    themeObserver = null
  }
}

// For backwards compatibility, export the update function
export { updateManifestThemeColor }