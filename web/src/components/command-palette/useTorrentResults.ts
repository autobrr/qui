/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useLayoutRoute } from "@/contexts/LayoutRouteContext"
import { useCopyGroupRows } from "@/hooks/useCopyGroupRows"
import { useDebounce } from "@/hooks/useDebounce"
import { useInstances } from "@/hooks/useInstances"
import { useIsMobile } from "@/hooks/useMediaQuery"
import { usePersistedColumnFilters } from "@/hooks/usePersistedColumnFilters"
import { usePersistedFilters } from "@/hooks/usePersistedFilters"
import { usePersistedUnifiedInstanceFilter } from "@/hooks/usePersistedUnifiedInstanceFilter"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { api } from "@/lib/api"
import { type ColumnFilter, columnFiltersToExpr } from "@/lib/column-filter-utils"
import { buildVisibleRows, COLLAPSED_COPY_GROUPS } from "@/lib/copy-groups"
import { hasSidebarFilters } from "@/lib/filter-views"
import { getLinuxIsoName, getLinuxTrackerDomain, useIncognitoMode } from "@/lib/incognito"
import { ALL_INSTANCES_ID, normalizeUnifiedInstanceIds } from "@/lib/instances"
import { navigateWithSearch } from "@/lib/router-search"
import { combineFilterExpr, isCrossSeedExpr } from "@/lib/torrent-filters"
import { EMPTY_SELECTION } from "@/lib/torrent-selection"
import { getStateLabel } from "@/lib/torrent-state-utils"
import { buildTrackerCustomizationLookup, extractTrackerHost, resolveTrackerDisplay } from "@/lib/tracker-customizations"
import { formatBytes } from "@/lib/utils"
import type { CrossInstanceTorrent, TorrentFilters } from "@/types"
import { keepPreviousData, useQuery } from "@tanstack/react-query"
import { useLocation, useNavigate } from "@tanstack/react-router"
import { ArrowDownUp, TextSearch } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { PaletteEntry } from "./palette"

const RESULT_LIMIT = 6
const MIN_QUERY_LENGTH = 2
export const PARTIAL_RETRY_MS = 3_000
const NO_TORRENTS: CrossInstanceTorrent[] = []
const torrentKey = (torrent: CrossInstanceTorrent) => `${torrent.instanceId}:${torrent.hash}`

export interface TorrentResults {
  entries: PaletteEntry[]
  noMatch: boolean
}

const NO_RESULTS: TorrentResults = { entries: [], noMatch: false }

export interface ListView {
  list: number
  sidebarFilters: TorrentFilters
  columnFilters: ColumnFilter[]
  phoneLayout: boolean
  unifiedInstanceIds: number[]
}

export function listSearchScope({ list, sidebarFilters, columnFilters, phoneLayout, unifiedInstanceIds }: ListView) {
  const columnExpr = phoneLayout ? null : columnFiltersToExpr(columnFilters)
  const filters = { ...sidebarFilters, expr: combineFilterExpr(columnExpr, sidebarFilters.expr) }
  const crossSeeds = isCrossSeedExpr(sidebarFilters.expr)
  let instanceIds: number[] | undefined
  if (list === ALL_INSTANCES_ID) instanceIds = unifiedInstanceIds
  else if (!crossSeeds) instanceIds = [list]
  return { filters, filtered: hasSidebarFilters(filters), instanceIds, groupCopies: !crossSeeds }
}

