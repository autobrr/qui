/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMemo } from "react"

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
