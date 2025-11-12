/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMemo } from "react"

/**
 * Hook to partition items into non-empty and empty based on their counts
 * Used for filtering items in the sidebar based on whether they have associated data
 */
export function useItemPartition<T>(
  items: T[],
  hasData: boolean,
  getCountKey: (item: T) => string,
  getRawCount: (key: string) => number
) {
  return useMemo(() => {
    if (!hasData) {
      return {
        nonEmpty: items,
        empty: [],
      }
    }

    const nonEmpty: T[] = []
    const empty: T[] = []

    items.forEach((item) => {
      const key = getCountKey(item)
      if (getRawCount(key) > 0) {
        nonEmpty.push(item)
      } else {
        empty.push(item)
      }
    })

    return { nonEmpty, empty }
  }, [items, hasData, getCountKey, getRawCount])
}
