/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { UnifiedScopeDropdownSection } from "@/components/layout/UnifiedScopeDropdownSection"
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
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { getThemeById, isThemePremium, themes } from "@/config/themes"
import { useMobileScroll } from "@/contexts/MobileScrollContext"
import { useIsPhoneLandscape } from "@/hooks/useMediaQuery"
import { useTorrentSelection } from "@/contexts/TorrentSelectionContext"
import { useAuth } from "@/hooks/useAuth"
import { usePersistedCompactViewState } from "@/hooks/usePersistedCompactViewState"
import { useCrossSeedInstanceState } from "@/hooks/useCrossSeedInstanceState"
import { useCustomThemes } from "@/hooks/useCustomThemes"
import { usePersistedUnifiedInstanceFilter } from "@/hooks/usePersistedUnifiedInstanceFilter"
import { api } from "@/lib/api"
import { getAppVersion } from "@/lib/build-info"
import { changeLanguage, languageNames, supportedLanguages } from "@/i18n"
import { useBuiltinThemes } from "@/hooks/useBuiltinThemes"
import { buildThemeCatalog } from "@/lib/theme-catalog"
import { normalizeUnifiedInstanceIds } from "@/lib/instances"
import { cn } from "@/lib/utils"
import {
  getCurrentTheme,
  getCurrentThemeMode,
  getThemeColors,
  getThemeVariation,
  setTheme,
  setThemeMode,
  setThemeVariation,
  type ThemeMode
} from "@/utils/theme"
import { useQuery } from "@tanstack/react-query"
import { Link, useLocation, useNavigate, useSearch } from "@tanstack/react-router"
import { navigateWithSearch } from "@/lib/router-search"
import {
  Archive,
  Check,
  Code,
  Copyright,
  CornerDownRight,
  Download,
  FileText,
  GitBranch,
  HardDrive,
  Home,
  Loader2,
  Globe,
  LogOut,
  Monitor,
  Moon,
  Palette,
  Rss,
  SearchCode,
  Search as SearchIcon,
  Server,
  Settings,
  Sun,
  Zap
} from "lucide-react"
import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"


