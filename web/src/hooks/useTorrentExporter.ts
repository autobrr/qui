/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useState } from "react"
import { api } from "@/lib/api"
import { getLinuxIsoName } from "@/lib/incognito"
import type { Torrent, TorrentFilters } from "@/types"
import { toast } from "sonner"

interface UseTorrentExporterOptions {
  instanceId: number
  incognitoMode: boolean
}

interface ExportSelection {
  hashes: string[]
  torrents: Torrent[]
  isAllSelected: boolean
  totalSelected: number
  filters?: TorrentFilters
  search?: string
  excludeHashes?: string[]
  sortField?: string
  sortOrder?: "asc" | "desc"
}

export function useTorrentExporter({ instanceId, incognitoMode }: UseTorrentExporterOptions) {
  const [isExporting, setIsExporting] = useState(false)

  const exportTorrents = useCallback(async (selection: ExportSelection) => {
    const {
      hashes,
      torrents,
      isAllSelected,
      totalSelected,
      filters,
      search,
      excludeHashes,
      sortField,
      sortOrder,
    } = selection

    const sanitizedHashes = Array.from(new Set(hashes)).filter(Boolean)
    const excludeSet = new Set(excludeHashes ?? [])

    if (!isAllSelected && sanitizedHashes.length === 0) {
      return
    }

    setIsExporting(true)

    try {
      let targets: Torrent[]
      if (isAllSelected) {
        targets = await fetchAllMatchingTorrents({
          instanceId,
          filters,
          search,
          sortField,
          sortOrder,
          totalSelected,
          excludeSet,
        })
      } else {
        targets = dedupeTorrents(torrents, sanitizedHashes)
      }

      if (targets.length === 0) {
        toast.info("No torrents found to export")
        return
      }

      const filenameCounts = new Map<string, number>()
      let exportedCount = 0

      for (const torrent of targets) {
        if (excludeSet.has(torrent.hash)) {
          continue
        }

        const { blob, filename } = await api.exportTorrent(instanceId, torrent.hash)
        const fallbackName = filename || torrent.name || torrent.hash
        const downloadName = buildDownloadName(torrent.hash, fallbackName, incognitoMode)
        const uniqueName = ensureUniqueFilename(downloadName, filenameCounts)

        triggerBrowserDownload(blob, uniqueName)
        exportedCount += 1
      }

      if (exportedCount === 0) {
        toast.info("No torrents exported")
      } else {
        toast.success(exportedCount === 1 ? "Torrent exported" : `${exportedCount} torrents exported`)
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to export torrent"
      toast.error(message)
    } finally {
      setIsExporting(false)
    }
  }, [incognitoMode, instanceId])

  return { exportTorrents, isExporting }
}

async function fetchAllMatchingTorrents({
  instanceId,
  filters,
  search,
  sortField,
  sortOrder,
  totalSelected,
  excludeSet,
}: {
  instanceId: number
  filters?: TorrentFilters
  search?: string
  sortField?: string
  sortOrder?: "asc" | "desc"
  totalSelected: number
  excludeSet: Set<string>
}): Promise<Torrent[]> {
  const results: Torrent[] = []
  const seen = new Set<string>()
  let page = 0
  const limit = 300

  while (true) {
    const response = await api.getTorrents(instanceId, {
      page,
      limit,
      filters,
      search,
      sort: sortField,
      order: sortOrder,
    })

    const pageTorrents = response.torrents ?? []

    for (const torrent of pageTorrents) {
      if (seen.has(torrent.hash) || excludeSet.has(torrent.hash)) {
        continue
      }
      seen.add(torrent.hash)
      results.push(torrent)

      if (totalSelected > 0 && results.length >= totalSelected) {
        return results
      }
    }

    const hasMoreFlag = response.hasMore
    const hasMore = hasMoreFlag === undefined ? pageTorrents.length === limit : hasMoreFlag

    if (!hasMore || pageTorrents.length === 0) {
      break
    }

    page += 1

    // Safety guard to prevent infinite loops if backend misbehaves
    if (page > 10000) {
      break
    }
  }

  return results
}

function dedupeTorrents(torrents: Torrent[], hashes: string[]): Torrent[] {
  const map = new Map<string, Torrent>()
  for (const torrent of torrents) {
    map.set(torrent.hash, torrent)
  }

  const results: Torrent[] = []
  for (const hash of hashes) {
    const found = map.get(hash)
    if (found) {
      results.push(found)
    }
  }
  return results
}

function triggerBrowserDownload(blob: Blob, filename: string): void {
  const objectUrl = URL.createObjectURL(blob)
  const link = document.createElement("a")
  link.href = objectUrl
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(objectUrl)
}

function buildDownloadName(hash: string, fallback: string, incognitoMode: boolean): string {
  const trimmedFallback = fallback.trim() || hash
  const baseName = incognitoMode ? getLinuxIsoName(hash).replace(/\.iso$/i, "") : trimmedFallback
  if (baseName.toLowerCase().endsWith(".torrent")) {
    return baseName
  }
  return `${baseName}.torrent`
}

function ensureUniqueFilename(filename: string, counts: Map<string, number>): string {
  const dotIndex = filename.lastIndexOf(".")
  const base = dotIndex > 0 ? filename.slice(0, dotIndex) : filename
  const extension = dotIndex > 0 ? filename.slice(dotIndex) : ""

  const currentCount = counts.get(base) ?? 0
  if (currentCount === 0) {
    counts.set(base, 1)
    return filename
  }

  const nextCount = currentCount + 1
  counts.set(base, nextCount)
  return `${base} (${nextCount})${extension}`
}
