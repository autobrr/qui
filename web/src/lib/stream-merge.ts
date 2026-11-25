/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { isTopRow } from "@/lib/copy-groups"
import type { Torrent } from "@/types"

export function mergeStreamedFirstPage<T extends { hash: string } & Pick<Torrent, "copy_group">>(
  prev: T[],
  nextTorrents: T[],
  total?: number,
  keyOf: (torrent: T) => string = torrent => torrent.hash
): T[] {
  if (nextTorrents.length === 0) {
    return []
  }

  if (prev.length === 0) {
    return nextTorrents
  }

  if (typeof total === "number" && nextTorrents.length >= total) {
    return nextTorrents
  }

  const seen = new Set(nextTorrents.map(keyOf))

  const trailing = prev.slice(windowEnd(prev, nextTorrents.filter(isTopRow).length)).filter(torrent => !seen.has(keyOf(torrent)))

  return [...nextTorrents, ...trailing]
}

function windowEnd(rows: Pick<Torrent, "copy_group">[], topRows: number): number {
  const groups = new Set<string>()
  let seen = 0
  for (let index = 0; index < rows.length; index++) {
    const id = rows[index].copy_group?.id
    if (id !== undefined) {
      if (groups.has(id)) continue
      groups.add(id)
    }
    if (seen++ === topRows) {
      return index
    }
  }
  return rows.length
}

export function applyOrderedDelta<T>(
  prevRows: T[],
  changedRows: T[],
  order: string[] | undefined,
  keyOf: (row: T) => string
): { rows: T[]; changed: boolean } {
  if (!order && changedRows.length === 0) {
    return { rows: prevRows, changed: false }
  }

  const byKey = new Map<string, T>()
  for (const row of prevRows) {
    byKey.set(keyOf(row), row)
  }
  for (const row of changedRows) {
    byKey.set(keyOf(row), row)
  }

  const keys = order ?? prevRows.map(keyOf)

  const rows: T[] = []
  for (const key of keys) {
    const row = byKey.get(key)
    if (row !== undefined) {
      rows.push(row)
    }
  }

  return { rows, changed: true }
}
