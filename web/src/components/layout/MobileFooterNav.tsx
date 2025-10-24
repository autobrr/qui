/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { isThemePremium, themes } from "@/config/themes"
import { useTorrentSelection } from "@/contexts/TorrentSelectionContext"
import { useAuth } from "@/hooks/useAuth"
import { useHasPremiumAccess } from "@/hooks/useLicense"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import {
  getCurrentTheme,
  getCurrentThemeMode,
  setTheme,
  setThemeMode,
  type ThemeMode
} from "@/utils/theme"
import { useQuery } from "@tanstack/react-query"
import { Link, useLocation } from "@tanstack/react-router"
import {
  Check,
  Github,
  HardDrive,
  Home,
  LogOut,
  Monitor,
  Moon,
  Palette,
  Server,
  Settings,
  Sun,
  TrendingUp
} from "lucide-react"
import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"


// Helper to extract primary color from theme
function getThemePrimaryColor(theme: typeof themes[0]) {
  const isDark = document.documentElement.classList.contains("dark")
  const cssVars = isDark ? theme.cssVars.dark : theme.cssVars.light
  return cssVars["--primary"] || ""
}

// Custom hook for theme change detection
const useThemeChange = () => {
  const [currentMode, setCurrentMode] = useState<ThemeMode>(getCurrentThemeMode())
  const [currentTheme, setCurrentTheme] = useState(getCurrentTheme())

  const checkTheme = useCallback(() => {
    setCurrentMode(getCurrentThemeMode())
    setCurrentTheme(getCurrentTheme())
  }, [])

  useEffect(() => {
    const handleThemeChange = () => {
      checkTheme()
    }

    window.addEventListener("themechange", handleThemeChange)
    return () => {
      window.removeEventListener("themechange", handleThemeChange)
    }
  }, [checkTheme])

  return { currentMode, currentTheme }
}

