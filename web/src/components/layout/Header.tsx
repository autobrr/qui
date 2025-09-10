/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TorrentManagementBar } from "@/components/torrents/TorrentManagementBar"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
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
import { cn } from "@/lib/utils"
import { Link, useNavigate, useRouterState, useSearch } from "@tanstack/react-router"
import { FunnelPlus, FunnelX, HardDrive, Home, Info, LogOut, Menu, Plus, Search, Server, Settings, X } from "lucide-react"
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react"
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

  return (
    <header className="sticky top-0 z-50 flex h-16 items-center justify-between sm:border-b bg-background pl-1 pr-4 sm:pr-6 lg:static">
      <div className="flex items-center gap-2 mr-2">
        {children}

        {/* Logo/title only shows when sidebar is collapsed */}
        {sidebarCollapsed && (
          <h1 className={cn(
            "flex items-center gap-2 pl-2 sm:pl-0 text-lg font-semibold transition-opacity duration-300",
            "lg:opacity-100 lg:pointer-events-auto",
            !shouldShowQuiOnMobile && "hidden sm:flex"
          )}>
            {theme === "swizzin" ? (
              <SwizzinLogo className="h-5 w-5 flex-shrink-0" />
            ) : (
              <Logo className="h-5 w-5 flex-shrink-0" />
            )}
            <span className="truncate max-w-[200px]">
              {instanceName ? instanceName : "qui"}
            </span>
          </h1>
        )}

        {/* Filter button and management bar always show on instance routes */}
        {isInstanceRoute && (
          <div className="hidden lg:flex items-center gap-2 ml-2">
            {/* Filter button */}
            {/* Filter button */}
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="icon"
                  className="hidden xl:inline-flex"
                  onClick={() => setFilterSidebarCollapsed(!filterSidebarCollapsed)}
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
            {/* Conditional Management Bar with smooth animations */}
            {(selectedHashes.length > 0 || isAllSelected) ? (
              <div className="animate-in fade-in duration-400 ease-out motion-reduce:animate-none motion-reduce:duration-0">
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
            ) : (
              <div className="h-9" aria-hidden="true" />
            )}
          </div>
        )}
      </div>

      {/* Instance route - search on right */}
      {isInstanceRoute && (
        <div className="flex items-center flex-1 gap-2">

          {/* Right side: Filter button and Search bar */}
          <div className="flex items-center gap-2 flex-1 justify-end mr-2">
            {/* Search bar */}
            <div className="relative w-full max-w-md">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none"/>
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
                  }
                }}
                className={`w-full pl-9 pr-16 transition-all text-xs ${
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


      <div className="grid grid-cols-[auto_auto] items-center gap-1 transition-all duration-300 ease-out">
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