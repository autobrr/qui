/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
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
import { NAV_ITEMS } from "@/components/layout/navigation"
import { BetaBadge } from "@/components/transfers/BetaBadge"
import { UnifiedScopeDropdownSection } from "@/components/layout/UnifiedScopeDropdownSection"
import { Logo } from "@/components/ui/Logo"
import { NapsterLogo } from "@/components/ui/NapsterLogo"
import { Separator } from "@/components/ui/separator"
import { SwizzinLogo } from "@/components/ui/SwizzinLogo"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { SystemNoticeList } from "@/components/ui/SystemNoticeList"
import { UpdateBanner } from "@/components/ui/UpdateBanner"
import { useAuth } from "@/hooks/useAuth"
import { useCrossSeedInstanceState } from "@/hooks/useCrossSeedInstanceState"
import { useTransferQueue } from "@/hooks/useTransferQueue"
import { usePersistedUnifiedInstanceFilter } from "@/hooks/usePersistedUnifiedInstanceFilter"
import { useTheme } from "@/hooks/useTheme"
import { changeLanguage, languageNames, supportedLanguages } from "@/i18n"
import { api } from "@/lib/api"
import { useClientSettingsSettled } from "@/lib/client-settings"
import { getAppVersion } from "@/lib/build-info"
import { demoLinks, isDemo } from "@/lib/demo"
import { instanceInitials, normalizeUnifiedInstanceIds } from "@/lib/instances"
import { cn, collapseRowClassName } from "@/lib/utils"
import { useQuery } from "@tanstack/react-query"
import { Link, useLocation, useNavigate, useSearch } from "@tanstack/react-router"
import { navigateWithSearch } from "@/lib/router-search"
import { themeArea } from "@/lib/theme-areas"
import {
  Check,
  Code,
  Copyright,
  Globe,
  HardDrive,
  Loader2,
  Ellipsis,
  LogOut,
  PanelLeftClose,
  PanelLeftOpen,
  Rss,
  SearchCode
} from "lucide-react"
import { useCallback, useMemo, type ReactElement, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

const rowClassName = "relative flex items-center gap-3 rounded-md px-3 py-1.5 text-sm font-medium short:py-1 short:text-[13px]/5"
// The icon-only sidebar is as wide as the padding and the icon of a row. The row cuts off the rest there, so each icon
// stays where it is at full width.
const iconOnlyRowClassName = "overflow-hidden whitespace-nowrap"
const rowStateClassName = (isActive: boolean) => isActive? "bg-sidebar-primary text-sidebar-primary-foreground": "text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground hover:transition-colors hover:duration-200 hover:ease-out"
const footerButtonClassName = "h-6 w-6 text-sidebar-foreground/40 hover:text-sidebar-foreground"

function IconOnlyLabel({ show, label, children }: { show: boolean; label: ReactNode; children: ReactElement }) {
  return (
    <Tooltip delayDuration={150} disabled={!show}>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent side="right" className="max-w-64 break-words text-xs">{label}</TooltipContent>
    </Tooltip>
  )
}

interface SidebarProps {
  iconOnly: boolean
  onToggleIconOnly: () => void
}

export function Sidebar({ iconOnly, onToggleIconOnly }: SidebarProps) {
  const { t, i18n } = useTranslation("common")
  const location = useLocation()
  const navigate = useNavigate()
  const routeSearch = useSearch({ strict: false }) as Record<string, unknown> | undefined
  const { logout } = useAuth()
  const { theme } = useTheme()
  const transferQueue = useTransferQueue()
  const settingsSettled = useClientSettingsSettled()

  const { data: instances } = useQuery({
    queryKey: ["instances"],
    queryFn: () => api.getInstances(),
  })
  const activeInstances = useMemo(
    () => (instances ?? []).filter(instance => instance.isActive),
    [instances]
  )
  const activeInstanceIds = useMemo(
    () => activeInstances.map(instance => instance.id),
    [activeInstances]
  )
  const activeInstanceInitials = useMemo(
    () => instanceInitials(activeInstances.map(instance => instance.name)),
    [activeInstances]
  )
  const [persistedUnifiedFilter, saveUnifiedFilter] = usePersistedUnifiedInstanceFilter()
  const normalizedUnifiedInstanceIds = useMemo(
    () => normalizeUnifiedInstanceIds(persistedUnifiedFilter, activeInstanceIds),
    [persistedUnifiedFilter, activeInstanceIds]
  )
  const effectiveUnifiedInstanceIds = normalizedUnifiedInstanceIds.length > 0? normalizedUnifiedInstanceIds: activeInstanceIds
  const isAllInstancesActive = location.pathname === "/instances" || location.pathname === "/instances/"
  const hasMultipleActiveInstances = activeInstances.length > 1
  const applyUnifiedScope = useCallback((nextIds: number[]) => {
    const normalizedIds = normalizeUnifiedInstanceIds(nextIds, activeInstanceIds)
    saveUnifiedFilter(normalizedIds)
    const nextSearch: Record<string, unknown> = isAllInstancesActive ? { ...(routeSearch || {}) } : {}

    navigateWithSearch({
      navigate,
      to: "/instances",
      search: nextSearch,
      replace: isAllInstancesActive,
    })
  }, [activeInstanceIds, isAllInstancesActive, navigate, routeSearch, saveUnifiedFilter])
  const toggleUnifiedScopeInstance = useCallback((instanceId: number) => {
    const currentlySelected = effectiveUnifiedInstanceIds.includes(instanceId)
    const nextIds = currentlySelected? effectiveUnifiedInstanceIds.filter(id => id !== instanceId): [...effectiveUnifiedInstanceIds, instanceId]

    if (nextIds.length === 0) {
      return
    }

    applyUnifiedScope(nextIds)
  }, [applyUnifiedScope, effectiveUnifiedInstanceIds])
  const hasConfiguredInstances = (instances?.length ?? 0) > 0

  const { state: crossSeedInstanceState } = useCrossSeedInstanceState()

  const appVersion = getAppVersion()

  const brand = (
    <span className="flex items-center gap-2">
      {theme === "swizzin" ? (
        <SwizzinLogo className="h-5 w-5" />
      ) : theme === "napster" ? (
        <NapsterLogo className="h-5 w-5" />
      ) : (
        <Logo className="h-5 w-5" />
      )}
      qui
    </span>
  )

  // A label stays in the page for a screen reader, and fades while the sidebar changes width.
  const fadeClassName = "transition-opacity duration-200 motion-reduce:transition-none"
  const labelClassName = cn(fadeClassName, iconOnly && "opacity-0")
  // The text of a row keeps the width that it has in the full sidebar, so that it does not move while it fades.
  const rowLabelClassName = (fullWidthClassName: string) => cn("flex min-w-0 flex-1 items-center gap-3 whitespace-nowrap", labelClassName, iconOnly && [fullWidthClassName, "shrink-0"])
  // For two marks that have the same place: one goes before the other comes.
  const swapClassName = (shown: boolean) => cn("transition-opacity duration-100 motion-reduce:transition-none", shown ? "delay-100" : "opacity-0")
  // The icon of an instance and its initials: one shrinks and fades while the other grows in its place, so the row always shows a mark.
  const morphClassName = (shown: boolean) => cn("transition-[opacity,scale] duration-200 ease-out motion-reduce:transition-none", !shown && "scale-50 opacity-0")
  const cornerDotClassName = cn("absolute left-[31px] top-1 h-1.5 w-1.5 rounded-full", fadeClassName, !iconOnly && "opacity-0")
  const languageItems = supportedLanguages.map((lng) => (
    <DropdownMenuItem
      key={lng}
      onClick={() => changeLanguage(lng)}
      className="flex items-center justify-between gap-4"
    >
      {languageNames[lng]}
      {i18n.language === lng && <Check className="h-3.5 w-3.5" />}
    </DropdownMenuItem>
  ))
  const iconOnlyToggleLabel = iconOnly ? t("sidebar.expandSidebar") : t("sidebar.collapseToIcons")
  const IconOnlyToggleIcon = iconOnly ? PanelLeftOpen : PanelLeftClose

  return (
    <div {...themeArea("app-sidebar")} className="flex h-full w-full flex-col border-r bg-sidebar border-sidebar-border">
      {/* The button stays at the right edge of the part that shows. At the icon-only width the brand has no room left, and the button is in the icon column. */}
      <div className="flex items-center py-3 pl-6 pr-[18px] short:py-2">
        <h2 inert={iconOnly} className={cn("flex min-w-0 flex-1 items-center overflow-hidden whitespace-nowrap text-lg font-semibold text-sidebar-foreground", labelClassName)}>
          {isDemo ? (
            <a href="/" target="_top" className="flex items-center gap-2 hover:opacity-80">{brand}</a>
          ) : brand}
        </h2>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="sticky right-[18px] -ml-1.5 h-7 w-7 flex-shrink-0 bg-sidebar text-sidebar-foreground/50 hover:text-sidebar-foreground"
              onClick={onToggleIconOnly}
              disabled={!settingsSettled}
              aria-label={iconOnlyToggleLabel}
            >
              <IconOnlyToggleIcon className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side={iconOnly ? "right" : "bottom"} className="text-xs">
            {iconOnlyToggleLabel}
          </TooltipContent>
        </Tooltip>
      </div>

      <nav className={cn("flex flex-1 min-h-0 flex-col overflow-y-auto px-3", iconOnly && "[scrollbar-width:none]")}>
        <div className="space-y-0.5 short:space-y-px">
          {!isDemo && NAV_ITEMS.map((item) => {
            const Icon = item.icon
            const isActive = item.isActive? item.isActive(location.pathname, location.search): location.pathname === item.href

            const title = t(item.titleKey)
            const queued = item.id === "transfers" && transferQueue > 0 ? t("nav.transfersQueued", { count: transferQueue }) : null

            return (
              <IconOnlyLabel
                key={item.id}
                show={iconOnly}
                label={queued ? <>{title}<span className="block opacity-70">{queued}</span></> : title}
              >
                <Link
                  to={item.href}
                  params={item.params}
                  search={item.search}
                  className={cn(rowClassName, iconOnly && iconOnlyRowClassName, rowStateClassName(isActive))}
                >
                  <Icon className="h-4 w-4 flex-shrink-0" />
                  <span className={rowLabelClassName("min-w-45")}>
                    {title}
                    {item.id === "transfers" && <BetaBadge />}
                    {queued && (
                      <span
                        className="ml-auto min-w-5 rounded-full bg-primary px-1.5 text-center text-xs text-primary-foreground"
                        aria-label={queued}
                      >
                        {transferQueue}
                      </span>
                    )}
                  </span>
                  {queued && (
                    <span className={cn(cornerDotClassName, isActive ? "bg-sidebar-primary-foreground" : "bg-primary")} aria-hidden="true" />
                  )}
                </Link>
              </IconOnlyLabel>
            )
          })}
          {isDemo && demoLinks.map((link) => (
            <IconOnlyLabel key={link.href} show={iconOnly} label={t(link.labelKey)}>
              <a
                href={link.href}
                target="_top"
                className={cn(rowClassName, iconOnly && iconOnlyRowClassName, "text-sidebar-foreground transition-all duration-200 ease-out hover:bg-sidebar-accent hover:text-sidebar-accent-foreground")}
              >
                <link.icon className="h-4 w-4 flex-shrink-0" />
                <span className={labelClassName}>{t(link.labelKey)}</span>
              </a>
            </IconOnlyLabel>
          ))}
        </div>

        <Separator className="my-2 short:my-1.5" />

        <div className={cn("shrink-0", collapseRowClassName(iconOnly))}>
          <p className="min-h-0 overflow-hidden whitespace-nowrap px-3 text-xs font-semibold uppercase tracking-wider text-sidebar-foreground/70">
            {t("sidebar.instances")}
          </p>
        </div>
        {/* A scrollbar that takes room would leave less than an icon of the icon-only width. */}
        <div className={cn("mt-1 space-y-0.5 overflow-y-auto short:space-y-px", iconOnly ? "[scrollbar-width:none]" : "pr-1", hasMultipleActiveInstances ? "min-h-24" : "shrink-0")}>
          {hasMultipleActiveInstances && (
            <>
              {iconOnly ? (
                <IconOnlyLabel
                  show
                  label={(
                    <>
                      {t("unifiedScope.unified")}
                      {effectiveUnifiedInstanceIds.length !== activeInstances.length && (
                        <span className="block tabular-nums opacity-70">{effectiveUnifiedInstanceIds.length}/{activeInstances.length}</span>
                      )}
                    </>
                  )}
                >
                  <Link to="/instances" className={cn(rowClassName, iconOnlyRowClassName, rowStateClassName(isAllInstancesActive))}>
                    <HardDrive className="h-4 w-4 flex-shrink-0" />
                    <span className="opacity-0">{t("unifiedScope.unified")}</span>
                  </Link>
                </IconOnlyLabel>
              ) : (
                <UnifiedScopeDropdownSection
                  activeInstances={activeInstances}
                  effectiveUnifiedInstanceIds={effectiveUnifiedInstanceIds}
                  isAllInstancesRoute={isAllInstancesActive}
                  onResetUnifiedScope={() => applyUnifiedScope(activeInstanceIds)}
                  onToggleUnifiedScopeInstance={toggleUnifiedScopeInstance}
                  scopeKeyPrefix="sidebar-scope"
                  variant="sidebar"
                />
              )}
              <Separator className="my-1" />
            </>
          )}
          {activeInstances.map((instance, index) => {
            const instancePath = `/instances/${instance.id}`
            const isActive = location.pathname === instancePath || location.pathname.startsWith(`${instancePath}/`)
            const csState = crossSeedInstanceState[instance.id]
            const hasRss = csState?.rssEnabled || csState?.rssRunning
            const hasSearch = csState?.searchRunning

            const initials = activeInstanceInitials[index]
            const statusColorClassName = instance.connected ? "bg-green-500" : "bg-red-500"

            return (
              <IconOnlyLabel
                key={instance.id}
                show={iconOnly}
                label={(
                  <>
                    {instance.name}
                    {!instance.connected && <span className="block opacity-70">{t("header.disconnected")}</span>}
                    {hasRss && <span className="block opacity-70">{csState?.rssRunning ? t("sidebar.rssRunning") : t("sidebar.rssEnabled")}</span>}
                    {hasSearch && <span className="block opacity-70">{t("sidebar.scanRunning")}</span>}
                  </>
                )}
              >
                <Link
                  to="/instances/$instanceId"
                  params={{ instanceId: instance.id.toString() }}
                  className={cn(rowClassName, iconOnly && iconOnlyRowClassName, rowStateClassName(isActive))}
                >
                  <span className="relative h-4 w-4 flex-shrink-0">
                    <HardDrive className={cn("h-4 w-4", morphClassName(!iconOnly || !initials))} />
                    <span className={cn("absolute inset-0 flex items-center justify-center text-[11px] font-semibold", morphClassName(iconOnly && Boolean(initials)))} aria-hidden="true">{initials}</span>
                  </span>
                  <span className={rowLabelClassName("min-w-44")}>
                    <span className="truncate max-w-36" title={instance.name}>{instance.name}</span>
                    <span className="ml-auto flex items-center gap-1.5">
                      {hasRss && (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span className="flex items-center">
                              {csState?.rssRunning ? (
                                <Loader2 className={cn(
                                  "h-3 w-3 animate-spin",
                                  isActive ? "text-sidebar-primary-foreground/70" : "text-sidebar-foreground/70"
                                )} />
                              ) : (
                                <Rss className={cn(
                                  "h-3 w-3",
                                  isActive ? "text-sidebar-primary-foreground/70" : "text-sidebar-foreground/70"
                                )} />
                              )}
                            </span>
                          </TooltipTrigger>
                          <TooltipContent side="right" className="text-xs">
                            {csState?.rssRunning ? t("sidebar.rssRunning") : t("sidebar.rssEnabled")}
                          </TooltipContent>
                        </Tooltip>
                      )}
                      {hasSearch && (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span className="flex items-center">
                              <SearchCode className={cn(
                                "h-3 w-3",
                                isActive ? "text-sidebar-primary-foreground/70" : "text-sidebar-foreground/70"
                              )} />
                            </span>
                          </TooltipTrigger>
                          <TooltipContent side="right" className="text-xs">
                            {t("sidebar.scanRunning")}
                          </TooltipContent>
                        </Tooltip>
                      )}
                      <span className={cn("h-2 w-2 flex-shrink-0 rounded-full", statusColorClassName)} />
                    </span>
                  </span>
                  <span className={cn(cornerDotClassName, statusColorClassName)} />
                </Link>
              </IconOnlyLabel>
            )
          })}
          {activeInstances.length === 0 && !iconOnly && (
            <p className="px-3 py-2 text-sm text-sidebar-foreground/50">
              {hasConfiguredInstances ? t("sidebar.allInstancesDisabled") : t("sidebar.noInstancesConfigured")}
            </p>
          )}
        </div>

        <div className="mt-auto space-y-3 pt-2 empty:hidden">
          <SystemNoticeList iconOnly={iconOnly} />
          <UpdateBanner iconOnly={iconOnly} />
        </div>
      </nav>

      <div className="flex-shrink-0 px-3 pt-1.5 pb-2">
        <Separator className="mb-1.5" />

        {/* The row is the same in both modes, so nothing above it moves. */}
        <div className={cn("relative flex items-center justify-between pl-3", iconOnly && "w-58")}>
          <div className={cn("flex flex-col gap-1 text-[10px] text-sidebar-foreground/40 select-none whitespace-nowrap", swapClassName(!iconOnly))}>
            <span className="font-medium text-sidebar-foreground/50">{t("sidebar.version", { version: appVersion })}</span>
            <div className="flex items-center gap-1">
              <Copyright className="h-2.5 w-2.5" />
              <span>{new Date().getFullYear()} autobrr</span>
            </div>
          </div>
          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger asChild>
                <DropdownMenuTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon"
                    inert={!iconOnly}
                    className={cn(footerButtonClassName, "absolute left-2", swapClassName(iconOnly))}
                    aria-label={t("kit.moreActions")}
                  >
                    <Ellipsis className="h-3.5 w-3.5" />
                  </Button>
                </DropdownMenuTrigger>
              </TooltipTrigger>
              <TooltipContent side="right" className="text-xs">
                {t("kit.moreActions")}
              </TooltipContent>
            </Tooltip>
            <DropdownMenuContent align="end" side="right">
              <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">
                {t("sidebar.version", { version: appVersion })}
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuSub>
                <DropdownMenuSubTrigger className="gap-2">
                  <Globe className="h-4 w-4 text-muted-foreground" />
                  {t("sidebar.language")}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent>{languageItems}</DropdownMenuSubContent>
              </DropdownMenuSub>
              <DropdownMenuItem asChild>
                <a href="https://github.com/autobrr/qui" target="_blank" rel="noopener noreferrer">
                  <Code className="h-4 w-4" />
                  {t("sidebar.viewOnGitHub")}
                </a>
              </DropdownMenuItem>
              {!isDemo && (
                <DropdownMenuItem onClick={() => logout()}>
                  <LogOut className="h-4 w-4" />
                  {t("sidebar.logout")}
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
          <div inert={iconOnly} className={cn("flex items-center gap-0.5", labelClassName)}>
            <DropdownMenu>
              <Tooltip>
                <TooltipTrigger asChild>
                  <DropdownMenuTrigger asChild>
                    <Button
                      variant="ghost"
                      size="icon"
                      className={footerButtonClassName}
                      aria-label={t("sidebar.language")}
                    >
                      <Globe className="h-3.5 w-3.5" />
                    </Button>
                  </DropdownMenuTrigger>
                </TooltipTrigger>
                <TooltipContent side="top" className="text-xs">
                  {t("sidebar.language")}
                </TooltipContent>
              </Tooltip>
              <DropdownMenuContent align="end" side="top">
                {languageItems}
              </DropdownMenuContent>
            </DropdownMenu>
            <Button
              variant="ghost"
              size="icon"
              className={footerButtonClassName}
              asChild
            >
              <a
                href="https://github.com/autobrr/qui"
                target="_blank"
                rel="noopener noreferrer"
                aria-label={t("sidebar.viewOnGitHub")}
              >
                <Code className="h-3.5 w-3.5" />
              </a>
            </Button>
            {!isDemo && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon"
                    className={footerButtonClassName}
                    onClick={() => logout()}
                    aria-label={t("sidebar.logout")}
                  >
                    <LogOut className="h-3.5 w-3.5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="top" className="text-xs">
                  {t("sidebar.logout")}
                </TooltipContent>
              </Tooltip>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
