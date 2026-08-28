/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TFunction } from "i18next"

// Shared helpers for deriving Torznab parameters from UI selections
// Mirrors backend category groupings in internal/services/jackett.

export type SearchType = "movies" | "tv" | "music" | "books" | "apps" | "xxx"

export type SearchTypeOption = {
  value: SearchType
  label: string
}

const SEARCH_TYPE_CATEGORY_MAP: Record<SearchType, number[]> = {
  movies: [2000, 2010, 2020, 2030, 2040, 2045, 2050, 2060, 2070, 2080],
  tv: [5000, 5010, 5020, 5030, 5040, 5045, 5070, 5080],
  music: [3000],
  books: [7000, 7020, 7030],
  apps: [4000],
  xxx: [6000, 6010, 6020, 6030, 6040, 6050, 6060, 6070],
}

const PARENT_CATEGORY_TO_TYPE: Record<number, SearchType> = {
  2000: "movies",
  3000: "music",
  4000: "apps",
  5000: "tv",
  6000: "xxx",
  7000: "books",
}

const SEARCH_TYPE_KEYS: Record<SearchType, string> = {
  movies: "searchTypes.movies.label",
  tv: "searchTypes.tv.label",
  music: "searchTypes.music.label",
  books: "searchTypes.books.label",
  apps: "searchTypes.apps.label",
  xxx: "searchTypes.xxx.label",
}

export function getSearchTypeOptions(t: TFunction): SearchTypeOption[] {
  return (Object.keys(SEARCH_TYPE_KEYS) as SearchType[]).map((value) => ({
    value,
    label: t(SEARCH_TYPE_KEYS[value]),
  }))
}

export function getCategoriesForSearchType(type: SearchType): number[] {
  return [...SEARCH_TYPE_CATEGORY_MAP[type]]
}

export function inferSearchTypeFromCategories(categories?: number[]): SearchType | null {
  if (!categories || categories.length === 0) {
    return null
  }

  const parentCategoryType = (category: number): SearchType | null => {
    const parent = Math.floor(category / 1000) * 1000
    return PARENT_CATEGORY_TO_TYPE[parent] ?? null
  }

  const firstType = parentCategoryType(categories[0])
  if (!firstType) {
    return null
  }

  const allSameFamily = categories.every((category) => parentCategoryType(category) === firstType)
  return allSameFamily ? firstType : null
}

export function getSearchTypeLabel(type: SearchType, t: TFunction): string {
  return t(SEARCH_TYPE_KEYS[type])
}

/**
 * Filter a recent search's saved indexer ids down to the ones still enabled.
 * Returns null when nothing usable remains, so callers keep their current selection.
 */
export function resolveSuggestionIndexerIds(savedIds: number[] | null | undefined, enabledIds: number[]): Set<number> | null {
  const enabled = new Set(enabledIds)
  const filtered = savedIds?.filter(id => enabled.has(id)) ?? []
  return filtered.length ? new Set(filtered) : null
}
