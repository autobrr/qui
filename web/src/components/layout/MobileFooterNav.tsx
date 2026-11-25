/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { openCommandPalette } from "@/components/command-palette/open"
import { MobileClientsTab } from "@/components/layout/MobileClientsTab"
import { SystemNoticeMenuItems } from "@/components/layout/SystemNoticeMenuItems"
import { openSupport } from "@/components/support/open"
import { useSupportShortcutHidden } from "@/components/support/reminder"
import { BetaBadge } from "@/components/transfers/BetaBadge"
import { SystemNoticeDialog } from "@/components/ui/SystemNotice"
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
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { getThemeById, themes } from "@/config/themes"
import { useMobileScroll } from "@/contexts/MobileScrollContext"
import { useTorrentSelectionContext } from "@/contexts/TorrentSelectionContext"
import { useAuth } from "@/hooks/useAuth"
import { useIsMobile } from "@/hooks/useMediaQuery"
import { usePersistedCompactViewState } from "@/hooks/usePersistedCompactViewState"
import { useTransferQueue } from "@/hooks/useTransferQueue"
import { useCustomThemes } from "@/hooks/useCustomThemes"
import { useHasPremiumAccess } from "@/hooks/useLicense"
import type { Theme } from "@/config/themes"
import { api } from "@/lib/api"
import { getAppVersion } from "@/lib/build-info"
import { demoLinks, isDemo } from "@/lib/demo"
import { changeLanguage, languageNames, supportedLanguages } from "@/i18n"
import { useBuiltinThemes } from "@/hooks/useBuiltinThemes"
import { buildThemeCatalog } from "@/lib/theme-catalog"
import { themeThumbnailPalette, themeVariationColors } from "@/lib/theme-thumbnail"
import { QuickThemeRow, QuickThemeVariations } from "@/components/themes/QuickThemeRow"
import { useThemeVisibility } from "@/hooks/useThemeVisibility"
import { hasSelection } from "@/lib/torrent-selection"
import { systemNoticeDotColor, topSystemNoticeSeverity } from "@/lib/system-notice"
import { cn } from "@/lib/utils"
import {
  getCurrentTheme,
  getCurrentThemeMode,
  getThemeVariation,
  isDarkMode,
  setTheme,
  setThemeMode,
  setThemeVariation,
  type ThemeMode
} from "@/utils/theme"
import { useOpenSystemNotice, useSystemNotices } from "@/hooks/useSystemNotices"
import { useQuery } from "@tanstack/react-query"
import { Link, useLocation, useSearch } from "@tanstack/react-router"
import { themeArea } from "@/lib/theme-areas"
import {
  Archive,
  ArrowRightLeft,
  ChartColumn,
  Check,
  Code,
  Copyright,
  Download,
  FileText,
  GitBranch,
  Heart,
  Home,
  ListChecks,
  Globe,
  LogOut,
  Monitor,
  Moon,
  Palette,
  Rss,
  Search as SearchIcon,
  Server,
  Settings,
  Sun,
  Zap
} from "lucide-react"
import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
const useThemeChange = (customThemes: readonly Theme[]) => {
  const [currentMode, setCurrentMode] = useState<ThemeMode>(getCurrentThemeMode())
  const [currentTheme, setCurrentTheme] = useState(getCurrentTheme())
  const [dark, setDark] = useState(() => isDarkMode(getCurrentThemeMode()))

  const checkTheme = useCallback(() => {
    setCurrentMode(getCurrentThemeMode())
    setCurrentTheme(getCurrentTheme())
    setDark(isDarkMode(getCurrentThemeMode()))
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

  useEffect(checkTheme, [checkTheme, customThemes])

  return { currentMode, currentTheme, dark }
}

export function MobileFooterNav() {
  const { t, i18n } = useTranslation("common")
  const { t: tTorrents } = useTranslation("torrents")
  const location = useLocation()
  const routeSearch = useSearch({ strict: false }) as Record<string, unknown> | undefined
  const { logout } = useAuth()
  const transferQueue = useTransferQueue()
  const isSelectionMode = hasSelection(useTorrentSelectionContext().selection)
  const { isFooterVisible } = useMobileScroll()
  const isMobile = useIsMobile()
  const { viewMode, setViewMode, viewModes } = usePersistedCompactViewState(isMobile ? "mobile" : "desktop")
  const { customThemes } = useCustomThemes()
  const { currentMode, currentTheme, dark } = useThemeChange(customThemes)
  const [themeVisibility] = useThemeVisibility()
  useBuiltinThemes()
  const themeCatalog = buildThemeCatalog(themes, customThemes, { visibility: themeVisibility, currentThemeId: currentTheme.id })
  const [showThemeDialog, setShowThemeDialog] = useState(false)
  const supportShortcutHidden = useSupportShortcutHidden()
  const { isSupporter } = useHasPremiumAccess()
  const appVersion = getAppVersion()

  const { data: updateInfo } = useQuery({
    queryKey: ["latest-version"],
    queryFn: () => api.getLatestVersion(),
    refetchInterval: 2 * 60 * 1000,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })
  const systemNotices = useSystemNotices()
  const openSystemNotice = useOpenSystemNotice(systemNotices)
  const updateItemClass = "flex items-center gap-2 text-green-600 dark:text-green-400 focus:text-green-600 dark:focus:text-green-400"
  const updateItemLabel = updateInfo && (
    <>
      <Download className="h-4 w-4" />
      <div className="flex flex-col">
        <span className="font-medium">{t("mobileNav.updateAvailable")}</span>
        <span className="text-[10px] opacity-80">{t("mobileNav.updateVersion", { version: updateInfo.tag_name })}</span>
      </div>
    </>
  )

  const isOnInstancePage = location.pathname === "/instances" || location.pathname.startsWith("/instances/")

  const handleModeSelect = useCallback(async (mode: ThemeMode) => {
    await setThemeMode(mode)
    const modeNames = { light: t("themeToggle.light"), dark: t("themeToggle.dark"), auto: t("themeToggle.system") }
    toast.success(t("themeToggle.switchedToMode", { mode: modeNames[mode] }))
  }, [t])

  const handleThemeSelect = useCallback(async (themeId: string) => {
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
      {...themeArea("mobile-nav")}
      className={cn(
        "fixed bottom-0 left-0 right-0 z-40 lg:hidden",
        "bg-background/80 backdrop-blur-md border-t border-border/50",
        "transition-transform duration-300",
        !isFooterVisible && "translate-y-full"
      )}
      style={{ paddingBottom: "env(safe-area-inset-bottom)" }}
    >
      <div className="flex items-center justify-around h-16">
        {!isDemo && <Link
          to="/dashboard"
          className={cn(
            "flex flex-col items-center justify-center gap-1 px-0.5 py-2 text-xs font-medium transition-colors min-w-0 flex-1",
            location.pathname === "/dashboard" ? "text-primary" : "text-muted-foreground hover:text-foreground"
          )}
        >
          <Home className={cn(
            "h-5 w-5",
            location.pathname === "/dashboard" && "text-primary"
          )} />
          <span className="max-w-full truncate">{t("mobileNav.dashboard")}</span>
        </Link>}

        {!isDemo && <Link
          to="/statistics"
          className={cn(
            "flex flex-col items-center justify-center gap-1 px-0.5 py-2 text-xs font-medium transition-colors min-w-0 flex-1",
            location.pathname === "/statistics" ? "text-primary" : "text-muted-foreground hover:text-foreground"
          )}
        >
          <ChartColumn className="h-5 w-5" />
          <span className="max-w-full truncate">{t("mobileNav.statistics")}</span>
        </Link>}

        <button
          type="button"
          onClick={() => openCommandPalette(isOnInstancePage ? String(routeSearch?.q ?? "") : "")}
          className="flex flex-col items-center justify-center gap-1 px-0.5 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground hover:cursor-pointer"
        >
          <SearchIcon className="h-5 w-5" />
          <span className="max-w-full truncate">{t("actions.search")}</span>
        </button>

        <MobileClientsTab />

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              className={cn(
                "flex flex-col items-center justify-center gap-1 px-0.5 py-2 text-xs font-medium transition-colors min-w-0 flex-1 hover:cursor-pointer",
                location.pathname === "/settings" ? "text-primary" : "text-muted-foreground hover:text-foreground"
              )}
            >
              <div className="relative">
                <Settings className={cn(
                  "h-5 w-5",
                  location.pathname === "/settings" && "text-primary"
                )} />
                {systemNotices.length > 0 && (
                  <span className={cn("absolute -top-1 -left-2 h-2.5 w-2.5 rounded-full", systemNoticeDotColor[topSystemNoticeSeverity(systemNotices)].fill)} />
                )}
                {updateInfo && (
                  <Badge
                    className="absolute -top-1 -right-2 h-4 w-4 p-0 flex items-center justify-center bg-green-500 hover:bg-green-500 text-white"
                    variant="default"
                  >
                    <Download className="h-2.5 w-2.5" />
                  </Badge>
                )}
              </div>
              <span className="max-w-full truncate">{t("mobileNav.settings")}</span>
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" side="top" className={cn("mb-2", systemNotices.length > 0 ? "w-72" : "w-56")}>
            <SystemNoticeMenuItems notices={systemNotices} onOpen={openSystemNotice.open} />
            {updateInfo && (
              <>
                <DropdownMenuItem asChild>
                  <Link to="/settings" search={{ tab: "application", modal: "install-update" }} className={updateItemClass}>{updateItemLabel}</Link>
                </DropdownMenuItem>
                <DropdownMenuSeparator />
              </>
            )}
            {!isDemo && (
              <>
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
                    to="/transfers"
                    className="flex items-center gap-2"
                  >
                    <ArrowRightLeft className="h-4 w-4" />
                    {t("nav.transfers")}
                    <BetaBadge />
                    {transferQueue > 0 && (
                      <Badge variant="default" className="ml-auto" aria-label={t("nav.transfersQueued", { count: transferQueue })}>
                        {transferQueue}
                      </Badge>
                    )}
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
              </>
            )}
            <DropdownMenuItem onClick={() => setShowThemeDialog(true)}>
              <Palette className="h-4 w-4" />
              {t("nav.appearance")}
            </DropdownMenuItem>
            {!supportShortcutHidden && (
              <DropdownMenuItem onClick={openSupport}>
                <Heart className="h-4 w-4 fill-current text-red-500" />
                {isSupporter ? t("support.supporter.title") : t("support.title")}
              </DropdownMenuItem>
            )}

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
            {isDemo && demoLinks.map((link) => (
              <DropdownMenuItem asChild key={link.href}>
                <a href={link.href} target="_top" className="flex items-center gap-2">
                  <link.icon className="h-4 w-4" />
                  {t(link.labelKey)}
                </a>
              </DropdownMenuItem>
            ))}
            {!isDemo && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  onClick={() => logout()}
                  className="text-destructive focus:text-destructive flex items-center gap-2"
                >
                  <LogOut className="h-4 w-4 text-destructive" />
                  {t("mobileNav.logout")}
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <Dialog open={showThemeDialog} onOpenChange={setShowThemeDialog}>
        <DialogContent className="flex max-h-[85dvh] max-w-md flex-col gap-4 overflow-hidden [@media(max-height:35rem)]:overflow-y-auto">
          <DialogHeader className="shrink-0">
            <DialogTitle>{t("themeToggle.appearance")}</DialogTitle>
          </DialogHeader>

          <div className="shrink-0 space-y-4">
            <div>
              <div className="text-sm font-medium mb-2">{t("themeToggle.mode")}</div>
              <div role="group" aria-label={t("themeToggle.mode")} className="grid grid-cols-3 gap-1">
                {([
                  { mode: "light", icon: Sun, label: t("themeToggle.light") },
                  { mode: "dark", icon: Moon, label: t("themeToggle.dark") },
                  { mode: "auto", icon: Monitor, label: t("themeToggle.system") },
                ] as const).map(({ mode, icon: Icon, label }) => (
                  <button
                    key={mode}
                    type="button"
                    aria-pressed={currentMode === mode}
                    onClick={() => {
                      handleModeSelect(mode)
                      setShowThemeDialog(false)
                    }}
                    className={cn(
                      "flex flex-col items-center gap-1 rounded-md border px-2 py-2 text-sm transition-colors",
                      currentMode === mode ? "border-primary/50 bg-primary/10 font-medium" : "border-transparent text-muted-foreground hover:bg-accent/50"
                    )}
                  >
                    <Icon className="h-4 w-4" />
                    {label}
                  </button>
                ))}
              </div>
            </div>

            <div>
              <div className="text-sm font-medium mb-2">{tTorrents("filterSidebar.viewMode")}</div>
              <div className="grid grid-cols-3 gap-1">
                {viewModes.map((mode) => (
                  <button
                    key={mode}
                    aria-pressed={viewMode === mode}
                    onClick={() => setViewMode(mode)}
                    className={cn(
                      "px-3 py-2 rounded-md border text-sm transition-colors",
                      viewMode === mode ? "border-primary/50 bg-primary/10 font-medium" : "border-transparent text-muted-foreground hover:bg-accent/50"
                    )}
                  >
                    {isMobile
                      ? mode === "normal"? tTorrents("filterSidebar.viewModeNormal"): mode === "compact"? tTorrents("filterSidebar.viewModeCompact"): tTorrents("filterSidebar.viewModeUltra")
                      : mode === "normal"? tTorrents("statusBar.viewModes.table"): mode === "dense"? tTorrents("statusBar.viewModes.dense"): tTorrents("statusBar.viewModes.stacked")}
                  </button>
                ))}
              </div>
            </div>

            <div className="text-sm font-medium">{t("themeToggle.theme")}</div>
          </div>

          <div className="-mx-6 -mt-2 min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 [@media(max-height:35rem)]:flex-none [@media(max-height:35rem)]:overflow-visible">
            <div className="space-y-1">
              {themeCatalog.map((theme) => {
                const isLocked = !!theme.locked
                const isCurrent = currentTheme.id === theme.id
                const variation = getThemeVariation(theme.id)
                const variations = themeVariationColors(theme, dark)

                return (
                  <div key={theme.id} className={cn("rounded-md", isCurrent && "bg-accent")}>
                    <button
                      type="button"
                      aria-current={isCurrent ? "true" : undefined}
                      onClick={() => {
                        handleThemeSelect(theme.id)
                        setShowThemeDialog(false)
                      }}
                      disabled={isLocked}
                      className={cn(
                        "w-full rounded-md px-3 py-2 text-left text-sm transition-colors",
                        !isCurrent && "hover:bg-accent/50",
                        isLocked && "cursor-not-allowed opacity-60"
                      )}
                    >
                      <QuickThemeRow
                        theme={theme}
                        palette={themeThumbnailPalette(theme, dark, variation)}
                        variations={variations}
                        isCurrent={isCurrent}
                      />
                    </button>
                    {isCurrent && variations.length > 0 && (
                      <QuickThemeVariations
                        variations={variations}
                        selected={variation}
                        onSelect={async (variationId) => {
                          if (await handleVariationSelect(theme.id, variationId)) setShowThemeDialog(false)
                        }}
                        className="gap-0 px-3 pb-2 [&_button]:size-11 [&_button>span]:size-7"
                      />
                    )}
                  </div>
                )
              })}
            </div>
          </div>

          <div className="-mx-6 -mb-4 shrink-0 border-t px-6 pt-2">
            <Link
              to="/settings"
              search={{ tab: "themes", modal: "theme-menu" }}
              onClick={() => setShowThemeDialog(false)}
              className="flex min-h-11 items-center gap-2 rounded-md px-3 text-sm hover:bg-accent/50"
            >
              <ListChecks className="h-4 w-4" />
              {t("settings:themes.menu.button")}
            </Link>
          </div>
        </DialogContent>
      </Dialog>
      <SystemNoticeDialog notice={openSystemNotice.notice} returnFocusTo={openSystemNotice.menuButton} onClose={openSystemNotice.close} />
    </nav>
  )
}
