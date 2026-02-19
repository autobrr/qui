/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Logo } from "@/components/ui/Logo"
import { NapsterLogo } from "@/components/ui/NapsterLogo"
import { Separator } from "@/components/ui/separator"
import { SwizzinLogo } from "@/components/ui/SwizzinLogo"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { UpdateBanner } from "@/components/ui/UpdateBanner"
import { useAuth } from "@/hooks/useAuth"
import { useCrossSeedInstanceState } from "@/hooks/useCrossSeedInstanceState"
import { useTheme } from "@/hooks/useTheme"
import { api } from "@/lib/api"
import { getAppVersion } from "@/lib/build-info"
import {
  encodeUnifiedInstanceIds,
  normalizeUnifiedInstanceIds,
  resolveUnifiedInstanceIds,
  UNIFIED_INSTANCE_IDS_SEARCH_PARAM
} from "@/lib/instances"
import { cn } from "@/lib/utils"
import { useQuery } from "@tanstack/react-query"
import { Link, useLocation, useNavigate, useSearch } from "@tanstack/react-router"
import {
  Archive,
  Copyright,
  FileText,
  GitBranch,
  Github,
  HardDrive,
  Home,
  Loader2,
  LogOut,
  Rss,
  Search,
  SearchCode,
  Settings,
  SlidersHorizontal,
  Zap
} from "lucide-react"
import { useCallback, useMemo } from "react"

interface NavItem {
  id: string
  title: string
  href: string
  icon: React.ComponentType<{ className?: string }>
  params?: Record<string, string>
  search?: Record<string, unknown>
  isActive?: (pathname: string, search: Record<string, unknown> | undefined) => boolean
}

const navigation: NavItem[] = [
  {
    id: "dashboard",
    title: "Dashboard",
    href: "/dashboard",
    icon: Home,
  },
  {
    id: "search",
    title: "Search",
    href: "/search",
    icon: Search,
  },
  {
    id: "cross-seed",
    title: "Cross-Seed",
    href: "/cross-seed",
    icon: GitBranch,
    params: {},
  },
  {
    id: "automations",
    title: "Automations",
    href: "/automations",
    icon: Zap,
  },
  {
    id: "backups",
    title: "Backups",
    href: "/backups",
    icon: Archive,
  },
  {
    id: "rss",
    title: "RSS",
    href: "/rss",
    icon: Rss,
  },
  {
    id: "settings",
    title: "Settings",
    href: "/settings",
    icon: Settings,
    isActive: (pathname, search) => pathname === "/settings" && search?.tab !== "logs",
  },
  {
    id: "logs",
    title: "Logs",
    href: "/settings",
    icon: FileText,
    search: { tab: "logs" },
    isActive: (pathname, search) => pathname === "/settings" && search?.tab === "logs",
  },
]

