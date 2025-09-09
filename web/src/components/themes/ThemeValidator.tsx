/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect } from "react"
import { usePremiumAccess } from "@/hooks/useThemeLicense"
import { themes, isThemePremium, getDefaultTheme } from "@/config/themes"
import { setValidatedThemes, setTheme } from "@/utils/theme"
import { router } from "@/router"

/**
 * ThemeValidator component validates theme access on mount and periodically
 * to prevent unauthorized access to premium themes via localStorage tampering
 */
export function ThemeValidator() {
  const { data, isLoading, isError } = usePremiumAccess()

  useEffect(() => {
    // Don't do anything while loading - let the stored theme persist
    if (isLoading) return

    // Check if we're on the login page using TanStack Router
    const currentPath = router.state.location.pathname
    const isLoginPage = currentPath === "/login"

    // If there's an error fetching license data
    if (isError) {
      console.warn("Failed to fetch license data")

      // Don't reset theme on login page to avoid disruption
      if (isLoginPage) {
        console.log("On login page, keeping current theme")
        // Still set some validated themes to prevent lockout
        const fallbackThemes: string[] = []
        themes.forEach(theme => {
          fallbackThemes.push(theme.id)
        })
        setValidatedThemes(fallbackThemes)
        return
      }

      // Reset to minimal theme when not on login page
      console.log("Resetting to minimal theme due to license fetch error")
      const minimalThemes = themes.filter(theme => !isThemePremium(theme.id)).map(theme => theme.id)
      setValidatedThemes(minimalThemes)

      // Force reset to minimal theme
      const currentTheme = localStorage.getItem("color-theme")
      if (currentTheme && isThemePremium(currentTheme)) {
        setTheme("minimal")
      }
      return
    }

    const accessibleThemes: string[] = []

    themes.forEach(theme => {
      if (!isThemePremium(theme.id)) {
        accessibleThemes.push(theme.id)
      } else if (data?.hasPremiumAccess) {
        accessibleThemes.push(theme.id)
      }
    })

    // Set the validated themes - this will also clear the isInitializing flag
    setValidatedThemes(accessibleThemes)

    // Now validate the current theme after we've set the accessible themes
    const validateCurrentTheme = () => {
      const storedThemeId = localStorage.getItem("color-theme")

      // Only reset if the stored theme is premium and user doesn't have access
      // This ensures we don't unnecessarily reset the theme
      if (storedThemeId && isThemePremium(storedThemeId) && !data?.hasPremiumAccess) {
        console.log("Premium theme detected without access, reverting to default")
        setTheme(getDefaultTheme().id)
      }
    }

    validateCurrentTheme()
  }, [data, isLoading, isError])

  // Set up periodic validation and storage event listener
  useEffect(() => {
    // Skip if still loading or no data
    if (isLoading || !data) return

    const validateStoredTheme = () => {
      const storedThemeId = localStorage.getItem("color-theme")
      // Only validate and reset if we have confirmed the user doesn't have access
      if (storedThemeId && isThemePremium(storedThemeId) && data?.hasPremiumAccess === false) {
        console.log("Periodic validation: Premium theme without access detected")
        localStorage.removeItem("color-theme")
        setTheme(getDefaultTheme().id)
      }
    }

    const interval = setInterval(validateStoredTheme, 30000)

    const handleStorageChange = (e: StorageEvent) => {
      if (e.key === "color-theme" && e.newValue) {
        // Only validate if the new value is a premium theme and user doesn't have access
        if (isThemePremium(e.newValue) && data?.hasPremiumAccess === false) {
          validateStoredTheme()
        }
      }
    }

    window.addEventListener("storage", handleStorageChange)

    return () => {
      clearInterval(interval)
      window.removeEventListener("storage", handleStorageChange)
    }
  }, [data, isLoading])

  return null
}