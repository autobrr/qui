/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getCategoriesForSearchType, type SearchType } from "@/lib/search-derived-params"
import { extractImdbId, extractTvdbId } from "@/lib/search-id-parsing"
import type { TorznabSearchRequest, TorznabSearchResult } from "@/types"

export const ADVANCED_FIELDS = ["imdbId", "tvdbId", "season", "episode", "year", "artist", "album", "limit", "offset"] as const
export type AdvancedField = (typeof ADVANCED_FIELDS)[number]
export type AdvancedParams = Partial<Record<AdvancedField, string>>

export const NUMERIC_FIELDS: ReadonlySet<AdvancedField> = new Set(["season", "episode", "year", "limit", "offset"])

const FIELDS_BY_TYPE: Record<SearchType, AdvancedField[]> = {
  movies: ["imdbId", "year", "limit", "offset"],
  tv: ["tvdbId", "imdbId", "season", "episode", "limit", "offset"],
  music: ["artist", "album", "year", "limit", "offset"],
  books: ["year", "limit", "offset"],
  apps: ["limit", "offset"],
  xxx: ["limit", "offset"],
}

export function advancedFieldsFor(type: SearchType): AdvancedField[] {
  return FIELDS_BY_TYPE[type]
}

export function activeAdvancedFields(type: SearchType, params: AdvancedParams): AdvancedField[] {
  return FIELDS_BY_TYPE[type].filter(field => (params[field] ?? "").trim() !== "")
}

export interface SubmittedSearch {
  query: string
  type: SearchType
  indexerIds: number[]
  params: AdvancedParams
  bypassCache?: boolean
}

function numberParam(value: string | undefined, min: number): number | undefined {
  const trimmed = value?.trim()
  if (!trimmed) return undefined
  const parsed = Number(trimmed)
  return Number.isSafeInteger(parsed) && parsed >= min ? parsed : undefined
}

export function buildSearchRequest(search: SubmittedSearch): TorznabSearchRequest {
  const fields = new Set(FIELDS_BY_TYPE[search.type])
  const param = (field: AdvancedField) => fields.has(field) ? search.params[field]?.trim() || undefined : undefined
  const query = search.query.trim()
  const request: TorznabSearchRequest = {
    query,
    categories: getCategoriesForSearchType(search.type),
    indexer_ids: search.indexerIds,
    imdb_id: param("imdbId") ?? extractImdbId(query) ?? undefined,
    tvdb_id: param("tvdbId") ?? extractTvdbId(query) ?? undefined,
    artist: param("artist"),
    album: param("album"),
    year: numberParam(param("year"), 0),
    season: numberParam(param("season"), 0),
    episode: numberParam(param("episode"), 0),
    limit: numberParam(param("limit"), 1),
    offset: numberParam(param("offset"), 0),
    cache_mode: search.bypassCache ? "bypass" : undefined,
  }
  return Object.fromEntries(Object.entries(request).filter(([, value]) => value !== undefined)) as TorznabSearchRequest
}

export function resultKey(result: TorznabSearchResult): string {
  return `${result.indexerId}-${result.guid.trim() || result.downloadUrl}`
}

export function tickResult(picked: ReadonlySet<string>, keys: readonly string[], key: string, anchor: string | null): Set<string> {
  const on = !picked.has(key)
  const next = new Set(picked)
  const index = keys.indexOf(key)
  if (index === -1) return next
  const last = anchor === null ? -1 : keys.indexOf(anchor)
  const from = last === -1 ? index : Math.min(last, index)
  const to = last === -1 ? index : Math.max(last, index)
  for (const each of keys.slice(from, to + 1)) {
    if (on) next.add(each)
    else next.delete(each)
  }
  return next
}

export const FACETS = ["indexer", "category", "source", "collection", "group", "freeleech"] as const
export type FacetKey = (typeof FACETS)[number]
export type FacetSelection = Partial<Record<FacetKey, string[]>>
export type CategoryName = (result: TorznabSearchResult) => string

export function freeleechValue(result: TorznabSearchResult): string {
  return String(Math.min(1, Math.max(0, result.downloadVolumeFactor)))
}

export function facetValue(result: TorznabSearchResult, facet: FacetKey, categoryName: CategoryName): string | undefined {
  switch (facet) {
    case "indexer":
      return result.indexer
    case "category":
      return categoryName(result)
    case "freeleech":
      return freeleechValue(result)
    default:
      return result[facet] || undefined
  }
}

export interface FacetOption {
  value: string
  count: number
}

export function facetOptions(results: TorznabSearchResult[], facet: FacetKey, categoryName: CategoryName): FacetOption[] {
  const counts = new Map<string, number>()
  for (const result of results) {
    const value = facetValue(result, facet, categoryName)
    if (value !== undefined) counts.set(value, (counts.get(value) ?? 0) + 1)
  }
  const options = Array.from(counts, ([value, count]) => ({ value, count }))
  if (facet === "freeleech") return options.sort((a, b) => Number(a.value) - Number(b.value))
  return options.sort((a, b) => b.count - a.count || a.value.localeCompare(b.value))
}

export function filterResults(results: TorznabSearchResult[], text: string, facets: FacetSelection, categoryName: CategoryName): TorznabSearchResult[] {
  const needles = text.trim().toLowerCase().split(/\s+/).filter(Boolean)
  const active = FACETS.filter(facet => (facets[facet]?.length ?? 0) > 0).map(facet => [facet, new Set(facets[facet])] as const)
  if (needles.length === 0 && active.length === 0) return results
  return results.filter(result => {
    for (const [facet, values] of active) {
      const value = facetValue(result, facet, categoryName)
      if (value === undefined || !values.has(value)) return false
    }
    if (needles.length === 0) return true
    const haystack = `${result.title} ${result.indexer} ${categoryName(result)} ${result.group ?? ""}`.toLowerCase()
    return needles.every(needle => haystack.includes(needle))
  })
}

export const SORT_KEYS = ["seeders", "published", "size", "title", "indexer"] as const
export type SortKey = (typeof SORT_KEYS)[number]
export interface ResultSort {
  key: SortKey
  desc: boolean
}

export const DEFAULT_SORT: ResultSort = { key: "seeders", desc: true }

const collator = new Intl.Collator(undefined, { sensitivity: "base", numeric: true })

function compare(a: TorznabSearchResult, b: TorznabSearchResult, key: SortKey): number {
  switch (key) {
    case "seeders":
      return a.seeders - b.seeders
    case "size":
      return a.size - b.size
    case "published":
      return (Date.parse(a.publishDate) || 0) - (Date.parse(b.publishDate) || 0)
    case "title":
      return collator.compare(a.title, b.title)
    case "indexer":
      return collator.compare(a.indexer, b.indexer)
  }
}

export function sortResults(results: TorznabSearchResult[], sort: ResultSort): TorznabSearchResult[] {
  const direction = sort.desc ? -1 : 1
  return [...results].sort((a, b) => direction * compare(a, b, sort.key) || b.seeders - a.seeders)
}
