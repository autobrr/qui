/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { useCallback, useMemo } from "react"

import { api } from "@/lib/api"
import type { SitePick, TorznabIndexer } from "@/types"

export type SitePickTrigger = "manual" | "completion" | "library" | "rss"

type RedOpsSource = "torrent" | "settings" | "none"

const TRIGGER_RED_OPS = {
  manual: "torrent",
  completion: "settings",
  library: "settings",
  rss: "none",
} as const satisfies Record<SitePickTrigger, RedOpsSource>

export const TORZNAB_INDEXERS_KEY = ["torznab", "indexers"] as const
export const GAZELLE_SITES_KEY = ["cross-seed", "gazelle-sites"] as const

export interface RedOpsSite {
  site: string
  name: string
  indexerId: number
  active?: boolean
  replacedIndexerIds?: number[]
}

export interface SiteOption {
  label: string
  value: string
  id: number
}

const EVERY_SITE: SitePick = { indexerIds: [], gazelleSites: [] }

export function enabledTorznabIndexers(indexers: readonly TorznabIndexer[]): TorznabIndexer[] {
  return indexers
    .filter(indexer => indexer.enabled)
    .sort((a, b) => b.priority - a.priority || a.name.localeCompare(b.name))
}

export function replacedIndexerIds(redOps: readonly RedOpsSite[]): Set<number> {
  return new Set(redOps.flatMap(site => site.replacedIndexerIds ?? []))
}

export function sitePickOptions(
  trigger: SitePickTrigger,
  indexers: readonly TorznabIndexer[],
  redOps: readonly RedOpsSite[],
  { pick = EVERY_SITE, torznabOn = true }: { pick?: SitePick; torznabOn?: boolean } = {}
): SiteOption[] {
  const sites = TRIGGER_RED_OPS[trigger] === "none" ? [] : redOps.filter(site => site.active !== false || pick.gazelleSites.includes(site.site))
  const replaced = replacedIndexerIds(sites)
  const torznab = torznabOn ? enabledTorznabIndexers(indexers).filter(indexer => !replaced.has(indexer.id)) : []
  return [
    ...torznab.map(indexer => ({ label: indexer.name, value: String(indexer.id), id: indexer.id })),
    ...sites.map(site => ({ label: site.name, value: String(site.indexerId), id: site.indexerId })),
  ]
}

export function sitePickFromIds(ids: readonly number[], sites: readonly RedOpsSite[]): SitePick {
  const picked = new Set(ids)
  return {
    indexerIds: [...picked].filter(id => id > 0),
    gazelleSites: sites.filter(site => picked.has(site.indexerId)).map(site => site.site),
  }
}

function sitePickToIds(pick: SitePick, sites: readonly RedOpsSite[]): number[] {
  const replacedBy = new Map(sites.flatMap(site => (site.replacedIndexerIds ?? []).map(id => [id, site.indexerId] as const)))
  return [...new Set([
    ...pick.indexerIds.map(id => replacedBy.get(id) ?? id),
    ...sites.filter(site => pick.gazelleSites.includes(site.site)).map(site => site.indexerId),
  ])]
}

export function sitePickSelected(pick: SitePick, options: readonly SiteOption[], sites: readonly RedOpsSite[]): string[] {
  const shown = new Set(options.map(option => option.id))
  return sitePickToIds(pick, sites).filter(id => shown.has(id)).map(String)
}

export function sitePickChange(
  pick: SitePick,
  values: readonly string[],
  options: readonly SiteOption[],
  sites: readonly RedOpsSite[]
): SitePick {
  const shown = new Set(options.map(option => option.id))
  const kept = sitePickToIds(pick, sites).filter(id => !shown.has(id))
  const next = sitePickFromIds([...kept, ...values.map(Number).filter(Number.isInteger)], sites)
  const known = new Set(sites.map(site => site.site))
  return { ...next, gazelleSites: [...next.gazelleSites, ...pick.gazelleSites.filter(host => !known.has(host))] }
}

export function sitePickForSave(pick: SitePick, options: readonly SiteOption[], sites: readonly RedOpsSite[]): SitePick {
  return sitePickChange(pick, sitePickSelected(pick, options, sites), options, sites)
}

export function useSitePickSources(trigger: SitePickTrigger) {
  const indexersQuery = useQuery({
    queryKey: TORZNAB_INDEXERS_KEY,
    queryFn: () => api.listTorznabIndexers(),
    staleTime: 5 * 60 * 1000,
  })
  const sitesQuery = useQuery({
    queryKey: GAZELLE_SITES_KEY,
    queryFn: () => api.getCrossSeedGazelleSites(),
    enabled: TRIGGER_RED_OPS[trigger] === "settings",
    staleTime: 60 * 1000,
  })
  const indexers = useMemo(() => enabledTorznabIndexers(indexersQuery.data ?? []), [indexersQuery.data])
  const sites = useMemo(() => sitesQuery.data ?? [], [sitesQuery.data])
  return {
    indexers,
    sites,
    isPending: indexersQuery.isPending || sitesQuery.isLoading,
    isError: indexersQuery.isError,
  }
}

export function useSitePickOptions(trigger: SitePickTrigger, { torznabOn = true }: { torznabOn?: boolean } = {}) {
  const sources = useSitePickSources(trigger)
  const { indexers, sites } = sources
  const optionsFor = useCallback(
    (pick: SitePick) => sitePickOptions(trigger, indexers, sites, { pick, torznabOn }),
    [trigger, indexers, sites, torznabOn]
  )
  const options = useMemo(() => optionsFor(EVERY_SITE), [optionsFor])
  return { ...sources, options, optionsFor }
}