export function useTorrentResults(query: string, close: () => void): TorrentResults {
  const { t } = useTranslation("common")
  const { t: tTorrents } = useTranslation("torrents")
  const navigate = useNavigate()
  const location = useLocation()
  const { instances } = useInstances()
  const { state: layoutRoute } = useLayoutRoute()
  const [incognito] = useIncognitoMode()
  const [unifiedPick] = usePersistedUnifiedInstanceFilter()
  const { data: trackerCustomizations } = useTrackerCustomizations()

  const activeInstances = (instances ?? []).filter(instance => instance.isActive)
  const openList = layoutRoute.showInstanceControls ? layoutRoute.instanceId : null
  const list = openList ?? (activeInstances.length === 1 ? activeInstances[0].id : ALL_INSTANCES_ID)
  const [sidebarFilters] = usePersistedFilters(list)
  const [columnFilters] = usePersistedColumnFilters(list)
  const phoneLayout = useIsMobile()
  const { filters, filtered, instanceIds, groupCopies } = listSearchScope({
    list,
    sidebarFilters,
    columnFilters,
    phoneLayout,
    unifiedInstanceIds: normalizeUnifiedInstanceIds(unifiedPick, activeInstances.map(instance => instance.id)),
  })

  const debouncedQuery = useDebounce(query, 200)
  const searches = query.length >= MIN_QUERY_LENGTH && debouncedQuery.length >= MIN_QUERY_LENGTH
  const search = useQuery({
    queryKey: ["command-palette-torrents", debouncedQuery, instanceIds, filters, groupCopies],
    queryFn: ({ signal }) => api.getCrossInstanceTorrents({ search: debouncedQuery, limit: RESULT_LIMIT, sort: "added_on", order: "desc", filters, groupCopies, instanceIds }, signal),
    enabled: searches,
    placeholderData: keepPreviousData,
    staleTime: 10_000,
    refetchInterval: ({ state }) => (state.data?.partialResults ? PARTIAL_RETRY_MS : false),
  })
  const found = searches ? search.data : undefined
  const rows = found?.crossInstanceTorrents ?? NO_TORRENTS
  const { describeRow } = useCopyGroupRows({ rows, expansion: COLLAPSED_COPY_GROUPS, selection: EMPTY_SELECTION, getSelectionIdentity: torrentKey })
  if (!query) return NO_RESULTS

  const settled = debouncedQuery === query && !search.isPlaceholderData
  const pathname = location.pathname
  const routeSearch = location.search as Record<string, unknown>
  const onUnifiedList = pathname === "/instances" || pathname === "/instances/"
  const severalInstances = activeInstances.length > 1
  const go = (action: () => void) => () => {
    close()
    action()
  }

  let matches: string | undefined
  let noMatch = false
  if (searches && search.isError) matches = t("commandPalette.searchFailed")
  else if (searches && (found === undefined || !settled)) matches = t("commandPalette.searching")
  else if (found?.partialResults && found.total === 0) matches = t("commandPalette.searchFailed")
  else if (found !== undefined) {
    noMatch = found.total === 0
    matches = noMatch ? t("commandPalette.noTorrentMatches") : t("commandPalette.torrentMatches", { count: found.total })
  }

  const filterList: PaletteEntry = {
    id: "torrent-search",
    label: t("commandPalette.searchTorrentsFor", { query }),
    icon: TextSearch,
    trail: severalInstances ? (activeInstances.find(instance => instance.id === list)?.name ?? t("unifiedScope.unified")) : undefined,
    note: [matches, filtered && t("commandPalette.listFiltersApply")].filter(Boolean).join(" · ") || undefined,
    shortcut: "Enter",
    run: go(() => {
      if (openList !== null) navigateWithSearch({ navigate, search: { ...routeSearch, q: query }, replace: true })
      else navigateWithSearch({ navigate, to: list === ALL_INSTANCES_ID ? "/instances" : `/instances/${list}`, search: { q: query } })
    }),
  }

  const trackerNames = buildTrackerCustomizationLookup(trackerCustomizations)
  const torrents: PaletteEntry[] = buildVisibleRows(rows, COLLAPSED_COPY_GROUPS).map((torrent) => {
    const listPath = `/instances/${torrent.instanceId}`
    const staysOnList = onUnifiedList || pathname === listPath
    const tracker = resolveTrackerDisplay(incognito ? getLinuxTrackerDomain(torrent.hash) : extractTrackerHost(torrent.tracker), trackerNames).displayName
    return {
      id: `torrent.${torrentKey(torrent)}`,
      label: incognito ? getLinuxIsoName(torrent.hash) : torrent.name,
      icon: ArrowDownUp,
      trail: severalInstances && (list === ALL_INSTANCES_ID || !instanceIds) ? torrent.instanceName : undefined,
      note: [getStateLabel(torrent.state, tTorrents), formatBytes(torrent.size), tracker].filter(Boolean).join(" · "),
      copies: describeRow(torrent).copies,
      run: go(() => navigateWithSearch({
        navigate,
        to: staysOnList ? undefined : listPath,
        search: {
          ...(staysOnList ? routeSearch : {}),
          q: query,
          torrent: torrent.hash,
          tab: "general",
          ...(onUnifiedList ? { instance: torrent.instanceId } : {}),
        },
      })),
    }
  })

  return { entries: [filterList, ...torrents], noMatch }
}