export function MobileFooterNav() {
  const location = useLocation()
  const { logout } = useAuth()
  const { isSelectionMode } = useTorrentSelection()
  const { currentMode, currentTheme } = useThemeChange()
  const { hasPremiumAccess } = useHasPremiumAccess()
  const [showThemeDialog, setShowThemeDialog] = useState(false)

  const { data: instances } = useQuery({
    queryKey: ["instances"],
    queryFn: () => api.getInstances(),
  })

  const activeInstances = instances?.filter(i => i.connected) || []
  const isOnInstancePage = location.pathname.startsWith("/instances/")
  const currentInstanceId = isOnInstancePage? location.pathname.split("/")[2]: null
  const currentInstance = instances?.find(i => i.id.toString() === currentInstanceId)
  const currentInstanceLabel = currentInstance ? currentInstance.name : "Clients"

  const handleModeSelect = useCallback(async (mode: ThemeMode) => {
    await setThemeMode(mode)
    const modeNames = { light: "Light", dark: "Dark", auto: "System" }
    toast.success(`Switched to ${modeNames[mode]} mode`)
  }, [])

  const handleThemeSelect = useCallback(async (themeId: string) => {
    const isPremium = isThemePremium(themeId)
    if (isPremium && !hasPremiumAccess) {
      toast.error("This is a premium theme. Please purchase a license to use it.")
      return
    }

    await setTheme(themeId)
    const theme = themes.find(t => t.id === themeId)
    toast.success(`Switched to ${theme?.name || themeId} theme`)
  }, [hasPremiumAccess])

  if (isSelectionMode) {
    return null
  }

  return (
    <nav
      className={cn(
        "fixed bottom-0 left-0 right-0 z-40 lg:hidden",
        "bg-background/80 backdrop-blur-md border-t border-border/50"
      )}
      style={{ paddingBottom: "env(safe-area-inset-bottom)" }}
    >
      <div className="flex items-center justify-around h-16">
        {/* Dashboard */}
        <Link
          to="/dashboard"
          className={cn(
            "flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1",
            location.pathname === "/dashboard"? "text-primary": "text-muted-foreground hover:text-foreground"
          )}
        >
          <Home className={cn(
            "h-5 w-5",
            location.pathname === "/dashboard" && "text-primary"
          )} />
          <span className="truncate">Dashboard</span>
        </Link>

        {/* Economy */}
        <Link
          to="/economy"
          className={cn(
            "flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1",
            location.pathname === "/economy"? "text-primary": "text-muted-foreground hover:text-foreground"
          )}
        >
          <TrendingUp className={cn(
            "h-5 w-5",
            location.pathname === "/economy" && "text-primary"
          )} />
          <span className="truncate">Economy</span>
        </Link>

        {/* Clients dropdown */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              className={cn(
                "flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 hover:cursor-pointer",
                isOnInstancePage? "text-primary": "text-muted-foreground hover:text-foreground"
              )}
            >
              <div className="relative">
                <HardDrive className={cn(
                  "h-5 w-5",
                  isOnInstancePage && "text-primary"
                )} />
                {activeInstances.length > 0 && (
                  <Badge
                    className="absolute -top-1 -right-2 h-4 w-4 p-0 flex items-center justify-center text-[9px]"
                    variant="default"
                  >
                    {activeInstances.length}
                  </Badge>
                )}
              </div>
              <span
                className="block max-w-[7.5rem] truncate text-center"
                title={currentInstanceLabel}
              >
                {currentInstanceLabel}
              </span>
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="center" side="top" className="w-56 mb-2">
            <DropdownMenuLabel>qBittorrent Clients</DropdownMenuLabel>
            <DropdownMenuSeparator />
            {instances?.map((instance) => (
              <DropdownMenuItem key={instance.id} asChild>
                <Link
                  to="/instances/$instanceId"
                  params={{ instanceId: instance.id.toString() }}
                  className="flex items-center gap-2 min-w-0"
                >
                  <HardDrive className="h-4 w-4" />
                  <span
                    className="flex-1 min-w-0 truncate"
                    title={instance.name}
                  >
                    {instance.name}
                  </span>
                  <span
                    className={cn(
                      "h-2 w-2 rounded-full",
                      instance.connected ? "bg-green-500" : "bg-red-500"
                    )}
                  />
                </Link>
              </DropdownMenuItem>
            ))}
            {(!instances || instances.length === 0) && (
              <DropdownMenuItem disabled>
                No clients configured
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>

        {/* Settings dropdown */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              className={cn(
                "flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 hover:cursor-pointer",
                location.pathname === "/settings"? "text-primary": "text-muted-foreground hover:text-foreground"
              )}
            >
              <Settings className={cn(
                "h-5 w-5",
                location.pathname === "/settings" && "text-primary"
              )} />
              <span className="truncate">Settings</span>
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" side="top" className="mb-2 w-56">
            <DropdownMenuItem asChild>
              <Link
                to="/settings"
                className="flex items-center gap-2"
              >
                <Settings className="h-4 w-4" />
                General Settings
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link
                to="/settings"
                search={{ tab: "instances" }}
                className="flex items-center gap-2"
              >
                <Server className="h-4 w-4" />
                Manage Instances
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link
                to="/backups"
                className="flex items-center gap-2"
              >
                <Archive className="h-4 w-4" />
                Instance Backups
              </Link>
            </DropdownMenuItem>

            <DropdownMenuSeparator />

            {/* Theme menu item - opens dialog */}
            <DropdownMenuItem onClick={() => setShowThemeDialog(true)}>
              <Palette className="h-4 w-4" />
              Appearance
            </DropdownMenuItem>

            <DropdownMenuSeparator />

            <DropdownMenuItem asChild>
              <a
                href="https://github.com/autobrr/qui"
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-2"
              >
                <Github className="h-4 w-4" />
                GitHub
              </a>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={() => logout()}
              className="text-destructive focus:text-destructive flex items-center gap-2"
            >
              <LogOut className="h-4 w-4 text-destructive" />
              Logout
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* Theme selection dialog */}
      <Dialog open={showThemeDialog} onOpenChange={setShowThemeDialog}>
        <DialogContent className="max-w-md max-h-[80vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Appearance</DialogTitle>
          </DialogHeader>

          <div className="space-y-4">
            {/* Mode Selection */}
            <div>
              <div className="text-sm font-medium mb-2">Mode</div>
              <div className="space-y-1">
                <button
                  onClick={() => {
                    handleModeSelect("light")
                    setShowThemeDialog(false)
                  }}
                  className={cn(
                    "w-full flex items-center gap-3 px-3 py-2 rounded-md transition-colors",
                    currentMode === "light" ? "bg-accent" : "hover:bg-accent/50"
                  )}
                >
                  <Sun className="h-4 w-4" />
                  <span className="flex-1 text-left">Light</span>
                  {currentMode === "light" && <Check className="h-4 w-4" />}
                </button>
                <button
                  onClick={() => {
                    handleModeSelect("dark")
                    setShowThemeDialog(false)
                  }}
                  className={cn(
                    "w-full flex items-center gap-3 px-3 py-2 rounded-md transition-colors",
                    currentMode === "dark" ? "bg-accent" : "hover:bg-accent/50"
                  )}
                >
                  <Moon className="h-4 w-4" />
                  <span className="flex-1 text-left">Dark</span>
                  {currentMode === "dark" && <Check className="h-4 w-4" />}
                </button>
                <button
                  onClick={() => {
                    handleModeSelect("auto")
                    setShowThemeDialog(false)
                  }}
                  className={cn(
                    "w-full flex items-center gap-3 px-3 py-2 rounded-md transition-colors",
                    currentMode === "auto" ? "bg-accent" : "hover:bg-accent/50"
                  )}
                >
                  <Monitor className="h-4 w-4" />
                  <span className="flex-1 text-left">System</span>
                  {currentMode === "auto" && <Check className="h-4 w-4" />}
                </button>
              </div>
            </div>

            {/* Theme Selection */}
            <div>
              <div className="text-sm font-medium mb-2">Theme</div>
              <div className="space-y-1">
                {themes
                  .sort((a, b) => {
                    const aIsPremium = isThemePremium(a.id)
                    const bIsPremium = isThemePremium(b.id)
                    if (aIsPremium === bIsPremium) return 0
                    return aIsPremium ? 1 : -1
                  })
                  .map((theme) => {
                    const isPremium = isThemePremium(theme.id)
                    const isLocked = isPremium && !hasPremiumAccess

                    return (
                      <button
                        key={theme.id}
                        onClick={() => {
                          if (!isLocked) {
                            handleThemeSelect(theme.id)
                            setShowThemeDialog(false)
                          }
                        }}
                        disabled={isLocked}
                        className={cn(
                          "w-full flex items-center gap-3 px-3 py-2 rounded-md transition-colors",
                          currentTheme.id === theme.id ? "bg-accent" : "hover:bg-accent/50",
                          isLocked && "opacity-60 cursor-not-allowed"
                        )}
                      >
                        <div
                          className="h-4 w-4 rounded-full ring-1 ring-black/10 dark:ring-white/10 flex-shrink-0"
                          style={{
                            backgroundColor: getThemePrimaryColor(theme),
                            backgroundImage: "none",
                            background: getThemePrimaryColor(theme) + " !important",
                          }}
                        />
                        <div className="flex items-center justify-between gap-2 flex-1 min-w-0">
                          <span className="truncate">{theme.name}</span>
                          {isPremium && (
                            <span className="text-[10px] px-1.5 py-0.5 rounded bg-secondary text-secondary-foreground font-medium flex-shrink-0">
                              Premium
                            </span>
                          )}
                        </div>
                        {currentTheme.id === theme.id && <Check className="h-4 w-4 flex-shrink-0" />}
                      </button>
                    )
                  })}
              </div>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </nav>
  )
}
