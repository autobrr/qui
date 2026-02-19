/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { InstancePreferencesDialog } from "@/components/instances/preferences/InstancePreferencesDialog"
import { TorrentManagementBar } from "@/components/torrents/TorrentManagementBar"
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
import { Logo } from "@/components/ui/Logo"
import { NapsterLogo } from "@/components/ui/NapsterLogo"
import { SwizzinLogo } from "@/components/ui/SwizzinLogo"
import { ThemeToggle } from "@/components/ui/ThemeToggle"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"
import { useLayoutRoute } from "@/contexts/LayoutRouteContext"
import { useTorrentSelection } from "@/contexts/TorrentSelectionContext"
import { useAuth } from "@/hooks/useAuth"
import { useCrossSeedInstanceState } from "@/hooks/useCrossSeedInstanceState"
import { useDebounce } from "@/hooks/useDebounce"
import { useInstances } from "@/hooks/useInstances"
import { usePersistedCompactViewState } from "@/hooks/usePersistedCompactViewState"
import { usePersistedFilterSidebarState } from "@/hooks/usePersistedFilterSidebarState"
import { useTheme } from "@/hooks/useTheme"
import { api } from "@/lib/api"
import { ALL_INSTANCES_ID, isAllInstancesScope } from "@/lib/instances"
import { cn } from "@/lib/utils"
import type { InstanceCapabilities } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate, useSearch } from "@tanstack/react-router"
import { Archive, ChevronsUpDown, Cog, Download, FileEdit, FileText, FunnelPlus, FunnelX, GitBranch, HardDrive, Home, Info, ListTodo, Loader2, LogOut, Menu, Plus, Rss, Search, SearchCode, Server, Settings, X, Zap } from "lucide-react"
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useHotkeys } from "react-hotkeys-hook"

interface HeaderProps {
  children?: ReactNode
  sidebarCollapsed?: boolean
}

