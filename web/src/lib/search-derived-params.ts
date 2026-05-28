/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TFunction } from "i18next"

// Shared helpers for deriving Torznab parameters from UI selections
// Mirrors backend category groupings in internal/services/jackett.

export type SearchType = "auto" | "movies" | "tv" | "music" | "books" | "apps" | "xxx"

export type SearchTypeOption = {
  value: SearchType
  label: string
  description?: string
}

type NonAutoSearchType = Exclude<SearchType, "auto">

const SEARCH_TYPE_CATEGORY_MAP: Record<NonAutoSearchType, number[]> = {
  movies: [2000, 2010, 2020, 2030, 2040, 2045, 2050, 2060, 2070, 2080],
  tv: [5000, 5010, 5020, 5030, 5040, 5045, 5070, 5080],
  music: [3000],
  books: [7000, 7020, 7030],
  apps: [4000],
  xxx: [6000, 6010, 6020, 6030, 6040, 6050, 6060, 6070],
}

const PARENT_CATEGORY_TO_TYPE: Record<number, NonAutoSearchType> = {
  2000: "movies",
  3000: "music",
  4000: "apps",
  5000: "tv",
  6000: "xxx",
  7000: "books",
}

const SEARCH_TYPE_KEYS: Record<SearchType, { label: string; description?: string }> = {
  auto: {
    label: "searchTypes.auto.label",
    description: "searchTypes.auto.description",
  },
  movies: {
    label: "searchTypes.movies.label",
  },
  tv: {
    label: "searchTypes.tv.label",
  },
  music: {
    label: "searchTypes.music.label",
  },
  books: {
    label: "searchTypes.books.label",
  },
  apps: {
    label: "searchTypes.apps.label",
  },
  xxx: {
    label: "searchTypes.xxx.label",
  },
}

export function getSearchTypeOptions(t: TFunction): SearchTypeOption[] {
  return (Object.keys(SEARCH_TYPE_KEYS) as SearchType[]).map((value) => ({
    value,
    label: t(SEARCH_TYPE_KEYS[value].label),
    description: SEARCH_TYPE_KEYS[value].description ? t(SEARCH_TYPE_KEYS[value].description) : undefined,
  }))
}

export function getCategoriesForSearchType(type: SearchType): number[] | undefined {
  if (type === "auto") {
    return undefined
  }

  return [...SEARCH_TYPE_CATEGORY_MAP[type]]
}

export function inferSearchTypeFromCategories(categories?: number[]): SearchType | null {
  if (!categories || categories.length === 0) {
    return null
  }

  const parentCategoryType = (category: number): NonAutoSearchType | null => {
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
  const key = SEARCH_TYPE_KEYS[type]
  return key ? t(key.label) : t("searchTypes.auto.label")
}