export function Sidebar() {
  const location = useLocation()
  const navigate = useNavigate()
  const routeSearch = useSearch({ strict: false }) as Record<string, unknown> | undefined
  const { logout } = useAuth()
  const { theme } = useTheme()

  const { data: instances } = useQuery({
    queryKey: ["instances"],
    queryFn: () => api.getInstances(),
  })
  const activeInstances = instances?.filter(instance => instance.isActive) ?? []
  const activeInstanceIds = useMemo(
    () => activeInstances.map(instance => instance.id),
    [activeInstances]
  )
  const effectiveUnifiedInstanceIds = useMemo(
    () => resolveUnifiedInstanceIds(routeSearch?.[UNIFIED_INSTANCE_IDS_SEARCH_PARAM], activeInstanceIds),
    [routeSearch, activeInstanceIds]
  )
  const normalizedUnifiedInstanceIds = useMemo(
    () => normalizeUnifiedInstanceIds(effectiveUnifiedInstanceIds, activeInstanceIds),
    [effectiveUnifiedInstanceIds, activeInstanceIds]
  )
  const isAllInstancesActive = location.pathname === "/instances" || location.pathname === "/instances/"
  const hasCustomUnifiedScope = normalizedUnifiedInstanceIds.length > 0
  const unifiedScopeSummary = `${effectiveUnifiedInstanceIds.length}/${activeInstances.length}`
  const applyUnifiedScope = useCallback((nextIds: number[]) => {
    const normalizedIds = normalizeUnifiedInstanceIds(nextIds, activeInstanceIds)
    const nextSearch: Record<string, unknown> = isAllInstancesActive ? { ...(routeSearch || {}) } : {}
    const encoded = encodeUnifiedInstanceIds(normalizedIds)

    if (encoded) {
      nextSearch[UNIFIED_INSTANCE_IDS_SEARCH_PARAM] = encoded
    } else {
      delete nextSearch[UNIFIED_INSTANCE_IDS_SEARCH_PARAM]
    }

    navigate({
      to: "/instances",
      search: nextSearch as any, // eslint-disable-line @typescript-eslint/no-explicit-any
      replace: isAllInstancesActive,
    })
  }, [activeInstanceIds, isAllInstancesActive, navigate, routeSearch])
  const toggleUnifiedScopeInstance = useCallback((instanceId: number) => {
    const currentlySelected = effectiveUnifiedInstanceIds.includes(instanceId)
    const nextIds = currentlySelected
      ? effectiveUnifiedInstanceIds.filter(id => id !== instanceId)
      : [...effectiveUnifiedInstanceIds, instanceId]

    if (nextIds.length === 0) {
      return
    }

    applyUnifiedScope(nextIds)
  }, [applyUnifiedScope, effectiveUnifiedInstanceIds])
  const hasConfiguredInstances = (instances?.length ?? 0) > 0

  const { state: crossSeedInstanceState } = useCrossSeedInstanceState()

  const appVersion = getAppVersion()

  return (
    <div className="flex h-full w-64 flex-col border-r bg-sidebar border-sidebar-border">
      <div className="p-6">
        <h2 className="flex items-center gap-2 text-lg font-semibold text-sidebar-foreground">
          {theme === "swizzin" ? (
            <SwizzinLogo className="h-5 w-5" />
          ) : theme === "napster" ? (
            <NapsterLogo className="h-5 w-5" />
          ) : (
            <Logo className="h-5 w-5" />
          )}
          qui
        </h2>
      </div>

      <nav className="flex flex-1 min-h-0 flex-col px-3">
        <div className="space-y-1">
          {navigation.map((item) => {
            const Icon = item.icon
            const isActive = item.isActive
              ? item.isActive(location.pathname, routeSearch)
              : location.pathname === item.href

            return (
              <Link
                key={item.id}
                to={item.href}
                params={item.params}
                search={item.search}
                className={cn(
                  "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-all duration-200 ease-out",
                  isActive? "bg-sidebar-primary text-sidebar-primary-foreground": "text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
                )}
              >
                <Icon className="h-4 w-4" />
                {item.title}
              </Link>
            )
          })}
        </div>

        <Separator className="my-4" />

        <div className="flex-1 min-h-0">
          <div className="flex h-full min-h-0 flex-col">
            <p className="px-3 text-xs font-semibold uppercase tracking-wider text-sidebar-foreground/70">
              Instances
            </p>
            <div className="mt-1 flex-1 overflow-y-auto space-y-1 pr-1">
              <Link
                to="/instances"
                search={hasCustomUnifiedScope ? { [UNIFIED_INSTANCE_IDS_SEARCH_PARAM]: encodeUnifiedInstanceIds(normalizedUnifiedInstanceIds) } : undefined}
                className={cn(
                  "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-all duration-200 ease-out",
                  isAllInstancesActive ? "bg-sidebar-primary text-sidebar-primary-foreground" : "text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
                )}
              >
                <HardDrive className="h-4 w-4 flex-shrink-0" />
                <span className="truncate max-w-36">Unified</span>
                <span
                  className={cn(
                    "ml-auto rounded border px-1.5 py-0.5 text-[10px] font-medium leading-none flex-shrink-0",
                    isAllInstancesActive ? "border-sidebar-primary-foreground/35 text-sidebar-primary-foreground/90" : "border-sidebar-border text-sidebar-foreground/70"
                  )}
                >
                  {activeInstances.length} active
                </span>
                {hasCustomUnifiedScope && (
                  <span
                    className={cn(
                      "rounded border px-1.5 py-0.5 text-[10px] font-medium leading-none flex-shrink-0",
                      isAllInstancesActive ? "border-sidebar-primary-foreground/35 text-sidebar-primary-foreground/90" : "border-sidebar-border text-sidebar-foreground/70"
                    )}
                  >
                    {unifiedScopeSummary}
                  </span>
                )}
              </Link>
              {activeInstances.length > 1 && (
                <div className="px-3">
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <button
                        type="button"
                        className={cn(
                          "mt-1 w-full rounded-md border border-sidebar-border/70 px-2 py-1 text-xs",
                          "text-sidebar-foreground/80 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
                          "inline-flex items-center gap-1.5"
                        )}
                      >
                        <SlidersHorizontal className="h-3 w-3" />
                        Scope
                      </button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent side="right" align="start" className="w-56">
                      <DropdownMenuLabel className="text-xs uppercase tracking-wide text-muted-foreground">
                        Unified Scope
                      </DropdownMenuLabel>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem
                        onSelect={(event) => {
                          event.preventDefault()
                          applyUnifiedScope(activeInstanceIds)
                        }}
                        className="cursor-pointer text-xs"
                      >
                        All active ({activeInstances.length})
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                      {activeInstances.map((instance) => {
                        const checked = effectiveUnifiedInstanceIds.includes(instance.id)
                        return (
                          <DropdownMenuCheckboxItem
                            key={`sidebar-scope-${instance.id}`}
                            checked={checked}
                            onSelect={(event) => {
                              event.preventDefault()
                              toggleUnifiedScopeInstance(instance.id)
                            }}
                            className="cursor-pointer"
                          >
                            <span className="flex w-full items-center justify-between gap-2">
                              <span className="truncate">{instance.name}</span>
                              <span
                                className={cn(
                                  "h-2 w-2 rounded-full flex-shrink-0",
                                  instance.connected ? "bg-green-500" : "bg-red-500"
                                )}
                                aria-hidden="true"
                              />
                            </span>
                          </DropdownMenuCheckboxItem>
                        )
                      })}
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              )}
              <Separator className="my-2" />
              {activeInstances.map((instance) => {
                const instancePath = `/instances/${instance.id}`
                const isActive = location.pathname === instancePath || location.pathname.startsWith(`${instancePath}/`)
                const csState = crossSeedInstanceState[instance.id]
                const hasRss = csState?.rssEnabled || csState?.rssRunning
                const hasSearch = csState?.searchRunning

                return (
                  <Link
                    key={instance.id}
                    to="/instances/$instanceId"
                    params={{ instanceId: instance.id.toString() }}
                    className={cn(
                      "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-all duration-200 ease-out",
                      isActive? "bg-sidebar-primary text-sidebar-primary-foreground": "text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
                    )}
                  >
                    <HardDrive className="h-4 w-4 flex-shrink-0" />
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
                            RSS {csState?.rssRunning ? "running" : "enabled"}
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
                )
              })}
              {activeInstances.length === 0 && (
                <p className="px-3 py-2 text-sm text-sidebar-foreground/50">
                  {hasConfiguredInstances ? "All instances are disabled" : "No instances configured"}
                </p>
              )}
            </div>
          </div>
        </div>
      </nav>

      <div className="mt-auto space-y-3 p-3">
        <UpdateBanner />

        <Button
          variant="ghost"
          className="w-full justify-start"
          onClick={() => logout()}
        >
          <LogOut className="mr-2 h-4 w-4" />
          Logout
        </Button>

        <Separator className="mx-3 mb-3" />

        <div className="flex items-center justify-between px-3 pb-3">
          <div className="flex flex-col gap-1 text-[10px] text-sidebar-foreground/40 select-none">
            <span className="font-medium text-sidebar-foreground/50">Version {appVersion}</span>
            <div className="flex items-center gap-1">
              <Copyright className="h-2.5 w-2.5" />
              <span>{new Date().getFullYear()} autobrr</span>
            </div>
          </div>
          <Button
            variant="ghost"
            size="icon"
            className="h-6 w-6 text-sidebar-foreground/40 hover:text-sidebar-foreground"
            asChild
          >
            <a
              href="https://github.com/autobrr/qui"
              target="_blank"
              rel="noopener noreferrer"
              aria-label="View on GitHub"
            >
              <Github className="h-3.5 w-3.5" />
            </a>
          </Button>
        </div>
      </div>
    </div>
  )
}
