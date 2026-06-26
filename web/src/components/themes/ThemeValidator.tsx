/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect } from "react"
import { usePremiumAccess } from "@/hooks/useLicense.ts"
import { themes, isThemePremium, getDefaultTheme, getThemeById } from "@/config/themes"
import { setValidatedThemes, setTheme } from "@/utils/theme"
import { getLicenseEntitlement, isWithinGracePeriod } from "@/lib/license-entitlement"

const THEME_VALIDATION_INTERVAL_MS = 60 * 60 * 1000

/**
 * ThemeValidator component validates theme access on mount and periodically
 * to prevent unauthorized access to premium themes via localStorage tampering.
 *
 * Key behaviors:
 * - On error: do not force-reset the current theme (fixes #837 UX issue)
 * - On error: do not allow switching to premium unless within grace period
 * - Only downgrade theme on confirmed unlicensed response, not on transient errors
 */
export function ThemeValidator(): null {
  const { data, isLoading, isError } = usePremiumAccess()

  useEffect(() => {
    // Don't do anything while loading - let the stored theme persist
    if (isLoading) return

    const storedThemeId = localStorage.getItem("color-theme")

    // If there's an error fetching license data
    if (isError) {
      console.warn("Failed to fetch license data")

      const storedEntitlement = getLicenseEntitlement()
      const withinGrace =
        storedEntitlement?.lastKnownHasPremiumAccess === true && isWithinGracePeriod(storedEntitlement)

      if (withinGrace) {
        // Previously-validated premium user within grace: allow current state.
        setValidatedThemes(themes.map(theme => theme.id))
        return
      }

      // Outside grace / unknown: restrict switching to premium but keep current theme applied.
      const accessibleThemes = themes
        .filter(theme => !isThemePremium(theme.id))
        .map(theme => theme.id)
      const storedTheme = storedThemeId ? getThemeById(storedThemeId) : undefined
      if (storedTheme?.isPremium) {
        accessibleThemes.push(storedTheme.id)
      }
      setValidatedThemes(accessibleThemes)
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

    // Only reset if we have a confirmed unlicensed response (not an error)
    if (storedThemeId && isThemePremium(storedThemeId) && data?.hasPremiumAccess === false) {
      setTheme(getDefaultTheme().id)
    }
  }, [data, isLoading, isError])

  // Set up periodic validation and storage event listener
  useEffect(() => {
    // Skip if still loading
    if (isLoading) return

    // Only validate if we have confirmed data (not in error state)
    if (isError || !data) return

    const validateStoredTheme = () => {
      const storedThemeId = localStorage.getItem("color-theme")
      // Only validate and reset if we have confirmed the user doesn't have access
      if (storedThemeId && isThemePremium(storedThemeId) && data?.hasPremiumAccess === false) {
        localStorage.removeItem("color-theme")
        setTheme(getDefaultTheme().id)
      }
    }

    const interval = setInterval(validateStoredTheme, THEME_VALIDATION_INTERVAL_MS)

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
  }, [data, isLoading, isError])

  return null
}
