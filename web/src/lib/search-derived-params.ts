/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Shared helpers for deriving Torznab parameters from UI selections
// Mirrors backend category groupings in internal/services/jackett.

export type SearchType = 'auto' | 'movies' | 'tv' | 'music' | 'books' | 'apps' | 'xxx'

export type SearchTypeOption = {
  value: SearchType
  label: string
  description?: string
}

type NonAutoSearchType = Exclude<SearchType, 'auto'>

const SEARCH_TYPE_CATEGORY_MAP: Record<NonAutoSearchType, number[]> = {
  movies: [2000, 2030, 2040, 2045],
  tv: [5000, 5030, 5040, 5045],
  music: [3000],
  books: [7000, 7020, 7030],
  apps: [4000],
  xxx: [6000, 6010, 6020, 6030, 6040, 6050, 6060, 6070]
}

const PARENT_CATEGORY_TO_TYPE: Record<number, NonAutoSearchType> = {
  2000: 'movies',
  3000: 'music',
  4000: 'apps',
  5000: 'tv',
  6000: 'xxx',
  7000: 'books'
}

export const SEARCH_TYPE_OPTIONS: SearchTypeOption[] = [
  { value: 'auto', label: 'Auto detect', description: 'Automatically infer the right categories' },
  { value: 'movies', label: 'Movies' },
  { value: 'tv', label: 'TV' },
  { value: 'music', label: 'Music' },
  { value: 'books', label: 'Books & comics' },
  { value: 'apps', label: 'Apps & games' },
  { value: 'xxx', label: 'Adult' }
]

export function getCategoriesForSearchType(type: SearchType): number[] | undefined {
  if (type === 'auto') {
    return undefined
  }

  // Return a new array so callers can mutate safely.
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

export function getSearchTypeLabel(type: SearchType): string {
  const match = SEARCH_TYPE_OPTIONS.find((option) => option.value === type)
  return match?.label ?? 'Auto detect'
}
