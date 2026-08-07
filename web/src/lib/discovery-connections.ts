/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorznabIndexer } from "@/types"

export interface SavedDiscoveryConnection {
  baseUrl: string
  backend: "jackett" | "prowlarr"
  sourceIndexerId: number
  indexerCount: number
}

// Native indexers point at the tracker itself, not a Prowlarr/Jackett server,
// so they are not reusable discovery connections.
export function deriveSavedConnections(indexers: TorznabIndexer[]): SavedDiscoveryConnection[] {
  const groups = new Map<string, SavedDiscoveryConnection>()
  for (const indexer of indexers) {
    if (indexer.backend !== "jackett" && indexer.backend !== "prowlarr") continue
    const key = `${indexer.backend}|${indexer.base_url}`
    const existing = groups.get(key)
    if (!existing) {
      groups.set(key, {
        baseUrl: indexer.base_url,
        backend: indexer.backend,
        sourceIndexerId: indexer.id,
        indexerCount: 1,
      })
    } else {
      existing.indexerCount++
    }
  }
  return [...groups.values()].sort((a, b) => a.baseUrl.localeCompare(b.baseUrl))
}
