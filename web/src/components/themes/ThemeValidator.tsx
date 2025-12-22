/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect } from "react"
import { usePremiumAccess } from "@/hooks/useLicense.ts"
import { themes, isThemePremium, getDefaultTheme, getThemeById } from "@/config/themes"
import { setValidatedThemes, setTheme } from "@/utils/theme"
import {
  getLicenseEntitlement,
  isWithinGracePeriod,
} from "@/lib/license-entitlement"

/**
 * ThemeValidator component validates theme access on mount and periodically
 * to prevent unauthorized access to premium themes via localStorage tampering.
 *
 * Key behaviors:
 * 1. On error: Never force-reset to a free theme (fixes #837 UX issue)
 * 2. On error: Never grant premium unless previously validated (grace period)
 * 3. Use grace period: If we had premium access within 7 days, preserve current theme
 * 4. Only downgrade theme on confirmed unlicensed response, not on transient errors
 */
export function ThemeValidator() {
  const { data, isLoading, isError } = usePremiumAccess()

  useEffect(() => {
    // Don't do anything while loading - let the stored theme persist
    if (isLoading) return

    // If there's an error fetching license data
    if (isError) {
      console.warn("Failed to fetch license data - checking grace period")

      const storedEntitlement = getLicenseEntitlement()
      const withinGrace = isWithinGracePeriod(storedEntitlement)
      const hadPremium = storedEntitlement?.lastKnownHasPremiumAccess === true

      if (hadPremium && withinGrace) {
        // Within grace period with previous premium access:
        // - Keep current theme (including premium) as-is
        // - Set validatedThemes to include premium themes (allow current state)
        console.log("Within grace period, preserving premium theme access")
        const accessibleThemes = themes.map(theme => theme.id)
        setValidatedThemes(accessibleThemes)
      } else {
        // Outside grace period OR never had premium:
        // - Set validatedThemes to free themes only
        // - Keep the currently-selected theme accessible (avoid fallback to default)
        // - The theme picker will block switching to premium themes
        console.log("Outside grace period or no prior premium, restricting to free themes")
        const accessibleThemes = themes
          .filter(theme => !isThemePremium(theme.id))
          .map(theme => theme.id)
        const storedThemeId = localStorage.getItem("color-theme")
        const storedTheme = storedThemeId ? getThemeById(storedThemeId) : undefined
        if (storedTheme?.isPremium) {
          accessibleThemes.push(storedTheme.id)
        }
        setValidatedThemes(accessibleThemes)

        // Note: We intentionally do NOT call setTheme() here to avoid
        // jarring theme resets on transient network errors. The next
        // successful validation will handle any necessary corrections.
      }
      return
    }

    // Successful response - build validated themes based on actual entitlement
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
    // Only reset if we have a CONFIRMED unlicensed response (not an error)
    const storedThemeId = localStorage.getItem("color-theme")
    if (storedThemeId && isThemePremium(storedThemeId) && data?.hasPremiumAccess === false) {
      console.log("Premium theme detected with confirmed no access, reverting to default")
      setTheme(getDefaultTheme().id)
    }
  }, [data, isLoading, isError])

  // Set up periodic validation and storage event listener
  useEffect(() => {
    // Skip if still loading
    if (isLoading) return

    // Only validate if we have confirmed data (not in error state)
    // During error state, we rely on the grace period logic above
    if (isError || !data) return

    const validateStoredTheme = () => {
      const storedThemeId = localStorage.getItem("color-theme")
      // Only validate and reset if we have CONFIRMED the user doesn't have access
      if (storedThemeId && isThemePremium(storedThemeId) && data.hasPremiumAccess === false) {
        console.log("Periodic validation: Premium theme without confirmed access detected")
        localStorage.removeItem("color-theme")
        setTheme(getDefaultTheme().id)
      }
    }

    const interval = setInterval(validateStoredTheme, 60 * 60 * 1000)

    const handleStorageChange = (e: StorageEvent) => {
      if (e.key === "color-theme" && e.newValue) {
        // Only validate if the new value is a premium theme and user doesn't have access
        if (isThemePremium(e.newValue) && data.hasPremiumAccess === false) {
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
