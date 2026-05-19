/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback } from "react"
import { toast } from "sonner"

import { api } from "@/lib/api"
import type { TorrentActionTarget } from "@/lib/torrent-action-targets"

function uniqueInfoHashes(hashes: string[]): string[] {
  const seen = new Set<string>()
  const unique: string[] = []

  for (const hash of hashes) {
    const normalized = hash.trim()
    if (!normalized) {
      continue
    }

    const dedupeKey = normalized.toLowerCase()
    if (seen.has(dedupeKey)) {
      continue
    }

    seen.add(dedupeKey)
    unique.push(normalized)
  }

  return unique
}

function resolveBlocklistTargets(
  instanceId: number,
  infoHashes: string[],
  explicitTargets?: TorrentActionTarget[]
): TorrentActionTarget[] {
  const hashSet = new Set(infoHashes.map(hash => hash.toLowerCase()))
  const seen = new Set<string>()
  const targets: TorrentActionTarget[] = []

  if (explicitTargets && explicitTargets.length > 0) {
    for (const target of explicitTargets) {
      const hash = target.hash?.trim()
      if (!hash || target.instanceId <= 0) {
        continue
      }

      if (!hashSet.has(hash.toLowerCase())) {
        continue
      }

      const dedupeKey = `${target.instanceId}:${hash.toLowerCase()}`
      if (seen.has(dedupeKey)) {
        continue
      }

      seen.add(dedupeKey)
      targets.push({ instanceId: target.instanceId, hash })
    }
  }

  if (targets.length > 0) {
    return targets
  }

  if (instanceId <= 0) {
    return []
  }

  return infoHashes.map(hash => ({ instanceId, hash }))
}

export function useCrossSeedBlocklistActions(instanceId: number) {
  const blockCrossSeedHashes = useCallback(async (hashes: string[], targets?: TorrentActionTarget[]) => {
    if (hashes.length === 0) return

    const uniqueHashes = uniqueInfoHashes(hashes)
    if (uniqueHashes.length === 0) return

    const resolvedTargets = resolveBlocklistTargets(instanceId, uniqueHashes, targets)
    if (resolvedTargets.length === 0) {
      toast.error("Unable to block cross-seed torrents for this selection")
      return
    }

    const results = await Promise.allSettled(
      resolvedTargets.map((target) => api.addCrossSeedBlocklist({ instanceId: target.instanceId, infoHash: target.hash }))
    )

    const failed = results.filter((result) => result.status === "rejected").length
    if (failed > 0) {
      toast.error(`Failed to block ${failed} of ${resolvedTargets.length} cross-seed torrents`)
      return
    }

    toast.success(`Blocked ${resolvedTargets.length} cross-seed ${resolvedTargets.length === 1 ? "torrent" : "torrents"}`)
  }, [instanceId])

  return { blockCrossSeedHashes } as const
}
