/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback } from "react"
import { toast } from "sonner"

import { api } from "@/lib/api"

function uniqueInfoHashes(hashes: string[]): string[] {
  return Array.from(new Set(hashes.filter(Boolean)))
}

export function useCrossSeedBlocklistActions(instanceId: number) {
  const blockCrossSeedHashes = useCallback(async (hashes: string[]) => {
    if (instanceId <= 0 || hashes.length === 0) return

    const uniqueHashes = uniqueInfoHashes(hashes)
    if (uniqueHashes.length === 0) return

    const results = await Promise.allSettled(
      uniqueHashes.map((infoHash) => api.addCrossSeedBlocklist({ instanceId, infoHash }))
    )

    const failed = results.filter((result) => result.status === "rejected").length
    if (failed > 0) {
      toast.error(`Failed to block ${failed} of ${uniqueHashes.length} cross-seed torrents`)
      return
    }

    toast.success(`Blocked ${uniqueHashes.length} cross-seed ${uniqueHashes.length === 1 ? "torrent" : "torrents"}`)
  }, [instanceId])

  return { blockCrossSeedHashes } as const
}