const MOBILE_VIEW_MODES = ["normal", "compact", "ultra-compact"] as const

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
  const { t, i18n } = useTranslation("common")
  const { t: tTorrents } = useTranslation("torrents")
  const location = useLocation()
  const navigate = useNavigate()
  const routeSearch = useSearch({ strict: false }) as Record<string, unknown> | undefined
  const { logout } = useAuth()
  const { isSelectionMode } = useTorrentSelection()
  const { isFooterVisible } = useMobileScroll()
  const isPhoneLandscape = useIsPhoneLandscape()
  const { viewMode, setViewMode } = usePersistedCompactViewState("compact", MOBILE_VIEW_MODES)
  const { currentMode, currentTheme } = useThemeChange()
  const { customThemes } = useCustomThemes()
  // Subscribe so the list re-renders when the async theme registry lands.
  useBuiltinThemes()
  const themeCatalog = buildThemeCatalog(themes, customThemes)
  const [showThemeDialog, setShowThemeDialog] = useState(false)
  const appVersion = getAppVersion()

  const { data: instances, isPending: isLoadingInstances } = useQuery({
    queryKey: ["instances"],
    queryFn: () => api.getInstances(),
  })

  const { data: updateInfo } = useQuery({
    queryKey: ["latest-version"],
    queryFn: () => api.getLatestVersion(),
    refetchInterval: 2 * 60 * 1000,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })

  const activeInstances = useMemo(() => {
    if (!instances) {
      return []
    }
    return instances.filter(instance => instance.isActive)
  }, [instances])
  const activeInstanceIds = useMemo(
    () => activeInstances.map(instance => instance.id),
    [activeInstances]
  )

  const { state: crossSeedInstanceState } = useCrossSeedInstanceState()
  const isOnAllInstancesPage = location.pathname === "/instances" || location.pathname === "/instances/"
  const isOnInstancePage = isOnAllInstancesPage || location.pathname.startsWith("/instances/")
  const [persistedUnifiedFilter, saveUnifiedFilter] = usePersistedUnifiedInstanceFilter()
  const normalizedUnifiedInstanceIds = useMemo(
    () => normalizeUnifiedInstanceIds(persistedUnifiedFilter, activeInstanceIds),
    [persistedUnifiedFilter, activeInstanceIds]
  )
  const effectiveUnifiedInstanceIds = normalizedUnifiedInstanceIds.length > 0? normalizedUnifiedInstanceIds: activeInstanceIds
  const hasMultipleActiveInstances = activeInstances.length > 1
  const applyUnifiedScope = useCallback((nextIds: number[]) => {
    const normalizedIds = normalizeUnifiedInstanceIds(nextIds, activeInstanceIds)
    saveUnifiedFilter(normalizedIds)
    const nextSearch: Record<string, unknown> = isOnAllInstancesPage ? { ...(routeSearch || {}) } : {}

    navigateWithSearch({
      navigate,
      to: "/instances",
      search: nextSearch,
      replace: isOnAllInstancesPage,
    })
  }, [activeInstanceIds, isOnAllInstancesPage, navigate, routeSearch, saveUnifiedFilter])
  const toggleUnifiedScopeInstance = useCallback((instanceId: number) => {
    const currentlySelected = effectiveUnifiedInstanceIds.includes(instanceId)
    const nextIds = currentlySelected? effectiveUnifiedInstanceIds.filter(id => id !== instanceId): [...effectiveUnifiedInstanceIds, instanceId]

    if (nextIds.length === 0) {
      return
    }

    applyUnifiedScope(nextIds)
  }, [applyUnifiedScope, effectiveUnifiedInstanceIds])
  const resetUnifiedScope = useCallback(() => {
    applyUnifiedScope(activeInstanceIds)
  }, [activeInstanceIds, applyUnifiedScope])
  const hasActiveInstances = activeInstances.length > 0
  const hasClientScopeEntry = isOnAllInstancesPage || hasActiveInstances
  const currentInstanceId = !isOnAllInstancesPage && location.pathname.startsWith("/instances/") ? location.pathname.split("/")[2] : null
  const currentInstance = instances?.find(i => i.id.toString() === currentInstanceId)
  const currentInstanceLabel = isOnAllInstancesPage? (hasMultipleActiveInstances ? t("header.unified") : (activeInstances[0]?.name ?? null)): (currentInstance && currentInstance.isActive ? currentInstance.name : null)

  const handleModeSelect = useCallback(async (mode: ThemeMode) => {
    await setThemeMode(mode)
    const modeNames = { light: t("themeToggle.light"), dark: t("themeToggle.dark"), auto: t("themeToggle.system") }
    toast.success(t("themeToggle.switchedToMode", { mode: modeNames[mode] }))
  }, [t])

  const handleThemeSelect = useCallback(async (themeId: string) => {
    // The server is the authority: a premium theme without a license arrives
    // as a locked stub with no CSS, so the locked flag is the gate.
    if (getThemeById(themeId)?.locked) {
      toast.error(t("themeToggle.premiumThemeError"))
      return
    }

    await setTheme(themeId)
    const theme = getThemeById(themeId)
    toast.success(t("themeToggle.switchedToTheme", { theme: theme?.name || themeId }))
  }, [t])

  const handleVariationSelect = useCallback(async (themeId: string, variationId: string): Promise<boolean> => {
    if (getThemeById(themeId)?.locked) {
      toast.error(t("themeToggle.premiumThemeError"))
      return false
    }

    await setTheme(themeId)
    await setThemeVariation(variationId)
    const theme = getThemeById(themeId)
    toast.success(t("themeToggle.switchedToThemeVariation", { theme: theme?.name || themeId, variation: variationId }))
    return true
  }, [t])

  if (isSelectionMode) {
    return null
  }

  return (
    <nav
      className={cn(
        "fixed bottom-0 left-0 right-0 z-40 lg:hidden",
        "bg-background/80 backdrop-blur-md border-t border-border/50",
        "transition-transform duration-300",
        "phone-land:static phone-land:order-first phone-land:w-20 phone-land:border-t-0 phone-land:border-r",
        !isFooterVisible && "translate-y-full phone-land:translate-y-0"
      )}
      style={{ paddingBottom: "env(safe-area-inset-bottom)" }}
    >
      <div className={cn("flex items-center justify-around h-16 phone-land:h-full phone-land:flex-col phone-land:py-2")}>
        {/* Dashboard */}
        <Link
          to="/dashboard"
          className={cn(
            "flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1",
            location.pathname === "/dashboard" ? "text-primary" : "text-muted-foreground hover:text-foreground"
          )}
        >
          <Home className={cn(
            "h-5 w-5",
            location.pathname === "/dashboard" && "text-primary"
          )} />
          <span className="truncate">{t("mobileNav.dashboard")}</span>
        </Link>

        {/* Clients access */}
        {hasClientScopeEntry ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                type="button"
                className={cn(
                  "flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 hover:cursor-pointer",
                  isOnInstancePage ? "text-primary" : "text-muted-foreground hover:text-foreground"
                )}
              >
                <div className="relative">
                  <HardDrive className={cn(
                    "h-5 w-5",
                    isOnInstancePage && "text-primary"
                  )} />
                  <Badge
                    className="absolute -top-1 -right-2 h-4 w-4 p-0 flex items-center justify-center text-[9px]"
                    variant="default"
                  >
                    {activeInstances.length}
                  </Badge>
                </div>
                <span
                  className="block max-w-[7.5rem] truncate text-center"
                  title={currentInstanceLabel ?? t("mobileNav.clients")}
                >
                  {currentInstanceLabel ?? t("mobileNav.clients")}
                </span>
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="center" side={isPhoneLandscape ? "right" : "top"} className="w-56 mb-2">
              <DropdownMenuLabel>{t("mobileNav.qbittorrentClients")}</DropdownMenuLabel>
              <DropdownMenuSeparator />
              {activeInstances.length > 0 ? (
                <>
                  <DropdownMenuLabel className="text-xs uppercase tracking-wide text-muted-foreground">
                    {t("mobileNav.instances")}
                  </DropdownMenuLabel>
                  {hasMultipleActiveInstances && (
                    <UnifiedScopeDropdownSection
                      activeInstances={activeInstances}
                      effectiveUnifiedInstanceIds={effectiveUnifiedInstanceIds}
                      isAllInstancesRoute={isOnAllInstancesPage}
                      onResetUnifiedScope={resetUnifiedScope}
                      onToggleUnifiedScopeInstance={toggleUnifiedScopeInstance}
                      scopeKeyPrefix="mobile-scope"
                    />
                  )}
                  {activeInstances.map((instance) => {
                    const csState = crossSeedInstanceState[instance.id]
                    const hasRss = csState?.rssEnabled || csState?.rssRunning
                    const hasSearch = csState?.searchRunning

                    return (
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
                          <span className="flex items-center gap-1.5">
                            {hasRss && (
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <span className="flex items-center">
                                    {csState?.rssRunning ? (
                                      <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
                                    ) : (
                                      <Rss className="h-3 w-3 text-muted-foreground" />
                                    )}
                                  </span>
                                </TooltipTrigger>
                                <TooltipContent side="left" className="text-xs">
                                  {csState?.rssRunning ? t("mobileNav.rssRunning") : t("mobileNav.rssEnabled")}
                                </TooltipContent>
                              </Tooltip>
                            )}
                            {hasSearch && (
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <span className="flex items-center">
                                    <SearchCode className="h-3 w-3 text-muted-foreground" />
                                  </span>
                                </TooltipTrigger>
                                <TooltipContent side="left" className="text-xs">
                                  {t("mobileNav.scanRunning")}
                                </TooltipContent>
                              </Tooltip>
                            )}
                            <span
                              className={cn(
                                "h-2 w-2 rounded-full",
                                instance.connected ? "bg-green-500" : "bg-red-500"
                              )}
                            />
                          </span>
                        </Link>
                      </DropdownMenuItem>
                    )
                  })}
                </>
              ) : (
                <DropdownMenuItem disabled className="text-xs text-muted-foreground">
                  {t("mobileNav.noActiveInstances")}
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : isLoadingInstances ? (
          <button
            className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium min-w-0 flex-1 text-muted-foreground"
            type="button"
            disabled
          >
            <HardDrive className="h-5 w-5 animate-pulse" />
            <span className="block max-w-[7.5rem] truncate text-center text-xs">{t("mobileNav.loading")}</span>
          </button>
        ) : (
          <button
            className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium min-w-0 flex-1 text-muted-foreground"
            type="button"
            disabled
          >
            <HardDrive className="h-5 w-5" />
            <span className="block max-w-[7.5rem] truncate text-center">{t("mobileNav.noActiveClients")}</span>
          </button>
        )}

        {/* Settings dropdown */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              className={cn(
                "flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 hover:cursor-pointer",
                location.pathname === "/settings" ? "text-primary" : "text-muted-foreground hover:text-foreground"
              )}
            >
              <div className="relative">
                <Settings className={cn(
                  "h-5 w-5",
                  location.pathname === "/settings" && "text-primary"
                )} />
                {updateInfo && (
                  <Badge
                    className="absolute -top-1 -right-2 h-4 w-4 p-0 flex items-center justify-center bg-green-500 hover:bg-green-500 text-white"
                    variant="default"
                  >
                    <Download className="h-2.5 w-2.5" />
                  </Badge>
                )}
              </div>
              <span className="truncate">{t("mobileNav.settings")}</span>
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" side={isPhoneLandscape ? "right" : "top"} className="mb-2 w-56">
            {updateInfo && (
              <>
                <DropdownMenuItem asChild>
                  <a
                    href={updateInfo.html_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-2 text-green-600 dark:text-green-400 focus:text-green-600 dark:focus:text-green-400"
                  >
                    <Download className="h-4 w-4" />
                    <div className="flex flex-col">
                      <span className="font-medium">{t("mobileNav.updateAvailable")}</span>
                      <span className="text-[10px] opacity-80">{t("mobileNav.updateVersion", { version: updateInfo.tag_name })}</span>
                    </div>
                  </a>
                </DropdownMenuItem>
                <DropdownMenuSeparator />
              </>
            )}
            <DropdownMenuItem asChild>
              <Link
                to="/search"
                className="flex items-center gap-2"
              >
                <SearchIcon className="h-4 w-4" />
                {t("nav.search")}
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link
                to="/cross-seed"
                params={{}}
                className="flex items-center gap-2"
              >
                <GitBranch className="h-4 w-4" />
                {t("nav.crossSeed")}
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link
                to="/automations"
                className="flex items-center gap-2"
              >
                <Zap className="h-4 w-4" />
                {t("nav.automations")}
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link
                to="/backups"
                className="flex items-center gap-2"
              >
                <Archive className="h-4 w-4" />
                {t("nav.instanceBackups")}
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link
                to="/rss"
                className="flex items-center gap-2"
              >
                <Rss className="h-4 w-4" />
                {t("nav.rss")}
              </Link>
            </DropdownMenuItem>

            <DropdownMenuSeparator />

            <DropdownMenuItem asChild>
              <Link
                to="/settings"
                className="flex items-center gap-2"
              >
                <Settings className="h-4 w-4" />
                {t("nav.generalSettings")}
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link
                to="/settings"
                search={{ tab: "instances" }}
                className="flex items-center gap-2"
              >
                <Server className="h-4 w-4" />
                {t("nav.manageInstances")}
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <Link
                to="/settings"
                search={{ tab: "logs" }}
                className="flex items-center gap-2"
              >
                <FileText className="h-4 w-4" />
                {t("nav.logs")}
              </Link>
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => setShowThemeDialog(true)}>
              <Palette className="h-4 w-4" />
              {t("nav.appearance")}
            </DropdownMenuItem>

            <DropdownMenuSeparator />

            <div className="flex items-center justify-between px-3 py-2">
              <div className="flex flex-col gap-0.5 text-[10px] text-muted-foreground/60 select-none">
                <span className="font-medium text-muted-foreground/70">{t("mobileNav.version", { version: appVersion })}</span>
                <div className="flex items-center gap-1">
                  <Copyright className="h-2.5 w-2.5 flex-shrink-0" />
                  <span>{new Date().getFullYear()} autobrr</span>
                </div>
              </div>
              <a
                href="https://github.com/autobrr/qui"
                target="_blank"
                rel="noopener noreferrer"
                aria-label={t("mobileNav.viewOnGitHub")}
                className="h-6 w-6 flex items-center justify-center text-muted-foreground/60 hover:text-foreground transition-colors"
              >
                <Code className="h-3.5 w-3.5" />
              </a>
            </div>
            <DropdownMenuSeparator />
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <Globe className="h-4 w-4" />
                {languageNames[i18n.language as keyof typeof languageNames] ?? i18n.language}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent>
                {supportedLanguages.map((lng) => (
                  <DropdownMenuItem
                    key={lng}
                    onClick={() => changeLanguage(lng)}
                    className="flex items-center justify-between gap-4"
                  >
                    {languageNames[lng]}
                    {i18n.language === lng && <Check className="h-3.5 w-3.5" />}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={() => logout()}
              className="text-destructive focus:text-destructive flex items-center gap-2"
            >
              <LogOut className="h-4 w-4 text-destructive" />
              {t("mobileNav.logout")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* Theme selection dialog */}
      <Dialog open={showThemeDialog} onOpenChange={setShowThemeDialog}>
        <DialogContent className="max-w-md max-h-[80vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{t("themeToggle.appearance")}</DialogTitle>
          </DialogHeader>

          <div className="space-y-4">
            {/* Mode Selection */}
            <div>
              <div className="text-sm font-medium mb-2">{t("themeToggle.mode")}</div>
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
                  <span className="flex-1 text-left">{t("themeToggle.light")}</span>
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
                  <span className="flex-1 text-left">{t("themeToggle.dark")}</span>
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
                  <span className="flex-1 text-left">{t("themeToggle.system")}</span>
                  {currentMode === "auto" && <Check className="h-4 w-4" />}
                </button>
              </div>
            </div>

            {/* Torrent list view mode */}
            <div>
              <div className="text-sm font-medium mb-2">{tTorrents("filterSidebar.viewMode")}</div>
              <div className="grid grid-cols-3 gap-1">
                {MOBILE_VIEW_MODES.map((mode) => (
                  <button
                    key={mode}
                    onClick={() => setViewMode(mode)}
                    className={cn(
                      "px-3 py-2 rounded-md text-sm transition-colors",
                      viewMode === mode ? "bg-accent" : "hover:bg-accent/50"
                    )}
                  >
                    {mode === "normal"? tTorrents("filterSidebar.viewModeNormal"): mode === "compact"? tTorrents("filterSidebar.viewModeCompact"): tTorrents("filterSidebar.viewModeUltra")}
                  </button>
                ))}
              </div>
            </div>

            {/* Theme Selection */}
            <div>
              <div className="text-sm font-medium mb-2">{t("themeToggle.theme")}</div>
              <div className="space-y-1">
                {themeCatalog.map((theme) => {
                  const isPremium = isThemePremium(theme.id)
                  const isLocked = !!theme.locked
                  const colors = getThemeColors(theme)
                  const currentVariation = getThemeVariation(theme.id)

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
                      <div className="flex-1">
                        <div className="flex items-center gap-3">
                          <div
                            className="h-4 w-4 rounded-full ring-1 ring-black/10 dark:ring-white/10 flex-shrink-0"
                            style={{
                              backgroundColor: colors.primary,
                              backgroundImage: "none",
                              background: colors.primary + " !important",
                            }}
                          />
                          <div className="flex items-center gap-2 flex-1 min-w-0">
                            <span className="truncate">{theme.name}</span>
                            {isPremium && (
                              <span className="text-[10px] px-1.5 py-0.5 rounded bg-secondary text-secondary-foreground font-medium flex-shrink-0">
                                {t("themeToggle.premium")}
                              </span>
                            )}
                          </div>
                        </div>

                        {/* Variation pills */}
                        {colors.variations && colors.variations.length > 0 && (
                          <div className="flex items-center gap-2 pl-1.5 mt-2">
                            <CornerDownRight className="h-4 w-4 text-muted-foreground" />
                            <div className="flex gap-2">
                              {colors.variations.map((variation) => {
                                const isSelected = currentVariation === variation.id
                                return (
                                  <div
                                    key={variation.id}
                                    onClick={async (e) => {
                                      e.stopPropagation()
                                      const success = await handleVariationSelect(theme.id, variation.id)
                                      if (success) {
                                        setShowThemeDialog(false)
                                      }
                                    }}
                                    className={cn(
                                      "w-8 h-8 rounded-full transition-all cursor-pointer",
                                      isSelected? "ring-2 ring-black dark:ring-white": "ring-1 ring-black/10 dark:ring-white/10"
                                    )}
                                    style={{
                                      backgroundColor: variation.color,
                                      backgroundImage: "none",
                                      background: variation.color + " !important",
                                    }}
                                  />
                                )
                              })}
                            </div>
                          </div>
                        )}
                      </div>
                      {currentTheme.id === theme.id && <Check className="h-4 w-4 flex-shrink-0 self-center" />}
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
