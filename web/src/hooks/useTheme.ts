/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"
import { getCurrentTheme, getCurrentThemeMode, getThemeVariation, setTheme as setThemeUtil, setThemeMode as setThemeModeUtil, setThemeVariation as setThemeVariationUtil } from "@/utils/theme"

export function useTheme() {
  const [theme, setThemeState] = useState(() => getCurrentTheme().id)
  const [mode, setModeState] = useState(() => getCurrentThemeMode())
  const [variation, setVariationState] = useState(() => getThemeVariation())

  useEffect(() => {
    const handleThemeChange = (event: CustomEvent) => {
      const { theme: newTheme, mode: newMode, variant: newVariant } = event.detail
      setThemeState(newTheme.id)
      setModeState(newMode)
      setVariationState(newVariant)
    }

    // Listen for theme changes
    window.addEventListener("themechange", handleThemeChange as EventListener)

    return () => {
      window.removeEventListener("themechange", handleThemeChange as EventListener)
    }
  }, [])

  return {
    theme,
    mode,
    variation,
    setTheme: setThemeUtil,
    setThemeMode: setThemeModeUtil,
    setVariation: setThemeVariationUtil,
    currentTheme: getCurrentTheme(),
  }
}