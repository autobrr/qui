/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

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
import { SwizzinLogo } from "@/components/ui/SwizzinLogo"
import { ThemeToggle } from "@/components/ui/ThemeToggle"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"
import { useTorrentSelection } from "@/contexts/TorrentSelectionContext"
import { useAuth } from "@/hooks/useAuth"
import { useDebounce } from "@/hooks/useDebounce"
import { useInstances } from "@/hooks/useInstances"
import { usePersistedFilterSidebarState } from "@/hooks/usePersistedFilterSidebarState"
import { useTheme } from "@/hooks/useTheme"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate, useRouterState, useSearch } from "@tanstack/react-router"
import { ChevronsUpDown, FileEdit, FunnelPlus, FunnelX, HardDrive, Home, Info, ListTodo, LogOut, Menu, Plus, Search, Server, Settings, X } from "lucide-react"
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
  const routeSearch = useSearch({ strict: false }) as { q?: string; modal?: string; [key: string]: unknown }

  // Get selection state from context
  const {
    selectedHashes,
    selectedTorrents,
    isAllSelected,
    totalSelectionCount,
    excludeHashes,
    filters,
    clearSelection,
  } = useTorrentSelection()

  const instanceId = useRouterState({
    select: (s) => s.matches.find((m) => m.routeId === "/_authenticated/instances/$instanceId")?.params?.instanceId as string | undefined,
  })
  const selectedInstanceId = useMemo(() => {
    const parsed = instanceId ? parseInt(instanceId, 10) : NaN
    return Number.isFinite(parsed) ? parsed : null
  }, [instanceId])
  const isInstanceRoute = selectedInstanceId !== null

  const shouldShowQuiOnMobile = !isInstanceRoute
  const [searchValue, setSearchValue] = useState<string>(routeSearch?.q || "")
  const debouncedSearch = useDebounce(searchValue, 500)
  const { instances } = useInstances()


  const instanceName = useMemo(() => {
    if (!isInstanceRoute || !instances || selectedInstanceId === null) return null
    return instances.find(i => i.id === selectedInstanceId)?.name ?? null
  }, [isInstanceRoute, instances, selectedInstanceId])

  // Keep local state in sync with URL when navigating between instances/routes
  useEffect(() => {
    setSearchValue(routeSearch?.q || "")
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedInstanceId])

  // Update URL search param after debounce
  useEffect(() => {
    if (!isInstanceRoute) return
    const next = { ...(routeSearch || {}) }
    if (debouncedSearch) next.q = debouncedSearch
    else delete next.q
    navigate({ search: next as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedSearch, isInstanceRoute])

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
    },
    [isInstanceRoute]
  )
  const { theme } = useTheme()

  // TODO: I dont like this
  // Query torrent creation tasks for badge count (only for instance routes)
  const { data: tasks } = useQuery({
    queryKey: ["torrent-creation-tasks", selectedInstanceId],
    queryFn: () => selectedInstanceId !== null ? api.getTorrentCreationTasks(selectedInstanceId) : Promise.resolve([]),
    enabled: selectedInstanceId !== null,
    refetchInterval: 5000, // Poll every 5 seconds for badge updates
  })

  // Count active tasks (queued or running)
  const activeTaskCount = tasks?.filter((t) => t.status === "Running" || t.status === "Queued").length || 0

  return (
    <header className="sticky top-0 z-50 flex flex-wrap lg:flex-nowrap items-start lg:items-center justify-between sm:border-b bg-background pl-2 pr-4 md:pl-4 md:pr-4 lg:pl-0 lg:static py-2 lg:py-0 lg:h-16">
      <div className="hidden md:flex items-center gap-2 mr-2 h-12 lg:h-auto order-1 lg:order-none">
        {children}
        {instanceName && instances && instances.length > 1 ? (
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
                Switch Instance
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <div className="max-h-64 overflow-y-auto space-y-1">
                {instances.map((instance) => (
                  <DropdownMenuItem key={instance.id} asChild>
                    <Link
                      to="/instances/$instanceId"
                      params={{ instanceId: instance.id.toString() }}
                      className={cn(
                        "flex items-center gap-2 cursor-pointer rounded-sm px-2 py-1.5 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1",
                        instance.id === selectedInstanceId? "bg-accent text-accent-foreground font-medium": "hover:bg-accent/80 data-[highlighted]:bg-accent/80 focus-visible:bg-accent/20 text-foreground"
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
                ))}
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
      {isInstanceRoute && (
        <>
          <div className={cn(
            "hidden md:flex items-center gap-2 h-12 lg:h-auto order-2 lg:order-none",
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
                    <FunnelPlus className="h-4 w-4"/>
                  ) : (
                    <FunnelX className="h-4 w-4"/>
                  )}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{filterSidebarCollapsed ? "Show filters" : "Hide filters"}</TooltipContent>
            </Tooltip>
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
                  <Plus className="h-4 w-4"/>
                </Button>
              </TooltipTrigger>
              <TooltipContent>Add torrent</TooltipContent>
            </Tooltip>
            {/* Create Torrent button */}
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
                  <FileEdit className="h-4 w-4"/>
                </Button>
              </TooltipTrigger>
              <TooltipContent>Create torrent</TooltipContent>
            </Tooltip>
            {/* Tasks button */}
            {tasks && tasks.length > 0 && (
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
                    <ListTodo className="h-4 w-4"/>
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
          </div>
          {/* Management Bar - only shows when torrents selected, wraps to new line on tablet */}
          {(selectedHashes.length > 0 || isAllSelected) && (
            <div className="sm:w-full sm:basis-full lg:basis-auto lg:w-auto sm:order-5 lg:order-none flex justify-center lg:justify-start lg:ml-2 animate-in fade-in duration-400 ease-out motion-reduce:animate-none motion-reduce:duration-0">
              <TorrentManagementBar
                instanceId={selectedInstanceId || undefined}
                selectedHashes={selectedHashes}
                selectedTorrents={selectedTorrents}
                isAllSelected={isAllSelected}
                totalSelectionCount={totalSelectionCount}
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
      {isInstanceRoute && (
        <div className="flex items-center flex-1 gap-2 sm:order-3 lg:order-none sm:h-12 lg:h-auto">

          {/* Right side: Filter button and Search bar */}
          <div className="flex items-center gap-2 flex-1 justify-end mr-2">
            {/* Search bar */}
            <div className="relative w-full md:w-62 md:focus-within:w-full max-w-md transition-[width] duration-100 ease-out will-change-[width]">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none transition-opacity duration-300"/>
              <Input
                ref={searchInputRef}
                placeholder={isGlobSearch ? "Glob pattern..." : `Search torrents... (${shortcutKey})`}
                value={searchValue}
                onChange={(e) => setSearchValue(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    const next = { ...(routeSearch || {}) }
                    if (searchValue) next.q = searchValue
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
                className={`w-full pl-9 pr-16 transition-[box-shadow,border-color] duration-200 text-xs ${
                  searchValue ? "ring-1 ring-primary/50" : ""
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
                        <X className="h-3.5 w-3.5 text-muted-foreground"/>
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
                      <Info className="h-3.5 w-3.5 text-muted-foreground"/>
                    </button>
                  </TooltipTrigger>
                  <TooltipContent className="max-w-xs">
                    <div className="space-y-2 text-xs">
                      <p className="font-semibold">Smart Search Features:</p>
                      <ul className="space-y-1 ml-2">
                        <li>• <strong>Glob patterns:</strong> *.mkv, *1080p*, S??E??</li>
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
            <span id="header-search-actions" className="flex items-center gap-1"/>
          </div>
        </div>
      )}


      <div className="grid grid-cols-[auto_auto] items-center gap-1 transition-all duration-300 ease-out sm:order-4 lg:order-none sm:h-12 lg:h-auto">
        <ThemeToggle/>
        <div className={cn(
          "transition-all duration-300 ease-out overflow-hidden",
          sidebarCollapsed ? "w-10 opacity-100" : "w-0 opacity-0"
        )}>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" className="hover:bg-muted hover:text-foreground transition-colors">
                <Menu className="h-4 w-4"/>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-52">
              <DropdownMenuItem asChild>
                <Link
                  to="/dashboard"
                  className="flex cursor-pointer"
                >
                  <Home className="mr-2 h-4 w-4"/>
                  Dashboard
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link
                  to="/instances"
                  className="flex cursor-pointer"
                >
                  <Server className="mr-2 h-4 w-4"/>
                  Instances
                </Link>
              </DropdownMenuItem>
              {instances && instances.length > 0 && (
                <>
                  {instances.map((instance) => (
                    <DropdownMenuItem key={instance.id} asChild>
                      <Link
                        to="/instances/$instanceId"
                        params={{ instanceId: instance.id.toString() }}
                        className="flex cursor-pointer pl-6"
                      >
                        <HardDrive className="mr-2 h-4 w-4"/>
                        <span className="truncate">{instance.name}</span>
                        <span
                          className={cn(
                            "ml-auto h-2 w-2 rounded-full flex-shrink-0",
                            instance.connected ? "bg-green-500" : "bg-red-500"
                          )}
                        />
                      </Link>
                    </DropdownMenuItem>
                  ))}
                </>
              )}
              <DropdownMenuSeparator/>
              <DropdownMenuItem asChild>
                <Link
                  to="/settings"
                  className="flex cursor-pointer"
                >
                  <Settings className="mr-2 h-4 w-4"/>
                  Settings
                </Link>
              </DropdownMenuItem>
              <DropdownMenuSeparator/>
              <DropdownMenuItem onClick={() => logout()}>
                <LogOut className="mr-2 h-4 w-4"/>
                Logout
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
    </header>
  )
}
