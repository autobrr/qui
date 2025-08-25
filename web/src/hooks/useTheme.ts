/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useEffect, } from "react"
import { getCurrentTheme, getCurrentThemeMode, setTheme as setThemeUtil, setThemeMode as setThemeModeUtil, type ThemeMode, } from "@/utils/theme"

export function useTheme() {
  const [theme, setThemeState,] = useState(() => getCurrentTheme().id,)
  const [mode, setModeState,] = useState(() => getCurrentThemeMode(),)

  useEffect(() => {
    const handleThemeChange = (event: CustomEvent,) => {
      const { theme: newTheme, mode: newMode, } = event.detail
      setThemeState(newTheme.id,)
      setModeState(newMode,)
    }

    // Listen for theme changes
    window.addEventListener("themechange", handleThemeChange as EventListener,)

    return () => {
      window.removeEventListener("themechange", handleThemeChange as EventListener,)
    }
  }, [],)

  const setTheme = async (themeId: string,) => {
    await setThemeUtil(themeId,)
  }

  const setThemeMode = async (newMode: ThemeMode,) => {
    await setThemeModeUtil(newMode,)
  }

  return {
    theme,
    mode,
    setTheme,
    setThemeMode,
    currentTheme: getCurrentTheme(),
  }
}