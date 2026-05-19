/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import { themes, isThemePremium, type Theme } from "@/config/themes"
import { useHasPremiumAccess } from "@/hooks/useLicense.ts"
import { useTheme } from "@/hooks/useTheme"
import { getThemeColors, getThemeVariation } from "@/utils/theme"
import { canSwitchToPremiumTheme } from "@/lib/license-entitlement"
import { Sparkles, Lock, Check, Palette, AlertTriangle, WifiOff } from "lucide-react"
import { toast } from "sonner"

interface ThemeCardProps {
  theme: Theme
  isSelected: boolean
  isLocked: boolean
  onSelect: () => void
  onVariationSelect: (themeId: string, variationId: string) => void
}

function ThemeCard({ theme, isSelected, isLocked, onSelect, onVariationSelect }: ThemeCardProps) {
  // Get current variation for theme (validated)
  const variation = getThemeVariation(theme.id)

  // Helper to extract colors from theme
  const colors = getThemeColors(theme)

  return (
    <Card
      className={`cursor-pointer transition-all duration-200 hover:shadow-md h-full ${
        isSelected ? "ring-2 ring-primary" : ""
      } ${isLocked ? "opacity-60" : ""}`}
      onClick={!isLocked ? onSelect : undefined}
    >
      <CardHeader className="pb-2 sm:pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm sm:text-base flex items-center gap-1 sm:gap-2">
            {theme.name}
            {isSelected && (
              <Check className="h-3 w-3 sm:h-4 sm:w-4 text-primary" />
            )}
          </CardTitle>
          {isLocked && (
            <Lock className="h-3 w-3 sm:h-4 sm:w-4 text-muted-foreground" />
          )}
        </div>
        {theme.description && (
          <CardDescription className="text-xs line-clamp-2">
            {theme.description}
          </CardDescription>
        )}
      </CardHeader>
      <CardContent className="pt-0 space-y-2 sm:space-y-3">
        {/* Theme preview colors and variations */}
        <div className="flex items-center justify-between gap-2 flex-wrap">
          {/* Preview colors */}
          <div className="flex gap-1">
            <div
              className="w-3 h-3 sm:w-4 sm:h-4 rounded-full ring-1 ring-black/10 dark:ring-white/10"
              style={{
                backgroundColor: colors.primary,
                backgroundImage: "none",
                background: colors.primary + " !important",
              }}
            />
            <div
              className="w-3 h-3 sm:w-4 sm:h-4 rounded-full ring-1 ring-black/10 dark:ring-white/10"
              style={{
                backgroundColor: colors.secondary,
                backgroundImage: "none",
                background: colors.secondary + " !important",
              }}
            />
            <div
              className="w-3 h-3 sm:w-4 sm:h-4 rounded-full ring-1 ring-black/10 dark:ring-white/10"
              style={{
                backgroundColor: colors.accent,
                backgroundImage: "none",
                background: colors.accent + " !important",
              }}
            />
          </div>

          {/* Variation colors */}
          {colors.variations && colors.variations.length > 0 && (
            <div className="flex gap-1">
              {colors.variations.map((v) => {
                const selected = variation === v.id
                return (
                  <button
                    key={v.id}
                    onClick={(e) => {
                      e.stopPropagation()
                      onVariationSelect(theme.id, v.id)
                    }}
                    className={`w-3 h-3 sm:w-4 sm:h-4 rounded-full transition-all ${
                      selected ? "ring-2 ring-black dark:ring-white" : "ring-1 ring-black/10 dark:ring-white/10"
                    }`}
                    style={{
                      backgroundColor: v.color,
                      backgroundImage: "none",
                      background: v.color + " !important",
                    }}
                  />
                )
              })}
            </div>
          )}
        </div>

        {/* Badges */}
        <div className="flex items-center gap-1 sm:gap-2">
          {theme.isPremium ? (
            <Badge variant="secondary" className="text-xs px-1.5 sm:px-2">
              <Sparkles className="h-2.5 w-2.5 sm:h-3 sm:w-3 mr-0.5 sm:mr-1" />
              <span className="hidden sm:inline">Premium</span>
              <span className="sm:hidden">Pro</span>
            </Badge>
          ) : (
            <Badge variant="outline" className="text-xs px-1.5 sm:px-2">
              Free
            </Badge>
          )}

          {isLocked && (
            <Badge variant="destructive" className="text-xs px-1.5 sm:px-2">
              <Lock className="h-2.5 w-2.5 sm:h-3 sm:w-3 mr-0.5 sm:mr-1" />
              <span className="hidden sm:inline">Locked</span>
              <span className="sm:hidden">Lock</span>
            </Badge>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

export function ThemeSelector() {
  const { theme: currentTheme, setTheme, setVariation } = useTheme()
  const { hasPremiumAccess, isLoading, isError } = useHasPremiumAccess()

  const canSwitchPremium = canSwitchToPremiumTheme({
    hasPremiumAccess,
    isError,
    isLoading,
  })

  const isThemeLicensed = (themeId: string) => {
    if (!isThemePremium(themeId)) return true // Free themes are always available
    return canSwitchPremium
  }

  const freeThemes = themes.filter(theme => !theme.isPremium)
  const premiumThemes = themes.filter(theme => theme.isPremium)

  const showThemeLockedToast = () => {
    if (isError) {
      toast.error("Unable to verify license", {
        description: "License check failed. Premium theme switching is temporarily unavailable.",
      })
      return
    }

    toast.error("This theme requires a premium license", {
      description: "Open Settings → Themes to see payment options and redeem your discount code.",
    })
  }

  const handleThemeSelect = (themeId: string) => {
    if (isThemeLicensed(themeId)) {
      setTheme(themeId)
    } else {
      showThemeLockedToast()
    }
  }

  const handleVariationSelect = (themeId: string, variationId: string) => {
    if (isThemeLicensed(themeId)) {
      setTheme(themeId)
      setVariation(variationId)
    } else {
      showThemeLockedToast()
    }
  }

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Palette className="h-5 w-5" />
            Theme Selection
          </CardTitle>
          <CardDescription>Loading available themes...</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="animate-pulse space-y-3">
            <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
              {[...Array(6)].map((_, i) => (
                <div key={i} className="h-24 bg-muted rounded"></div>
              ))}
            </div>
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Palette className="h-5 w-5" />
          Theme Selection
        </CardTitle>
        <CardDescription>
          Choose from available themes. Premium themes require a valid license.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {isError && !canSwitchPremium && (
          <div className="flex items-center gap-2 p-3 rounded-md bg-yellow-50 dark:bg-yellow-950/20 border border-yellow-200 dark:border-yellow-800 text-yellow-800 dark:text-yellow-200">
            <WifiOff className="h-4 w-4 flex-shrink-0" />
            <p className="text-sm">
              License verification unavailable. Premium theme switching is temporarily disabled.
            </p>
          </div>
        )}

        {/* Free Themes */}
        <div>
          <h4 className="font-medium mb-3 flex items-center gap-2">
            <Badge variant="outline" className="text-xs">Free</Badge>
            Free Themes
          </h4>
          <div className="grid grid-cols-2 md:grid-cols-3 gap-2 sm:gap-3">
            {freeThemes.map((theme) => (
              <ThemeCard
                key={theme.id}
                theme={theme}
                isSelected={currentTheme === theme.id}
                isLocked={false}
                onSelect={() => handleThemeSelect(theme.id)}
                onVariationSelect={handleVariationSelect}
              />
            ))}
          </div>
        </div>

        <Separator />

        {/* Premium Themes */}
        <div>
          <h4 className="font-medium mb-3 flex items-center gap-2">
            <Badge variant="secondary" className="text-xs">
              <Sparkles className="h-3 w-3 mr-1" />
              Premium
            </Badge>
            Premium Themes
          </h4>

          {premiumThemes.length === 0 ? (
            <div className="flex flex-col items-center py-8 space-y-3">
              <Badge variant="outline" className="text-orange-600 border-orange-200 bg-orange-50 dark:text-orange-400 dark:border-orange-800 dark:bg-orange-950/20">
                <AlertTriangle className="h-3 w-3 mr-1" />
                Premium themes not loaded
              </Badge>
              <p className="text-sm text-muted-foreground text-center max-w-sm">
                Configure <code className="text-xs bg-muted px-1 py-0.5 rounded">THEMES_REPO_TOKEN</code> and run{" "}
                <code className="text-xs bg-muted px-1 py-0.5 rounded">make themes-fetch</code>.{" "}
              </p>
            </div>
          ) : (
            <div className="grid grid-cols-2 md:grid-cols-3 gap-2 sm:gap-3">
              {premiumThemes.map((theme) => {
                const isLicensed = isThemeLicensed(theme.id)
                return (
                  <ThemeCard
                    key={theme.id}
                    theme={theme}
                    isSelected={currentTheme === theme.id}
                    isLocked={!isLicensed}
                    onSelect={() => handleThemeSelect(theme.id)}
                    onVariationSelect={handleVariationSelect}
                  />
                )
              })}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
