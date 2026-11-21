/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useClientSetting } from "@/lib/client-settings"
import type { SearchType } from "@/lib/search-derived-params"

const SEARCH_TYPES: readonly SearchType[] = ["movies", "tv", "music", "books", "apps", "xxx"]

const TYPE_SETTING = {
  defaultValue: "movies" as SearchType,
  parse: (raw: string): SearchType => {
    const value = JSON.parse(raw)
    return SEARCH_TYPES.includes(value) ? value : "movies"
  },
}

export function useSearchType(): [SearchType, (type: SearchType) => void] {
  return useClientSetting<SearchType>("qui-search-type", TYPE_SETTING)
}
