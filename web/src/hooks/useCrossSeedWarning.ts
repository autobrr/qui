/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { CrossSeedTorrent } from "@/lib/cross-seed-utils"
import type { Torrent } from "@/types"

interface UseCrossSeedWarningOptions {
  instanceId: number
  instanceName: string
  torrents: Torrent[]
  enabled: boolean
}

export interface CrossSeedWarningResult {
  /** Cross-seed torrents on this instance that share files with torrents being deleted */
  affectedTorrents: CrossSeedTorrent[]
  /** Whether we're still searching for cross-seeds */
  isLoading: boolean
  /** Whether there are cross-seeds that would be affected */
  hasWarning: boolean
}

/**
 * Hook to detect cross-seeded torrents on the current instance that would be
 * affected when deleting files.
 *
 * Only checks the current instance since that's where the files live.
 * Cross-instance cross-seeds are handled by the Filter Cross-Seeds feature
 * and the Cross-Seed tab in torrent details.
 */
export function useCrossSeedWarning({
  instanceId,
  instanceName,
  torrents,
  enabled,
}: UseCrossSeedWarningOptions): CrossSeedWarningResult {
  // Build the expression filter for content_path matching
  const { expr, hashesBeingDeleted } = useMemo(() => {
    if (!enabled || torrents.length === 0) {
      return { expr: "", hashesBeingDeleted: new Set<string>() }
    }

    const hashes = new Set<string>()
    const paths = new Set<string>()

    for (const t of torrents) {
      hashes.add(t.hash)
      if (t.content_path) {
        paths.add(t.content_path)
      }
    }

    if (paths.size === 0) {
      return { expr: "", hashesBeingDeleted: hashes }
    }

    // Build expression: ContentPath == "path1" || ContentPath == "path2"
    // Escape backslashes first (for Windows paths), then quotes
    const conditions = Array.from(paths).map(
      (p) => `ContentPath == "${p.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`
    )
    const expression = conditions.join(" || ")

    return { expr: expression, hashesBeingDeleted: hashes }
  }, [enabled, torrents])

  // Query for torrents matching the content paths
  const { data, isLoading } = useQuery({
    queryKey: ["cross-seed-warning", instanceId, expr],
    queryFn: () =>
      api.getTorrents(instanceId, {
        filters: {
          status: [],
          excludeStatus: [],
          categories: [],
          excludeCategories: [],
          tags: [],
          excludeTags: [],
          trackers: [],
          excludeTrackers: [],
          expr,
        },
        limit: 100, // Reasonable limit for warning display
      }),
    enabled: enabled && expr.length > 0,
    staleTime: 10000, // 10 seconds - data might change
    gcTime: 30000,
  })

  // Filter out torrents being deleted and add matchType
  const result = useMemo((): CrossSeedWarningResult => {
    if (!enabled || expr.length === 0) {
      return {
        affectedTorrents: [],
        isLoading: false,
        hasWarning: false,
      }
    }

    if (isLoading || !data?.torrents) {
      return {
        affectedTorrents: [],
        isLoading: true,
        hasWarning: false,
      }
    }

    const affectedTorrents: CrossSeedTorrent[] = []

    for (const t of data.torrents) {
      // Skip torrents being deleted
      if (hashesBeingDeleted.has(t.hash)) {
        continue
      }

      affectedTorrents.push({
        ...t,
        instanceId,
        instanceName,
        matchType: "content_path",
      })
    }

    return {
      affectedTorrents,
      isLoading: false,
      hasWarning: affectedTorrents.length > 0,
    }
  }, [enabled, expr, isLoading, data, hashesBeingDeleted, instanceId, instanceName])

  return result
}
