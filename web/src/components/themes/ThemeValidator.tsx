/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, } from "react"
import { useLicensedThemes, } from "@/hooks/useThemeLicense"
import { themes, isThemePremium, getDefaultTheme, } from "@/config/themes"
import { setValidatedThemes, setTheme, } from "@/utils/theme"

/**
 * ThemeValidator component validates theme access on mount and periodically
 * to prevent unauthorized access to premium themes via localStorage tampering
 */
export function ThemeValidator() {
  const { data, isLoading, isError, } = useLicensedThemes()
  
  useEffect(() => {
    // Don't do anything while loading - let the stored theme persist
    if (isLoading) return

    // If there's an error fetching license data, don't reset themes
    // This prevents losing theme on network issues
    if (isError) {
      console.warn("Failed to fetch license data, keeping current theme",)
      // Still set some validated themes to prevent lockout
      const fallbackThemes: string[] = []
      themes.forEach(theme => {
        // Allow all themes on error to avoid disrupting user experience
        fallbackThemes.push(theme.id,)
      },)
      setValidatedThemes(fallbackThemes,)
      return
    }

    const accessibleThemes: string[] = []
    
    themes.forEach(theme => {
      if (!isThemePremium(theme.id,)) {
        accessibleThemes.push(theme.id,)
      } else if (data?.hasPremiumAccess) {
        accessibleThemes.push(theme.id,)
      }
    },)

    // Set the validated themes - this will also clear the isInitializing flag
    setValidatedThemes(accessibleThemes,)

    // Now validate the current theme after we've set the accessible themes
    const validateCurrentTheme = () => {
      const storedThemeId = localStorage.getItem("color-theme",)
      
      // Only reset if the stored theme is premium and user doesn't have access
      // This ensures we don't unnecessarily reset the theme
      if (storedThemeId && isThemePremium(storedThemeId,) && !data?.hasPremiumAccess) {
        console.log("Premium theme detected without access, reverting to default",)
        setTheme(getDefaultTheme().id,)
      }
    }
    
    validateCurrentTheme()
  }, [data, isLoading, isError,],)
  
  // Set up periodic validation and storage event listener
  useEffect(() => {
    // Skip if still loading or no data
    if (isLoading || !data) return
    
    const validateStoredTheme = () => {
      const storedThemeId = localStorage.getItem("color-theme",)
      // Only validate and reset if we have confirmed the user doesn't have access
      if (storedThemeId && isThemePremium(storedThemeId,) && data?.hasPremiumAccess === false) {
        console.log("Periodic validation: Premium theme without access detected",)
        localStorage.removeItem("color-theme",)
        setTheme(getDefaultTheme().id,)
      }
    }

    const interval = setInterval(validateStoredTheme, 30000,)

    const handleStorageChange = (e: StorageEvent,) => {
      if (e.key === "color-theme" && e.newValue) {
        // Only validate if the new value is a premium theme and user doesn't have access
        if (isThemePremium(e.newValue,) && data?.hasPremiumAccess === false) {
          validateStoredTheme()
        }
      }
    }
    
    window.addEventListener("storage", handleStorageChange,)
    
    return () => {
      clearInterval(interval,)
      window.removeEventListener("storage", handleStorageChange,)
    }
  }, [data, isLoading,],)
  
  return null
}