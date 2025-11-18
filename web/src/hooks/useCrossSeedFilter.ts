import { useCallback, useRef, useState } from "react"
import { toast } from "sonner"

import { api } from "@/lib/api"
import { searchCrossSeedMatches, type CrossSeedTorrent } from "@/lib/cross-seed-utils"
import type { Instance, Torrent, TorrentFilters } from "@/types"

interface UseCrossSeedFilterOptions {
  instanceId: number
  onFilterChange?: (filters: TorrentFilters) => void
}

export function useCrossSeedFilter({ instanceId, onFilterChange }: UseCrossSeedFilterOptions) {
  const [isFilteringCrossSeeds, setIsFilteringCrossSeeds] = useState(false)
  const isFilteringRef = useRef(false)

  const filterCrossSeeds = useCallback(async (torrents: Torrent[]) => {
    if (!onFilterChange) {
      toast.error("Filtering is unavailable in this view")
      return
    }

    if (isFilteringRef.current) {
      return
    }

    if (torrents.length !== 1) {
      toast.info("Cross-seed filtering only works with a single selected torrent")
      return
    }

    const selectedTorrent = torrents[0]
    isFilteringRef.current = true
    setIsFilteringCrossSeeds(true)
    toast.info("Identifying cross-seeded torrents...")

    try {
      const [allInstances, torrentFiles] = await Promise.all([
        api.getInstances(),
        api.getTorrentFiles(instanceId, selectedTorrent.hash),
      ])

      const matches: CrossSeedTorrent[] = []

      if (allInstances && Array.isArray(allInstances)) {
        const activeInstances = allInstances.filter(instance => instance.isActive)
        if (activeInstances.length === 0) {
          toast.info("No active instances available for cross-seed filtering")
          return
        }

        const searchWithTimeout = async (instance: Instance, timeoutMs = 15000) => {
          let timeoutHandle: ReturnType<typeof setTimeout> | null = null

          const timeoutPromise = new Promise<CrossSeedTorrent[]>((_, reject) => {
            timeoutHandle = setTimeout(() => reject(new Error(`Timeout after ${timeoutMs}ms`)), timeoutMs)
          })

          const searchPromise = searchCrossSeedMatches(
            selectedTorrent,
            instance,
            instanceId,
            torrentFiles || [],
            selectedTorrent.infohash_v1,
            selectedTorrent.infohash_v2
          )

          try {
            return await Promise.race([searchPromise, timeoutPromise])
          } finally {
            if (timeoutHandle) {
              clearTimeout(timeoutHandle)
            }
          }
        }

        const searchPromises = activeInstances.map(instance => searchWithTimeout(instance))
        const results = await Promise.allSettled(searchPromises)

        let successful = 0
        let timedOut = 0
        let failed = 0

        results.forEach(result => {
          if (result.status === "fulfilled") {
            matches.push(...result.value)
            successful++
          } else if (result.reason?.message?.includes("Timeout")) {
            timedOut++
          } else {
            failed++
          }
        })

        if (timedOut > 0 || failed > 0) {
          toast.info(
            `Search completed with partial results`,
            {
              description: `${successful}/${activeInstances.length} instances searched successfully. ${timedOut} timed out, ${failed} failed.`,
              duration: 5000,
            }
          )
        }
      }

      if (matches.length === 0) {
        toast.info("No cross-seeded torrents found")
        return
      }

      const hashConditions = matches.map(match => `Hash == "${match.hash}"`)
      hashConditions.push(`Hash == "${selectedTorrent.hash}"`)
      const uniqueConditions = [...new Set(hashConditions)]

      const newFilters: TorrentFilters = {
        status: [],
        excludeStatus: [],
        categories: [],
        excludeCategories: [],
        tags: [],
        excludeTags: [],
        trackers: [],
        excludeTrackers: [],
        expr: uniqueConditions.join(" || "),
      }

      onFilterChange(newFilters)
      toast.success(`Found ${matches.length} cross-seeded torrents (showing ${uniqueConditions.length} total)`)
    } catch (error) {
      console.error("Failed to identify cross-seeded torrents:", error)
      toast.error("Failed to identify cross-seeded torrents")
    } finally {
      isFilteringRef.current = false
      setIsFilteringCrossSeeds(false)
    }
  }, [instanceId, onFilterChange])

  return { isFilteringCrossSeeds, filterCrossSeeds }
}
