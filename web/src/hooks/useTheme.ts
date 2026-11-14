/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect } from "react"
import { getCurrentTheme, getCurrentThemeMode, getThemeVariation, isDarkMode, setTheme as setThemeUtil, setThemeMode as setThemeModeUtil, setThemeVariation as setThemeVariationUtil } from "@/utils/theme"

export function useTheme() {
  const [theme, setThemeState] = useState(() => getCurrentTheme().id)
  const [mode, setModeState] = useState(() => getCurrentThemeMode())
  const [variation, setVariationState] = useState(() => getThemeVariation())
  // A system switch in auto mode keeps the mode and the theme, so the resolved
  // light or dark is state of its own, to render again on that switch.
  const [dark, setDarkState] = useState(() => isDarkMode(getCurrentThemeMode()))

  useEffect(() => {
    const handleThemeChange = (event: CustomEvent) => {
      const { theme: newTheme, mode: newMode, variant: newVariant } = event.detail
      setThemeState(newTheme.id)
      setModeState(newMode)
      setVariationState(newVariant)
      setDarkState(isDarkMode(newMode))
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
    /** True when the mode setting paints themes dark now, with auto resolved by the system. */
    dark,
    setTheme: setThemeUtil,
    setThemeMode: setThemeModeUtil,
    setVariation: setThemeVariationUtil,
    currentTheme: getCurrentTheme(),
  }
}