export function Header({
  children,
  sidebarCollapsed = false,
}: HeaderProps) {
  const { logout } = useAuth()
  const navigate = useNavigate()
  const routeSearch = useSearch({ strict: false }) as { q?: string; modal?: string;[key: string]: unknown }
  const { state: layoutRouteState } = useLayoutRoute()

  // Get selection state from context
  const {
    selectedHashes,
    selectedTorrents,
    isAllSelected,
    totalSelectionCount,
    selectedTotalSize,
    excludeHashes,
    filters,
    clearSelection,
  } = useTorrentSelection()

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


  const currentInstance = useMemo(() => {
    if (!isInstanceRoute || selectedInstanceId === null || selectedInstanceId <= 0) return undefined
    return instances?.find(i => i.id === selectedInstanceId)
  }, [isInstanceRoute, instances, selectedInstanceId])
  const instanceName = isAllInstancesRoute ? "Unified" : (currentInstance?.name ?? null)
  const hasMultipleActiveInstances = activeInstances.length > 1

  // Keep local state in sync with URL when navigating between instances/routes
  useEffect(() => {
    setSearchValue(routeSearch?.q || "")
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedInstanceId])

  // Update URL search param after debounce
  useEffect(() => {
    if (!shouldShowInstanceControls) return
    const trimmedSearch = debouncedSearch.trim()
    const next = { ...(routeSearch || {}) }
    if (trimmedSearch) next.q = trimmedSearch
    else delete next.q
    navigate({ search: next as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedSearch, shouldShowInstanceControls])

  const isGlobSearch = !!searchValue && /[*?[\]]/.test(searchValue)
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

  // Detect platform for appropriate key display
  const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad|iPod/.test(navigator.userAgent)
  const shortcutKey = isMac ? "⌘K" : "Ctrl+K"

  // Global keyboard shortcut to focus search
  useHotkeys(
    "meta+k, ctrl+k",
    (event) => {
      event.preventDefault()
      searchInputRef.current?.focus()
    },
    {
      preventDefault: true,
      enableOnFormTags: ["input", "textarea", "select"],
      enabled: shouldShowInstanceControls,
    },
    [shouldShowInstanceControls]
  )
  const { theme } = useTheme()
  const { viewMode } = usePersistedCompactViewState("normal")

  // Query active task count for badge (lightweight endpoint, only for instance routes)
  const { data: activeTaskCount = 0 } = useQuery({
    queryKey: ["active-task-count", selectedInstanceId],
    queryFn: () => canManageSelectedInstance && selectedInstanceId !== null ? api.getActiveTaskCount(selectedInstanceId) : Promise.resolve(0),
    enabled: canManageSelectedInstance,
    refetchInterval: 30000, // Poll every 30 seconds (lightweight check)
    refetchIntervalInBackground: true,
  })

  // Query for available updates
  const { data: updateInfo } = useQuery({
    queryKey: ["latest-version"],
    queryFn: () => api.getLatestVersion(),
    refetchInterval: 2 * 60 * 1000,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })

  // Query instance capabilities via the dedicated lightweight endpoint
  const { data: instanceCapabilities } = useQuery<InstanceCapabilities>({
    queryKey: ["instance-capabilities", selectedInstanceId],
    queryFn: () => api.getInstanceCapabilities(selectedInstanceId!),
    enabled: canManageSelectedInstance,
    staleTime: 300000, // Cache for 5 minutes (capabilities don't change often)
  })

  const supportsTorrentCreation = canManageSelectedInstance ? (instanceCapabilities?.supportsTorrentCreation ?? true) : false

  // Instance settings dialog state
  const [instanceSettingsOpen, setInstanceSettingsOpen] = useState(false)

  useEffect(() => {
    setInstanceSettingsOpen(false)
  }, [selectedInstanceId])

  const { state: crossSeedInstanceState } = useCrossSeedInstanceState()

  // Dense mode uses reduced header height
  const headerHeight = viewMode === "dense" ? "lg:h-12" : "lg:h-16"
  const innerHeight = viewMode === "dense" ? "h-10 lg:h-auto" : "h-12 lg:h-auto"
  const smInnerHeight = viewMode === "dense" ? "sm:h-10 lg:h-auto" : "sm:h-12 lg:h-auto"

  return (
    <header className={cn("sticky top-0 z-50 hidden md:flex flex-wrap lg:flex-nowrap items-start lg:items-center justify-between sm:border-b bg-background pl-2 pr-4 md:pl-4 md:pr-4 lg:pl-0 lg:static py-2 lg:py-0", headerHeight)}>
      <div className={cn("hidden md:flex items-center gap-2 mr-2 order-1 lg:order-none", innerHeight)}>
        {children}
        {instanceName && (hasMultipleActiveInstances || isAllInstancesRoute) ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                className={cn(
                  "group flex items-center gap-2 pl-2 sm:pl-0 text-xl font-semibold transition-all duration-300 hover:opacity-90 rounded-sm px-1 -mx-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
                  "lg:hidden", // Hidden on desktop by default
                  sidebarCollapsed && "lg:flex", // Visible on desktop when sidebar collapsed
                  !shouldShowQuiOnMobile && "hidden sm:flex" // Hide on mobile when on instance routes
                )}
                aria-label={`Current instance: ${instanceName}. Click to switch instances.`}
                aria-haspopup="menu"
              >
                {theme === "swizzin" ? (
                  <SwizzinLogo className="h-5 w-5" />
                ) : theme === "napster" ? (
                  <NapsterLogo className="h-5 w-5" />
                ) : (
                  <Logo className="h-5 w-5" />
                )}
                <span className="flex items-center max-w-32">
                  <span className="truncate">{instanceName}</span>
                  <ChevronsUpDown className="h-3 w-3 text-muted-foreground ml-1 mt-0.5 opacity-60 flex-shrink-0" />
                </span>
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent className="w-64 mt-2" side="bottom" align="start">
              <DropdownMenuLabel className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">
                Switch Scope
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem asChild>
                <Link
                  to="/instances"
                  className={cn(
                    "flex items-center gap-2 cursor-pointer rounded-sm px-2 py-1.5 text-sm focus-visible:outline-none",
                    isAllInstancesRoute ? "bg-accent text-accent-foreground font-medium" : "hover:bg-accent/80 data-[highlighted]:bg-accent/80 text-foreground"
                  )}
                >
                  <HardDrive className="h-4 w-4 flex-shrink-0" />
                  <span className="flex-1 truncate">Unified</span>
                  <span className="rounded border px-1.5 py-0.5 text-[10px] font-medium leading-none text-muted-foreground">
                    {activeInstances.length} active
                  </span>
                </Link>
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <div className="max-h-64 overflow-y-auto space-y-1">
                {activeInstances.length > 0 ? (
                  activeInstances.map((instance) => (
                    <DropdownMenuItem key={instance.id} asChild>
                      <Link
                        to="/instances/$instanceId"
                        params={{ instanceId: instance.id.toString() }}
                        className={cn(
                          "flex items-center gap-2 cursor-pointer rounded-sm px-2 py-1.5 text-sm focus-visible:outline-none",
                          instance.id === selectedInstanceId ? "bg-accent text-accent-foreground font-medium" : "hover:bg-accent/80 data-[highlighted]:bg-accent/80 text-foreground"
                        )}
                      >
                        <HardDrive className="h-4 w-4 flex-shrink-0" />
                        <span className="flex-1 truncate">{instance.name}</span>
                        <span
                          className={cn(
                            "h-2 w-2 rounded-full flex-shrink-0",
                            instance.connected ? "bg-green-500" : "bg-red-500"
                          )}
                          aria-label={instance.connected ? "Connected" : "Disconnected"}
                        />
                      </Link>
                    </DropdownMenuItem>
                  ))
                ) : (
                  <p className="px-2 py-1.5 text-xs text-muted-foreground">No active instances</p>
                )}
              </div>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          <h1 className={cn(
            "flex items-center gap-2 pl-2 sm:pl-0 text-xl font-semibold transition-all duration-300",
            "lg:hidden", // Hidden on desktop by default
            sidebarCollapsed && "lg:flex", // Visible on desktop when sidebar collapsed
            !shouldShowQuiOnMobile && "hidden sm:flex" // Hide on mobile when on instance routes
          )}>
            {theme === "swizzin" ? (
              <SwizzinLogo className="h-5 w-5" />
            ) : theme === "napster" ? (
              <NapsterLogo className="h-5 w-5" />
            ) : (
              <Logo className="h-5 w-5" />
            )}
            {instanceName ? (
              <span className="truncate max-w-32">{instanceName}</span>
            ) : "qui"}
          </h1>
        )}
      </div>

      {/* Filter button and action buttons - always on first row */}
      {shouldShowInstanceControls && (
        <>
          <div className={cn(
            "hidden md:flex items-center gap-2 order-2 lg:order-none",
            innerHeight,
            sidebarCollapsed && "lg:ml-2"
          )}>
            {/* Filter button */}
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
              <TooltipContent>{filterSidebarCollapsed ? "Show filters" : "Hide filters"}</TooltipContent>
            </Tooltip>
            {canManageSelectedInstance && (
              <>
                {/* Add Torrent button */}
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      variant="outline"
                      size="icon"
                      className="hidden md:inline-flex"
                      onClick={() => {
                        const next = { ...(routeSearch || {}), modal: "add-torrent" }
                        navigate({ search: next as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
                      }}
                    >
                      <Plus className="h-4 w-4" />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>Add torrent</TooltipContent>
                </Tooltip>
                {/* Create Torrent button - only show if instance supports it */}
                {supportsTorrentCreation && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant="outline"
                        size="icon"
                        className="hidden md:inline-flex"
                        onClick={() => {
                          const next = { ...(routeSearch || {}), modal: "create-torrent" }
                          navigate({ search: next as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
                        }}
                      >
                        <FileEdit className="h-4 w-4" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>Create torrent</TooltipContent>
                  </Tooltip>
                )}
                {/* Tasks button - only show on instance routes if torrent creation is supported */}
                {isInstanceRoute && supportsTorrentCreation && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant="outline"
                        size="icon"
                        className="hidden md:inline-flex relative"
                        onClick={() => {
                          const next = { ...(routeSearch || {}), modal: "tasks" }
                          navigate({ search: next as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
                        }}
                      >
                        <ListTodo className="h-4 w-4" />
                        {activeTaskCount > 0 && (
                          <Badge variant="default" className="absolute -top-1 -right-1 h-5 min-w-5 flex items-center justify-center p-0 text-xs">
                            {activeTaskCount}
                          </Badge>
                        )}
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>Torrent creation tasks</TooltipContent>
                  </Tooltip>
                )}
                {/* Instance settings button */}
                {isInstanceRoute && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant="outline"
                        size="icon"
                        className="hidden md:inline-flex"
                        onClick={() => setInstanceSettingsOpen(true)}
                        aria-label="Instance settings"
                      >
                        <Cog className="h-4 w-4" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>Instance settings</TooltipContent>
                  </Tooltip>
                )}
              </>
            )}
          </div>
          {/* Management Bar - only shows when torrents selected, wraps to new line on tablet */}
          {(selectedHashes.length > 0 || isAllSelected) && (
            <div className="sm:w-full sm:basis-full lg:basis-auto lg:w-auto sm:order-5 lg:order-none flex justify-center lg:justify-start lg:ml-2 animate-in fade-in duration-400 ease-out motion-reduce:animate-none motion-reduce:duration-0">
              <TorrentManagementBar
                instanceId={selectedInstanceId ?? undefined}
                selectedHashes={selectedHashes}
                selectedTorrents={selectedTorrents}
                isAllSelected={isAllSelected}
                totalSelectionCount={totalSelectionCount}
                totalSelectionSize={selectedTotalSize}
                filters={filters}
                search={routeSearch?.q}
                excludeHashes={excludeHashes}
                onComplete={clearSelection}
              />
            </div>
          )}
        </>
      )}
      {/* Instance route - search on right */}
      {shouldShowInstanceControls && (
        <div className={cn("flex items-center flex-1 gap-2 sm:order-3 lg:order-none", smInnerHeight)}>

          {/* Right side: Filter button and Search bar */}
          <div className="flex items-center gap-2 flex-1 justify-end mr-2">
            {/* Search bar - hidden on mobile (< lg), use modal search button instead */}
            <div className="relative w-full md:w-62 md:focus-within:w-full max-w-md transition-[width] duration-100 ease-out will-change-[width] hidden md:block">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none transition-opacity duration-300" />
              <Input
                ref={searchInputRef}
                placeholder={isGlobSearch ? "Glob pattern..." : `Search torrents... (${shortcutKey})`}
                value={searchValue}
                onChange={(e) => setSearchValue(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    const next = { ...(routeSearch || {}) }
                    const trimmedValue = searchValue.trim()
                    if (trimmedValue) next.q = trimmedValue
                    else delete next.q
                    navigate({ search: next as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
                  } else if (e.key === "Escape") {
                    // Clear search and blur the input
                    e.preventDefault()
                    e.stopPropagation() // Prevent event from bubbling to table selection handler
                    if (searchValue) {
                      setSearchValue("")
                    }
                    // Delay blur to match animation duration
                    setTimeout(() => {
                      searchInputRef.current?.blur()
                    }, 100)
                  }
                }}
                className={`w-full pl-9 pr-16 transition-[box-shadow,border-color] duration-200 text-xs ${searchValue ? "ring-1 ring-primary/50" : ""
                } ${isGlobSearch ? "ring-1 ring-primary" : ""}`}
              />
              <div className="absolute right-2 top-1/2 -translate-y-1/2 flex items-center gap-1">
                {/* Clear search button */}
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
                          navigate({ search: next as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
                        }}
                      >
                        <X className="h-3.5 w-3.5 text-muted-foreground" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent>Clear search</TooltipContent>
                  </Tooltip>
                )}
                {/* Info tooltip */}
                <Tooltip>
                  <TooltipTrigger asChild>
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
                      <p className="font-semibold">Smart Search Features:</p>
                      <ul className="space-y-1 ml-2">
                        <li>• <strong>Glob patterns:</strong> *.mkv, *1080p*, *S??E??*</li>
                        <li>• <strong>Fuzzy matching:</strong> "breaking bad" finds "Breaking.Bad"</li>
                        <li>• Handles dots, underscores, and brackets</li>
                        <li>• Searches name, category, and tags</li>
                        <li>• Press Enter for instant search</li>
                        <li>• Auto-searches after 500ms pause</li>
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
        <ThemeToggle />
        <div className={cn(
          "transition-all duration-300 ease-out overflow-hidden",
          sidebarCollapsed ? "w-10 opacity-100" : "w-0 opacity-0"
        )}>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" className="hover:bg-muted hover:text-foreground transition-colors relative">
                <Menu className="h-4 w-4" />
                {updateInfo && (
                  <span className="absolute top-1 right-1 h-2 w-2 bg-green-500 rounded-full" />
                )}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-52">
              {updateInfo && (
                <>
                  <DropdownMenuItem asChild>
                    <a
                      href={updateInfo.html_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="flex items-center gap-2 text-green-600 dark:text-green-400 focus:text-green-600 dark:focus:text-green-400 cursor-pointer"
                    >
                      <Download className="mr-2 h-4 w-4" />
                      <div className="flex flex-col">
                        <span className="font-medium">Update Available</span>
                        <span className="text-[10px] opacity-80">Version {updateInfo.tag_name}</span>
                      </div>
                    </a>
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                </>
              )}
              <DropdownMenuItem asChild>
                <Link
                  to="/dashboard"
                  className="flex cursor-pointer"
                >
                  <Home className="mr-2 h-4 w-4" />
                  Dashboard
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link
                  to="/search"
                  className="flex cursor-pointer"
                >
                  <Search className="mr-2 h-4 w-4" />
                  Search
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link
                  to="/cross-seed"
                  className="flex cursor-pointer"
                >
                  <GitBranch className="mr-2 h-4 w-4" />
                  Cross-Seed
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link
                  to="/automations"
                  className="flex cursor-pointer"
                >
                  <Zap className="mr-2 h-4 w-4" />
                  Automations
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link
                  to="/backups"
                  className="flex cursor-pointer"
                >
                  <Archive className="mr-2 h-4 w-4" />
                  Backups
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link
                  to="/rss"
                  className="flex cursor-pointer"
                >
                  <Rss className="mr-2 h-4 w-4" />
                  RSS
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link
                  to="/settings"
                  search={{ tab: "instances" }}
                  className="flex cursor-pointer"
                >
                  <Server className="mr-2 h-4 w-4" />
                  Instances
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link
                  to="/settings"
                  search={{ tab: "logs" }}
                  className="flex cursor-pointer"
                >
                  <FileText className="mr-2 h-4 w-4" />
                  Logs
                </Link>
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem asChild>
                <Link
                  to="/instances"
                  className={cn(
                    "flex cursor-pointer",
                    isAllInstancesRoute && "bg-accent text-accent-foreground font-medium"
                  )}
                >
                  <HardDrive className="mr-2 h-4 w-4" />
                  <span className="truncate">Unified</span>
                  <span className="ml-auto rounded border px-1.5 py-0.5 text-[10px] font-medium leading-none text-muted-foreground">
                    {activeInstances.length} active
                  </span>
                </Link>
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              {activeInstances.length > 0 ? (
                activeInstances.map((instance) => {
                  const csState = crossSeedInstanceState[instance.id]
                  const hasRss = csState?.rssEnabled || csState?.rssRunning
                  const hasSearch = csState?.searchRunning

                  return (
                    <DropdownMenuItem key={instance.id} asChild>
                      <Link
                        to="/instances/$instanceId"
                        params={{ instanceId: instance.id.toString() }}
                        className="flex cursor-pointer pl-6"
                      >
                        <HardDrive className="mr-2 h-4 w-4" />
                        <span className="truncate">{instance.name}</span>
                        <span className="ml-auto flex items-center gap-1.5">
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
                                RSS {csState?.rssRunning ? "running" : "enabled"}
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
                                Scan running
                              </TooltipContent>
                            </Tooltip>
                          )}
                          <span
                            className={cn(
                              "h-2 w-2 rounded-full flex-shrink-0",
                              instance.connected ? "bg-green-500" : "bg-red-500"
                            )}
                          />
                        </span>
                      </Link>
                    </DropdownMenuItem>
                  )
                })
              ) : (
                <DropdownMenuItem disabled className="text-xs text-muted-foreground">
                  No active instances
                </DropdownMenuItem>
              )}
              <DropdownMenuSeparator />
              <DropdownMenuItem asChild>
                <Link
                  to="/settings"
                  className="flex cursor-pointer"
                >
                  <Settings className="mr-2 h-4 w-4" />
                  Settings
                </Link>
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => logout()}>
                <LogOut className="mr-2 h-4 w-4" />
                Logout
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {/* Instance Preferences Dialog */}
      {selectedInstanceId !== null && selectedInstanceId > ALL_INSTANCES_ID && instanceName && (
        <InstancePreferencesDialog
          open={instanceSettingsOpen}
          onOpenChange={setInstanceSettingsOpen}
          instanceId={selectedInstanceId}
          instanceName={instanceName}
          instance={currentInstance}
        />
      )}
    </header>
  )
}
