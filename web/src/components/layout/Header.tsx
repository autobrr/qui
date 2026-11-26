/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { InstanceMarkIcon } from "@/components/instances/marks/InstanceMarkIcon"
import { InstancePreferencesDialog } from "@/components/instances/preferences/InstancePreferencesDialog"
import { HeaderInstanceTitle } from "@/components/layout/HeaderInstanceTitle"
import { SpreadsheetRibbonTabs } from "@/components/spreadsheet/SpreadsheetRibbonTabs"
import { AddTorrentDialog } from "@/components/torrents/AddTorrentDialog"
import { TorrentCreationTasks } from "@/components/torrents/TorrentCreationTasks"
import { TorrentCreatorDialog } from "@/components/torrents/TorrentCreatorDialog"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { TorrentManagementBar } from "@/components/torrents/TorrentManagementBar"
import { themeArea } from "@/lib/theme-areas"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { SupportButton } from "@/components/support/SupportButton"
import { ThemeToggle } from "@/components/ui/ThemeToggle"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"
import { useLayoutRoute } from "@/contexts/LayoutRouteContext"
import { useTorrentSelectionContext, useTorrentSelectionSummary } from "@/contexts/TorrentSelectionContext"
import { useActiveTaskCount } from "@/hooks/useActiveTaskCount"
import { useDebounce } from "@/hooks/useDebounce"
import { instanceCapabilitiesQuery } from "@/hooks/useInstanceCapabilities"
import { useInstances } from "@/hooks/useInstances"
import { useIsMobile } from "@/hooks/useMediaQuery"
import { usePersistedCompactViewState } from "@/hooks/usePersistedCompactViewState"
import { usePersistedFilterSidebarState } from "@/hooks/usePersistedFilterSidebarState"
import { usePersistedUnifiedInstanceFilter } from "@/hooks/usePersistedUnifiedInstanceFilter"
import { api } from "@/lib/api"
import {
  ALL_INSTANCES_ID,
  isAllInstancesScope,
  normalizeUnifiedInstanceIds
} from "@/lib/instances"
import { modShortcut } from "@/lib/keyboard"
import { useSpreadsheetDisguise } from "@/lib/spreadsheet-disguise"
import { hasSelection } from "@/lib/torrent-selection"
import { cn } from "@/lib/utils"
import type { InstanceCapabilities } from "@/types"
import { useQueries, useQuery } from "@tanstack/react-query"
import { useNavigate, useSearch } from "@tanstack/react-router"
import { navigateWithSearch } from "@/lib/router-search"
import { Cog, FileEdit, FunnelPlus, FunnelX, Info, ListTodo, PanelLeftOpen, Plus, Search, X } from "lucide-react"
import { useSystemNotices } from "@/hooks/useSystemNotices"
import { useUpdateBannerDismissed } from "@/hooks/useUpdateBannerDismissed"
import { systemNoticeDotColor, topSystemNoticeSeverity } from "@/lib/system-notice"
import { type ReactNode, type Ref, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useHotkeys } from "react-hotkeys-hook"
import { useTranslation } from "react-i18next"

interface HeaderProps {
  sidebarIconOnly?: boolean
  sidebarHidden?: boolean
  sidebarOpen?: boolean
  openSidebarButtonRef?: Ref<HTMLButtonElement>
  onOpenSidebar?: () => void
}

function OpenSidebarButton({ open, buttonRef, onClick }: { open: boolean; buttonRef?: Ref<HTMLButtonElement>; onClick?: () => void }) {
  const { t } = useTranslation("common")
  const systemNotices = useSystemNotices()
  const { data: updateInfo } = useQuery({ queryKey: ["latest-version"], queryFn: () => api.getLatestVersion(), enabled: false })
  const [updateDismissed] = useUpdateBannerDismissed()
  const showUpdate = Boolean(updateInfo) && !updateDismissed
  const extras = [
    systemNotices.length > 0 && t("systemNotice.needsAttention"),
    showUpdate && t("updateBanner.updateAvailable"),
  ].filter(Boolean)
  return (
    <Tooltip>
      <TooltipTrigger asChild onFocus={(event) => { if (!event.currentTarget.matches(":focus-visible")) event.preventDefault() }}>
        <Button
          ref={buttonRef}
          variant="ghost"
          size="icon"
          className="group relative hidden text-muted-foreground transition-colors hover:bg-muted hover:text-foreground lg:inline-flex"
          onClick={onClick}
          aria-label={[t("sidebar.showSidebar"), ...extras].join(", ")}
          aria-expanded={open}
          aria-haspopup="dialog"
        >
          <PanelLeftOpen className="h-4 w-4" />
          {systemNotices.length > 0 && (
            <span aria-hidden="true" className={cn("absolute top-1.5 left-1.5 h-1.5 w-1.5 rounded-full ring-2 ring-background transition-colors group-hover:ring-muted", systemNoticeDotColor[topSystemNoticeSeverity(systemNotices)].fill)} />
          )}
          {showUpdate && <span aria-hidden="true" className="absolute top-1.5 right-1.5 h-1.5 w-1.5 rounded-full bg-green-500 ring-2 ring-background transition-colors group-hover:ring-muted" />}
        </Button>
      </TooltipTrigger>
      <TooltipContent side="bottom" className="text-xs">
        {t("sidebar.showSidebar")}
        {extras.map((extra) => <span key={String(extra)} className="block opacity-70">{extra}</span>)}
      </TooltipContent>
    </Tooltip>
  )
}

