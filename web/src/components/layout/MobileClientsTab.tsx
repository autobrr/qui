/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { InstanceMarkIcon } from "@/components/instances/marks/InstanceMarkIcon"
import { UnifiedScopeDropdownSection } from "@/components/layout/UnifiedScopeDropdownSection"
import { Badge } from "@/components/ui/badge"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useCrossSeedInstanceState } from "@/hooks/useCrossSeedInstanceState"
import { usePersistedUnifiedInstanceFilter } from "@/hooks/usePersistedUnifiedInstanceFilter"
import { api } from "@/lib/api"
import { normalizeUnifiedInstanceIds } from "@/lib/instances"
import { navigateWithSearch } from "@/lib/router-search"
import { cn } from "@/lib/utils"
import { useQuery } from "@tanstack/react-query"
import { Link, useLocation, useNavigate, useSearch } from "@tanstack/react-router"
import { HardDrive, Loader2, Rss, SearchCode } from "lucide-react"
import { useCallback, useMemo } from "react"
import { useTranslation } from "react-i18next"

const tabClass = "flex flex-col items-center justify-center gap-1 px-0.5 py-2 text-xs font-medium min-w-0 flex-1"

export function MobileClientsTab() {
  const { t } = useTranslation("common")
  const location = useLocation()
  const navigate = useNavigate()
  const routeSearch = useSearch({ strict: false }) as Record<string, unknown> | undefined

  const { data: instances, isPending: isLoadingInstances } = useQuery({
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
  const currentInstanceId = !isOnAllInstancesPage && location.pathname.startsWith("/instances/") ? location.pathname.split("/")[2] : null
  const currentInstance = instances?.find(i => i.id.toString() === currentInstanceId)
  const currentInstanceLabel = isOnAllInstancesPage? (hasMultipleActiveInstances ? t("header.unified") : (activeInstances[0]?.name ?? null)): (currentInstance && currentInstance.isActive ? currentInstance.name : null)

  if (activeInstances.length === 0) {
    return (
      <button className={cn(tabClass, "text-muted-foreground")} type="button" disabled>
        <HardDrive className={cn("h-5 w-5", isLoadingInstances && "animate-pulse")} />
        <span className="block max-w-full truncate text-center">
          {isLoadingInstances ? t("mobileNav.loading") : t("mobileNav.noActiveClients")}
        </span>
      </button>
    )
  }

  const activeTabClass = cn(
    tabClass,
    "transition-colors hover:cursor-pointer",
    isOnInstancePage ? "text-primary" : "text-muted-foreground hover:text-foreground"
  )
  const tabContent = (
    <>
      <div className="relative">
        <HardDrive className={cn("h-5 w-5", isOnInstancePage && "text-primary")} />
        <Badge
          className="absolute -top-1 -right-2 h-4 w-4 p-0 flex items-center justify-center text-[9px]"
          variant="default"
        >
          {activeInstances.length}
        </Badge>
      </div>
      <TruncatedText className="block max-w-full text-center" openOnTap={false}>
        {currentInstanceLabel ?? t("mobileNav.clients")}
      </TruncatedText>
    </>
  )

  if (!hasMultipleActiveInstances) {
    return (
      <Link
        to="/instances/$instanceId"
        params={{ instanceId: activeInstances[0].id.toString() }}
        className={activeTabClass}
      >
        {tabContent}
      </Link>
    )
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button type="button" className={activeTabClass}>
          {tabContent}
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="center" side="top" className="w-56 mb-2">
        <DropdownMenuLabel>{t("mobileNav.qbittorrentClients")}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuLabel className="text-xs uppercase tracking-wide text-muted-foreground">
          {t("mobileNav.instances")}
        </DropdownMenuLabel>
        <UnifiedScopeDropdownSection
          activeInstances={activeInstances}
          effectiveUnifiedInstanceIds={effectiveUnifiedInstanceIds}
          isAllInstancesRoute={isOnAllInstancesPage}
          onResetUnifiedScope={resetUnifiedScope}
          onToggleUnifiedScopeInstance={toggleUnifiedScopeInstance}
          scopeKeyPrefix="mobile-scope"
        />
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
                <InstanceMarkIcon instanceId={instance.id} problem={!instance.connected} />
                <TruncatedText className="flex-1 min-w-0" openOnTap={false}>
                  {instance.name}
                </TruncatedText>
                {!instance.connected && <span className="sr-only">{t("header.disconnected")}</span>}
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
                </span>
              </Link>
            </DropdownMenuItem>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
