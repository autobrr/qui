/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect } from 'react'
import { useLicensedThemes } from '@/hooks/useThemeLicense'
import { themes, isThemePremium, getDefaultTheme } from '@/config/themes'
import { setValidatedThemes, getCurrentTheme, setTheme } from '@/utils/theme'

/**
 * ThemeValidator component validates theme access on mount and periodically
 * to prevent unauthorized access to premium themes via localStorage tampering
 */
export function ThemeValidator() {
  const { data, isLoading } = useLicensedThemes()
  
  useEffect(() => {
    if (isLoading) return

    const accessibleThemes: string[] = []
    
    themes.forEach(theme => {
      if (!isThemePremium(theme.id)) {
        accessibleThemes.push(theme.id)
      } else if (data?.hasPremiumAccess) {
        accessibleThemes.push(theme.id)
      }
    })

    setValidatedThemes(accessibleThemes)

    const validateCurrentTheme = () => {
      const currentTheme = getCurrentTheme()
      if (isThemePremium(currentTheme.id) && !data?.hasPremiumAccess) {
        setTheme(getDefaultTheme().id)
      }
    }
    
    validateCurrentTheme()
  }, [data, isLoading])
  
  // Set up periodic validation and storage event listener
  useEffect(() => {
    const validateStoredTheme = () => {
      const storedThemeId = localStorage.getItem('color-theme')
      if (storedThemeId && isThemePremium(storedThemeId) && !data?.hasPremiumAccess) {
        localStorage.removeItem('color-theme')
        setTheme(getDefaultTheme().id)
      }
    }

    const interval = setInterval(validateStoredTheme, 30000)

    const handleStorageChange = (e: StorageEvent) => {
      if (e.key === 'color-theme' && e.newValue) {
        validateStoredTheme()
      }
    }
    
    window.addEventListener('storage', handleStorageChange)
    
    return () => {
      clearInterval(interval)
      window.removeEventListener('storage', handleStorageChange)
    }
  }, [data])
  
  return null
}