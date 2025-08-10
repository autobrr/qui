/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { themes, isThemePremium, type Theme } from '@/config/themes'
import { useHasPremiumAccess } from '@/hooks/useThemeLicense'
import { useTheme } from '@/hooks/useTheme'
import { Sparkles, Lock, Check, Palette } from 'lucide-react'

interface ThemeCardProps {
  theme: Theme
  isSelected: boolean
  isLocked: boolean
  onSelect: () => void
}

// Helper to extract color preview from theme
function getThemeColors(theme: Theme) {
  // Check if dark mode is active by looking at the document element
  const isDark = document.documentElement.classList.contains('dark')
  const cssVars = isDark ? theme.cssVars.dark : theme.cssVars.light
  
  // Extract the actual color values from the theme
  const primary = cssVars['--primary']
  const secondary = cssVars['--secondary'] 
  const accent = cssVars['--accent']
  
  return { primary, secondary, accent }
}

function ThemeCard({ theme, isSelected, isLocked, onSelect }: ThemeCardProps) {
  const colors = getThemeColors(theme)
  
  return (
    <Card 
      className={`cursor-pointer transition-all duration-200 hover:shadow-md h-full ${
        isSelected ? 'ring-2 ring-primary' : ''
      } ${isLocked ? 'opacity-60' : ''}`}
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
        {/* Theme preview colors */}
        <div className="flex gap-1">
          <div 
            className="w-3 h-3 sm:w-4 sm:h-4 rounded-full ring-1 ring-black/10 dark:ring-white/10"
            style={{ 
              backgroundColor: colors.primary,
              backgroundImage: 'none',
              background: colors.primary + ' !important'
            }}
          />
          <div 
            className="w-3 h-3 sm:w-4 sm:h-4 rounded-full ring-1 ring-black/10 dark:ring-white/10"
            style={{ 
              backgroundColor: colors.secondary,
              backgroundImage: 'none',
              background: colors.secondary + ' !important'
            }}
          />
          <div 
            className="w-3 h-3 sm:w-4 sm:h-4 rounded-full ring-1 ring-black/10 dark:ring-white/10"
            style={{ 
              backgroundColor: colors.accent,
              backgroundImage: 'none',
              background: colors.accent + ' !important'
            }}
          />
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
  const { theme: currentTheme, setTheme } = useTheme()
  const { hasPremiumAccess, isLoading } = useHasPremiumAccess()
  
  const isThemeLicensed = (themeId: string) => {
    if (!isThemePremium(themeId)) return true // Free themes are always available
    return hasPremiumAccess // Premium themes require premium access
  }
  
  const freeThemes = themes.filter(theme => !theme.isPremium)
  const premiumThemes = themes.filter(theme => theme.isPremium)
  
  const handleThemeSelect = (themeId: string) => {
    if (isThemeLicensed(themeId)) {
      setTheme(themeId)
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
            <p className="text-sm text-muted-foreground text-center py-8">
              No premium themes available yet. Check back later for new themes!
            </p>
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
                  />
                )
              })}
            </div>
          )}
        </div>
        
        
        {/* Current theme info */}
        <div className="text-center">
          <p className="text-sm text-muted-foreground">
            Current theme: <span className="font-medium">{themes.find(t => t.id === currentTheme)?.name || 'Unknown'}</span>
          </p>
        </div>
      </CardContent>
    </Card>
  )
}