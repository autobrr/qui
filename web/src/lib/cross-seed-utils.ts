/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { LocalCrossSeedMatch, Torrent } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { useMemo } from "react"

// Cross-seed matching utilities
export const normalizePath = (path: string) => path?.toLowerCase().replace(/[\\/]+/g, "/").replace(/\/$/, "") || ""

/**
 * Check if a path is inside a base directory.
 * Returns true if base is non-empty and path equals base or starts with base + "/".
 */
export const isInsideBase = (path: string, base: string): boolean => {
  if (!base) return false
  return path === base || path.startsWith(base + "/")
}

/**
 * Check if a torrent is hardlink-managed based on instance config and paths.
 * Only returns true if the instance has hardlink mode enabled AND has local access.
 */
export const isHardlinkManaged = (
  match: { save_path?: string; content_path?: string },
  instance: { useHardlinks?: boolean; hasLocalFilesystemAccess?: boolean; hardlinkBaseDir?: string } | undefined
): boolean => {
  if (!instance?.useHardlinks || !instance?.hasLocalFilesystemAccess) return false
  const base = normalizePath(instance.hardlinkBaseDir || "")
  if (!base) return false
  const savePath = normalizePath(match.save_path || "")
  const contentPath = normalizePath(match.content_path || "")
  return isInsideBase(savePath, base) || isInsideBase(contentPath, base)
}

// Extended torrent type with cross-seed metadata
export interface CrossSeedTorrent extends Torrent {
  instanceId: number
  instanceName: string
  matchType: "content_path" | "name" | "release"
}

/**
 * Hook to find cross-seed matches using the backend API.
 * Uses proper release metadata parsing (rls library) instead of frontend fuzzy matching.
 */
export const useLocalCrossSeedMatches = (
  instanceId: number,
  torrent: Torrent | null,
  enabled: boolean = true
) => {
  // Fetch all instances for the instance lookup map
  const { data: allInstances, isLoading: isLoadingInstances } = useQuery({
    queryKey: ["instances"],
    queryFn: api.getInstances,
    enabled,
    staleTime: 60000,
  })

  // Call the backend API to get local matches
  const { data: matches, isLoading: isLoadingMatches } = useQuery({
    queryKey: ["cross-seed-local-matches", instanceId, torrent?.hash],
    queryFn: () => api.getLocalCrossSeedMatches(instanceId, torrent!.hash),
    enabled: enabled && !!torrent && instanceId > 0,
    staleTime: 60000,
    gcTime: 5 * 60 * 1000,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })

  // Convert LocalCrossSeedMatch to CrossSeedTorrent format for compatibility
  const matchingTorrents = useMemo((): CrossSeedTorrent[] => {
    if (!matches) return []
    return matches.map((m): CrossSeedTorrent => toCompatibleMatch(m))
  }, [matches])

  return {
    matchingTorrents,
    isLoadingMatches: isLoadingInstances || isLoadingMatches,
    isLoadingInstances,
    pendingQueryCount: isLoadingMatches ? 1 : 0,
    allInstances: allInstances || [],
  }
}

/**
 * Convert LocalCrossSeedMatch to CrossSeedTorrent for backward compatibility.
 */
export function toCompatibleMatch(m: LocalCrossSeedMatch): CrossSeedTorrent {
  return {
    hash: m.hash,
    name: m.name,
    size: m.size,
    progress: m.progress,
    save_path: m.savePath,
    content_path: m.contentPath,
    category: m.category,
    tags: m.tags,
    state: m.state,
    tracker: m.tracker,
    tracker_health: m.trackerHealth as "unregistered" | "tracker_down" | undefined,
    instanceId: m.instanceId,
    instanceName: m.instanceName,
    matchType: m.matchType,
    // Default values for required Torrent fields
    added_on: 0,
    completion_on: 0,
    dlspeed: 0,
    downloaded: 0,
    downloaded_session: 0,
    eta: 0,
    num_leechs: 0,
    num_seeds: 0,
    priority: 0,
    seq_dl: false,
    f_l_piece_prio: false,
    super_seeding: false,
    force_start: false,
    auto_tmm: false,
    seen_complete: 0,
    time_active: 0,
    num_complete: 0,
    num_incomplete: 0,
    amount_left: 0,
    completed: 0,
    last_activity: 0,
    magnet_uri: "",
    availability: 0,
    dl_limit: 0,
    download_path: "",
    infohash_v1: "",
    infohash_v2: "",
    popularity: 0,
    private: false,
    max_ratio: 0,
    max_seeding_time: 0,
    seeding_time: 0,
    ratio: 0,
    ratio_limit: 0,
    reannounce: 0,
    seeding_time_limit: 0,
    total_size: m.size,
    trackers_count: 0,
    up_limit: 0,
    uploaded: 0,
    uploaded_session: 0,
    upspeed: 0,
  }
}