interface UnifiedActionDropdownProps {
  icon: ReactNode
  tooltip: string
  label: string
  instances: Array<{ id: number; name: string; connected: boolean }>
  onSelectInstance: (id: number) => void
}

function UnifiedActionDropdown({ icon, tooltip, label, instances, onSelectInstance }: UnifiedActionDropdownProps) {
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button
              variant="outline"
              size="icon"
              className="hidden md:inline-flex"
              aria-label={tooltip}
            >
              {icon}
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent>{tooltip}</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="start">
        <DropdownMenuLabel className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">
          {label}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {instances.map((instance) => (
          <DropdownMenuItem
            key={instance.id}
            onSelect={() => onSelectInstance(instance.id)}
            className="cursor-pointer"
          >
            <InstanceMarkIcon instanceId={instance.id} problem={!instance.connected} className="mr-2" />
            <span className="flex-1 truncate">{instance.name}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function Header({ sidebarIconOnly = false, sidebarHidden = false, sidebarOpen = false, openSidebarButtonRef, onOpenSidebar }: HeaderProps) {
  const showTitleOnDesktop = sidebarIconOnly || sidebarHidden
  const { t } = useTranslation("common")
  const navigate = useNavigate()
  const routeSearch = useSearch({ strict: false }) as { q?: string; modal?: string;[key: string]: unknown }
  const { state: layoutRouteState } = useLayoutRoute()
  const spreadsheetDisguise = useSpreadsheetDisguise()

  const { selection } = useTorrentSelectionContext()
  const selectionSummary = useTorrentSelectionSummary()
  const isMobile = useIsMobile()

  const selectedInstanceId = layoutRouteState.instanceId
  const isInstanceRoute = selectedInstanceId !== null
  const isAllInstancesRoute = isAllInstancesScope(selectedInstanceId ?? -1)
  const shouldShowInstanceControls = layoutRouteState.showInstanceControls && isInstanceRoute
  const canManageSelectedInstance = shouldShowInstanceControls && selectedInstanceId !== null && selectedInstanceId > 0

  const shouldShowQuiOnMobile = !isInstanceRoute
  const [searchValue, setSearchValue] = useState<string>(routeSearch?.q || "")
  const debouncedSearch = useDebounce(searchValue, 500)
  const { instances } = useInstances()
  const activeInstances = useMemo(
    () => (instances ?? []).filter(instance => instance.isActive),
    [instances]
  )
  const activeInstanceIds = useMemo(
    () => activeInstances.map(instance => instance.id),
    [activeInstances]
  )
  const [persistedUnifiedFilter, saveUnifiedFilter] = usePersistedUnifiedInstanceFilter()
  const normalizedUnifiedInstanceIds = useMemo(
    () => normalizeUnifiedInstanceIds(persistedUnifiedFilter, activeInstanceIds),
    [persistedUnifiedFilter, activeInstanceIds]
  )
  const effectiveUnifiedInstanceIds = normalizedUnifiedInstanceIds.length > 0? normalizedUnifiedInstanceIds: activeInstanceIds
  const unifiedScopeInstances = useMemo(
    () => activeInstances.filter(instance => effectiveUnifiedInstanceIds.includes(instance.id)),
    [activeInstances, effectiveUnifiedInstanceIds]
  )
  const unifiedManageableInstances = useMemo(
    () => unifiedScopeInstances.filter((instance) => instance.id > 0),
    [unifiedScopeInstances]
  )
  const unifiedCapabilitiesResults = useQueries({
    queries: unifiedManageableInstances.map((instance) => ({
      ...instanceCapabilitiesQuery(instance.id),
      enabled: isAllInstancesRoute,
    })),
  })
  const unifiedTorrentCreationInstances = useMemo(
    () => unifiedManageableInstances.filter((_instance, i) =>
      unifiedCapabilitiesResults[i]?.data?.supportsTorrentCreation === true
    ),
    [unifiedManageableInstances, unifiedCapabilitiesResults]
  )
  const applyUnifiedScope = useCallback((nextIds: number[]) => {
    const normalizedIds = normalizeUnifiedInstanceIds(nextIds, activeInstanceIds)
    saveUnifiedFilter(normalizedIds)
    const nextSearch: Record<string, unknown> = isAllInstancesRoute ? { ...(routeSearch || {}) } : {}

    navigateWithSearch({
      navigate,
      to: "/instances",
      search: nextSearch,
      replace: isAllInstancesRoute,
    })
  }, [activeInstanceIds, isAllInstancesRoute, navigate, routeSearch, saveUnifiedFilter])
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
  }, [applyUnifiedScope, activeInstanceIds])


  const currentInstance = useMemo(() => {
    if (!isInstanceRoute || selectedInstanceId === null || selectedInstanceId <= 0) return undefined
    return instances?.find(i => i.id === selectedInstanceId)
  }, [isInstanceRoute, instances, selectedInstanceId])
  const hasMultipleActiveInstances = activeInstances.length > 1
  const instanceName = isAllInstancesRoute? (hasMultipleActiveInstances ? t("header.unified") : (activeInstances[0]?.name ?? null)): (currentInstance?.name ?? null)

  useEffect(() => {
    const routeQuery = routeSearch?.q || ""
    setSearchValue(prev => (prev.trim() === routeQuery ? prev : routeQuery))
  }, [selectedInstanceId, routeSearch?.q])

  useEffect(() => {
    if (!shouldShowInstanceControls) return
    const trimmedSearch = debouncedSearch.trim()
    const next = { ...(routeSearch || {}) }
    if (trimmedSearch) next.q = trimmedSearch
    else delete next.q
    navigateWithSearch({ navigate, search: next, replace: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedSearch])

  const isGlobSearch = !!searchValue && /[*?]/.test(searchValue)
  const [filterSidebarCollapsed, setFilterSidebarCollapsed] = usePersistedFilterSidebarState(false)
  const searchInputRef = useRef<HTMLInputElement>(null)
  const lastFilterToggleRef = useRef(0)

  const handleToggleFilters = useCallback(() => {
    const now = Date.now()
    if (now - lastFilterToggleRef.current < 250) {
      return
    }

    lastFilterToggleRef.current = now
    setFilterSidebarCollapsed((prev) => !prev)
  }, [setFilterSidebarCollapsed])

  useHotkeys(
    ["/", "shift+/"],
    () => searchInputRef.current?.focus(),
    {
      useKey: true,
      preventDefault: true,
      enabled: shouldShowInstanceControls,
      ignoreEventWhen: event => !searchInputRef.current?.offsetParent || (event.target instanceof Element && event.target.closest("[role=\"dialog\"]") !== null),
    },
    [shouldShowInstanceControls]
  )
  const { viewMode } = usePersistedCompactViewState("desktop")
  const activeTaskCount = useActiveTaskCount(selectedInstanceId, canManageSelectedInstance && shouldShowInstanceControls)

  const { data: instanceCapabilities } = useQuery<InstanceCapabilities>({
    queryKey: ["instance-capabilities", selectedInstanceId],
    queryFn: () => api.getInstanceCapabilities(selectedInstanceId!),
    enabled: canManageSelectedInstance,
    staleTime: 300000,
  })

  const supportsTorrentCreation = canManageSelectedInstance ? (instanceCapabilities?.supportsTorrentCreation ?? true) : false

  const [instanceSettingsOpen, setInstanceSettingsOpen] = useState(false)
  const [unifiedAddTorrentInstanceId, setUnifiedAddTorrentInstanceId] = useState<number | null>(null)
  const [unifiedCreateTorrentInstanceId, setUnifiedCreateTorrentInstanceId] = useState<number | null>(null)
  const [unifiedTasksInstanceId, setUnifiedTasksInstanceId] = useState<number | null>(null)
  const [unifiedSettingsInstanceId, setUnifiedSettingsInstanceId] = useState<number | null>(null)

  const validUnifiedIds = useMemo(
    () => new Set(unifiedManageableInstances.map((instance) => instance.id)),
    [unifiedManageableInstances]
  )
  const validUnifiedTorrentCreationIds = useMemo(
    () => new Set(unifiedTorrentCreationInstances.map((instance) => instance.id)),
    [unifiedTorrentCreationInstances]
  )

  useEffect(() => {
    setInstanceSettingsOpen(false)
  }, [selectedInstanceId])

  const headerHeight = viewMode === "dense" ? "lg:h-12" : "lg:h-16"
  const innerHeight = viewMode === "dense" ? "h-10 lg:h-auto" : "h-12 lg:h-auto"
  const smInnerHeight = viewMode === "dense" ? "sm:h-10 lg:h-auto" : "sm:h-12 lg:h-auto"

  const headerElement = (
    <header {...themeArea("app-header")} className={cn("sticky top-0 z-50 hidden md:flex flex-wrap lg:flex-nowrap items-start lg:items-center justify-between sm:border-b bg-background pl-2 pr-4 md:pl-4 md:pr-4 lg:pl-2 lg:static py-2 lg:py-0", headerHeight, !isInstanceRoute && "sideways:hidden")}>
      <div className={cn("hidden md:flex items-center gap-2 mr-2 order-1 lg:order-none", innerHeight)}>
        {sidebarHidden && <OpenSidebarButton open={sidebarOpen} buttonRef={openSidebarButtonRef} onClick={onOpenSidebar} />}
        <HeaderInstanceTitle
          className={cn(
            "lg:hidden",
            showTitleOnDesktop && "lg:flex",
            !shouldShowQuiOnMobile && "hidden sm:flex"
          )}
          instanceName={instanceName}
          activeInstances={activeInstances}
          selectedInstanceId={selectedInstanceId}
          isAllInstancesRoute={isAllInstancesRoute}
          effectiveUnifiedInstanceIds={effectiveUnifiedInstanceIds}
          onResetUnifiedScope={resetUnifiedScope}
          onToggleUnifiedScopeInstance={toggleUnifiedScopeInstance}
        />
      </div>

      {shouldShowInstanceControls && (
        <>
          <div className={cn(
            "hidden md:flex items-center gap-2 order-2 lg:order-none",
            innerHeight,
            showTitleOnDesktop && "lg:ml-2"
          )}>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="icon"
                  className="hidden md:inline-flex"
                  onClick={handleToggleFilters}
                >
                  {filterSidebarCollapsed ? (
                    <FunnelPlus className="h-4 w-4" />
                  ) : (
                    <FunnelX className="h-4 w-4" />
                  )}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{filterSidebarCollapsed ? t("header.showFilters") : t("header.hideFilters")}</TooltipContent>
            </Tooltip>
            {isAllInstancesRoute && unifiedManageableInstances.length > 0 && (
              <>
                <UnifiedActionDropdown
                  icon={<Plus className="h-4 w-4" />}
                  tooltip={t("header.addTorrent")}
                  label={t("header.addToInstance")}
                  instances={unifiedManageableInstances}
                  onSelectInstance={setUnifiedAddTorrentInstanceId}
                />
                {unifiedTorrentCreationInstances.length > 0 && (
                  <>
                    <UnifiedActionDropdown
                      icon={<FileEdit className="h-4 w-4" />}
                      tooltip={t("header.createTorrent")}
                      label={t("header.createForInstance")}
                      instances={unifiedTorrentCreationInstances}
                      onSelectInstance={setUnifiedCreateTorrentInstanceId}
                    />
                    <UnifiedActionDropdown
                      icon={<ListTodo className="h-4 w-4" />}
                      tooltip={t("header.torrentCreationTasks")}
                      label={t("header.tasksForInstance")}
                      instances={unifiedTorrentCreationInstances}
                      onSelectInstance={setUnifiedTasksInstanceId}
                    />
                  </>
                )}
                <UnifiedActionDropdown
                  icon={<Cog className="h-4 w-4" />}
                  tooltip={t("header.instanceSettings")}
                  label={t("header.settingsForInstance")}
                  instances={unifiedManageableInstances}
                  onSelectInstance={setUnifiedSettingsInstanceId}
                />
              </>
            )}
            {canManageSelectedInstance && (
              <>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      variant="outline"
                      size="icon"
                      className="hidden md:inline-flex"
                      onClick={() => {
                        const next = { ...(routeSearch || {}), modal: "add-torrent" }
                        navigateWithSearch({ navigate, search: next, replace: true })
                      }}
                    >
                      <Plus className="h-4 w-4" />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>{t("header.addTorrent")}</TooltipContent>
                </Tooltip>
                {supportsTorrentCreation && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant="outline"
                        size="icon"
                        className="hidden md:inline-flex"
                        onClick={() => {
                          const next = { ...(routeSearch || {}), modal: "create-torrent" }
                          navigateWithSearch({ navigate, search: next, replace: true })
                        }}
                      >
                        <FileEdit className="h-4 w-4" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>{t("header.createTorrent")}</TooltipContent>
                  </Tooltip>
                )}
                {isInstanceRoute && supportsTorrentCreation && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant="outline"
                        size="icon"
                        className="hidden md:inline-flex relative"
                        onClick={() => {
                          const next = { ...(routeSearch || {}), modal: "tasks" }
                          navigateWithSearch({ navigate, search: next, replace: true })
                        }}
                      >
                        <ListTodo className="h-4 w-4" />
                        {activeTaskCount > 0 && (
                          <Badge variant="default" className="absolute -top-1 -right-1 h-5 min-w-5 flex items-center justify-center p-0">
                            {activeTaskCount}
                          </Badge>
                        )}
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>{t("header.torrentCreationTasks")}</TooltipContent>
                  </Tooltip>
                )}
                {isInstanceRoute && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant="outline"
                        size="icon"
                        className="hidden md:inline-flex"
                        onClick={() => setInstanceSettingsOpen(true)}
                        aria-label={t("header.instanceSettings")}
                      >
                        <Cog className="h-4 w-4" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>{t("header.instanceSettings")}</TooltipContent>
                  </Tooltip>
                )}
              </>
            )}
          </div>
          {!isMobile && selectionSummary && hasSelection(selection) && (
            <div className="sm:w-full sm:basis-full lg:basis-auto lg:w-auto sm:order-5 lg:order-none flex justify-center lg:justify-start lg:ml-2 animate-in fade-in duration-400 ease-out motion-reduce:animate-none motion-reduce:duration-0">
              <TorrentManagementBar
                instanceId={selectedInstanceId ?? undefined}
                actionTarget={selectionSummary.actionTarget}
              />
            </div>
          )}
        </>
      )}
      {shouldShowInstanceControls && (
        <div className={cn("flex items-center flex-1 gap-2 sm:order-3 lg:order-none", smInnerHeight)}>

          <div className="flex items-center gap-2 flex-1 justify-end mr-2">
            <div className="relative w-full md:w-62 md:focus-within:w-full max-w-md transition-[width] duration-100 ease-out will-change-[width] hidden md:block">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none transition-opacity duration-300" />
              <Input
                ref={searchInputRef}
                placeholder={isGlobSearch ? t("header.globPattern") : t("header.searchTorrents")}
                value={searchValue}
                onChange={(e) => setSearchValue(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    const next = { ...(routeSearch || {}) }
                    const trimmedValue = searchValue.trim()
                    if (trimmedValue) next.q = trimmedValue
                    else delete next.q
                    navigateWithSearch({ navigate, search: next, replace: true })
                  } else if (e.key === "Escape") {
                    e.preventDefault()
                    e.stopPropagation()
                    if (searchValue) {
                      setSearchValue("")
                    }
                    setTimeout(() => {
                      searchInputRef.current?.blur()
                    }, 100)
                  }
                }}
                className={`w-full pl-9 ${searchValue ? "pr-16" : "pr-9"} transition-[box-shadow,border-color] duration-200 text-xs ${searchValue ? "ring-1 ring-primary/50" : ""
                } ${isGlobSearch ? "ring-1 ring-primary" : ""}`}
              />
              <div className="absolute right-2 top-1/2 -translate-y-1/2 flex items-center gap-1">
                {searchValue && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        type="button"
                        className="p-1 hover:bg-muted rounded-sm transition-colors hidden sm:block"
                        onClick={() => {
                          setSearchValue("")
                          const next = { ...(routeSearch || {}) }
                          delete next.q
                          navigateWithSearch({ navigate, search: next, replace: true })
                        }}
                      >
                        <X className="h-3.5 w-3.5 text-muted-foreground" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent>{t("header.clearSearch")}</TooltipContent>
                  </Tooltip>
                )}
                <Tooltip>
                  <TooltipTrigger asChild openOnTap>
                    <button
                      type="button"
                      className="p-1 hover:bg-muted rounded-sm transition-colors hidden sm:block"
                      onClick={(e) => e.preventDefault()}
                    >
                      <Info className="h-3.5 w-3.5 text-muted-foreground" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent className="max-w-xs">
                    <div className="space-y-2 text-xs">
                      <p className="font-semibold">{t("header.smartSearchTitle")}</p>
                      <ul className="space-y-1 ml-2">
                        <li>• <strong>{t("header.smartSearchGlob")}</strong> {t("header.smartSearchGlobExamples")}</li>
                        <li>• <strong>{t("header.smartSearchFuzzy")}</strong> {t("header.smartSearchFuzzyExample")}</li>
                        <li>• {t("header.smartSearchDots")}</li>
                        <li>• {t("header.smartSearchFields")}</li>
                        <li>• {t("header.smartSearchEnter")}</li>
                        <li>• {t("header.smartSearchAuto")}</li>
                        <li>• {t("header.smartSearchShortcut", { shortcutKey: "/" })}</li>
                        <li>• {t("header.smartSearchPalette", { shortcutKey: modShortcut("K") })}</li>
                      </ul>
                    </div>
                  </TooltipContent>
                </Tooltip>
              </div>
            </div>
            <span id="header-search-actions" className="flex items-center gap-1" />
          </div>
        </div>
      )}


      <div className={cn("grid grid-cols-[auto_auto] items-center gap-1 transition-all duration-300 ease-out sm:order-4 lg:order-none", smInnerHeight)}>
        <SupportButton />
        <ThemeToggle />
      </div>

      {selectedInstanceId !== null && selectedInstanceId > ALL_INSTANCES_ID && instanceName && (
        <InstancePreferencesDialog
          open={instanceSettingsOpen}
          onOpenChange={setInstanceSettingsOpen}
          instanceId={selectedInstanceId}
          instanceName={instanceName}
          instance={currentInstance}
        />
      )}

      {unifiedAddTorrentInstanceId !== null && validUnifiedIds.has(unifiedAddTorrentInstanceId) && (
        <AddTorrentDialog
          instanceId={unifiedAddTorrentInstanceId}
          open={true}
          onOpenChange={(open) => {
            if (!open) setUnifiedAddTorrentInstanceId(null)
          }}
        />
      )}

      {unifiedCreateTorrentInstanceId !== null && validUnifiedTorrentCreationIds.has(unifiedCreateTorrentInstanceId) && (
        <TorrentCreatorDialog
          instanceId={unifiedCreateTorrentInstanceId}
          open={true}
          onOpenChange={(open) => {
            if (!open) setUnifiedCreateTorrentInstanceId(null)
          }}
        />
      )}

      {unifiedTasksInstanceId !== null && validUnifiedTorrentCreationIds.has(unifiedTasksInstanceId) && (() => {
        const inst = activeInstances.find(i => i.id === unifiedTasksInstanceId)
        if (!inst) return null
        return (
          <Dialog open={true} onOpenChange={(open) => { if (!open) setUnifiedTasksInstanceId(null) }}>
            <DialogContent className="w-full sm:max-w-screen-sm md:max-w-screen-md lg:max-w-screen-xl xl:max-w-screen-xl max-h-[85vh] overflow-hidden flex flex-col">
              <DialogHeader>
                <DialogTitle>{t("header.torrentCreationTasksTitle")}{inst ? ` — ${inst.name}` : ""}</DialogTitle>
              </DialogHeader>
              <div className="flex-1 overflow-auto">
                <TorrentCreationTasks instanceId={unifiedTasksInstanceId} />
              </div>
            </DialogContent>
          </Dialog>
        )
      })()}

      {unifiedSettingsInstanceId !== null && validUnifiedIds.has(unifiedSettingsInstanceId) && (() => {
        const inst = activeInstances.find(i => i.id === unifiedSettingsInstanceId)
        if (!inst) return null
        return (
          <InstancePreferencesDialog
            open={true}
            onOpenChange={(open) => { if (!open) setUnifiedSettingsInstanceId(null) }}
            instanceId={unifiedSettingsInstanceId}
            instanceName={inst.name}
            instance={inst}
          />
        )
      })()}

    </header>
  )

  return (
    <>
      {spreadsheetDisguise && <SpreadsheetRibbonTabs />}
      {headerElement}
    </>
  )
}
