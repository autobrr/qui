/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useAuth, } from "@/hooks/useAuth"
import { Button, } from "@/components/ui/button"
import { Input, } from "@/components/ui/input"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { 
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { User, LogOut, Key, Search, Info, Filter, Plus, X, } from "lucide-react"
import { ThemeToggle, } from "@/components/ui/ThemeToggle"
import { cn, } from "@/lib/utils"
import { Link, useNavigate, useSearch, useRouterState, } from "@tanstack/react-router"
import { useEffect, useMemo, useState, } from "react"
import { useDebounce, } from "@/hooks/useDebounce"
import { usePersistedFilterSidebarState, } from "@/hooks/usePersistedFilterSidebarState"
import { useInstances, } from "@/hooks/useInstances"

interface HeaderProps {
  children?: React.ReactNode
  sidebarCollapsed?: boolean
}

export function Header({ children, sidebarCollapsed = false, }: HeaderProps,) {
  const { user, logout, } = useAuth()
  const navigate = useNavigate()
  const routeSearch = useSearch({ strict: false, },) as any

  const instanceId = useRouterState({
    select: (s,) => s.matches.find((m,) => m.routeId === "/_authenticated/instances/$instanceId",)?.params?.instanceId as string | undefined,
  },)
  const selectedInstanceId = useMemo(() => {
    const parsed = instanceId ? parseInt(instanceId, 10,) : NaN
    return Number.isFinite(parsed,) ? parsed : null
  }, [instanceId,],)
  const isInstanceRoute = selectedInstanceId !== null

  const shouldShowQuiOnMobile = !isInstanceRoute
  const [searchValue, setSearchValue,] = useState<string>(routeSearch?.q || "",)
  const debouncedSearch = useDebounce(searchValue, 1000,)
  const { instances, } = useInstances()

  const instanceName = useMemo(() => {
    if (!isInstanceRoute || !instances || selectedInstanceId === null) return null
    return instances.find(i => i.id === selectedInstanceId,)?.name ?? null
  }, [isInstanceRoute, instances, selectedInstanceId,],)

  // Keep local state in sync with URL when navigating between instances/routes
  useEffect(() => {
    setSearchValue(routeSearch?.q || "",)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedInstanceId,],)

  // Update URL search param after debounce
  useEffect(() => {
    if (!isInstanceRoute) return
    const next = { ...(routeSearch || {}), }
    if (debouncedSearch) next.q = debouncedSearch
    else delete next.q
    navigate({ search: next, replace: true, },)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedSearch, isInstanceRoute,],)

  const isGlobSearch = !!searchValue && /[*?\[\]]/.test(searchValue,)
  const [filterSidebarCollapsed, setFilterSidebarCollapsed,] = usePersistedFilterSidebarState(false,)

  return (
    <header className="sticky top-0 z-50 flex h-16 items-center justify-between sm:border-b bg-background pl-1 pr-4 sm:pr-6 lg:static">
      <div className="flex items-center gap-2">
        {children}
        <h1 className={cn(
          "text-xl font-semibold transition-opacity duration-300",
          shouldShowQuiOnMobile ? "block sm:hidden pl-4" : "hidden", // Show 'qui' on mobile for non-instance routes
          sidebarCollapsed && "sm:block",
        )}>{instanceName ? `qui - ${instanceName}` : "qui"}</h1>
        {isInstanceRoute && (
          <div className="ml-2 hidden sm:block">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    const next = { ...(routeSearch || {}), modal: "add-torrent", }
                    navigate({ search: next, replace: true, },)
                  }}
                >
                  <Plus className="h-4 w-4 sm:mr-2" />
                  <span className="hidden sm:inline">Add Torrent</span>
                </Button>
              </TooltipTrigger>
              <TooltipContent>Add Torrent</TooltipContent>
            </Tooltip>
          </div>
        )}
      </div>

      {/* Instance search bar */}
      {isInstanceRoute && (
        <div className="flex-1 max-w-xl mx-2">
          <div className="flex items-center gap-2">
            {/* Slot to place actions directly to the left of the filter button (desktop only) */}
            <span id="header-left-of-filter" className="hidden xl:inline-flex" />
            <Button
              variant="outline"
              size="icon"
              className="hidden xl:inline-flex"
              onClick={() => setFilterSidebarCollapsed(!filterSidebarCollapsed,)}
              title={filterSidebarCollapsed ? "Show filters" : "Hide filters"}
            >
              <Filter className="h-4 w-4" />
            </Button>
            {/* Mobile filter button moved to card/table toolbars */}
            <div className="relative flex-1">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
              <Input
                placeholder={isGlobSearch ? "Glob pattern..." : "Search torrents..."}
                value={searchValue}
                onChange={(e,) => setSearchValue(e.target.value,)}
                onKeyDown={(e,) => {
                  if (e.key === "Enter") {
                    const next = { ...(routeSearch || {}), }
                    if (searchValue) next.q = searchValue
                    else delete next.q
                    navigate({ search: next, replace: true, },)
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
                          setSearchValue("",)
                          const next = { ...(routeSearch || {}), }
                          delete next.q
                          navigate({ search: next, replace: true, },)
                        }}
                      >
                        <X className="h-3.5 w-3.5 text-muted-foreground" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent>Clear search</TooltipContent>
                  </Tooltip>
                )}
                {/* Slot for actions next to search (e.g., Toggle columns) */}
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      className="p-1 hover:bg-muted rounded-sm transition-colors hidden sm:block"
                      onClick={(e,) => e.preventDefault()}
                    >
                      <Info className="h-3.5 w-3.5 text-muted-foreground" />
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
                        <li>• Auto-searches after 1 second pause</li>
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

      <div className="flex items-center gap-3">
        <ThemeToggle />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="sm" className="gap-2">
              <User className="h-4 w-4" />
              <span className="hidden sm:inline">{user?.username}</span>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuLabel>My Account</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild>
              <Link to="/settings" search={{ tab: "api", }} className="flex cursor-pointer">
                <Key className="mr-2 h-4 w-4" />
                API Keys
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
    </header>
  )
}