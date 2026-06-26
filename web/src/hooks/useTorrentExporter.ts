/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useState } from "react"
import { api } from "@/lib/api"
import { getLinuxIsoName } from "@/lib/incognito"
import { isAllInstancesScope } from "@/lib/instances"
import { getTorrentTargetInstanceId, type TorrentActionTarget } from "@/lib/torrent-action-targets"
import type { Torrent, TorrentFilters } from "@/types"
import { useTranslation } from "react-i18next"
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
  excludeTargets?: TorrentActionTarget[]
  instanceIds?: number[]
  sortField?: string
  sortOrder?: "asc" | "desc"
}

export function useTorrentExporter({ instanceId, incognitoMode }: UseTorrentExporterOptions) {
  const { t } = useTranslation("torrents")
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
      excludeTargets,
      instanceIds,
      sortField,
      sortOrder,
    } = selection

    const sanitizedHashes = Array.from(new Set(hashes)).filter(Boolean)
    const excludeSet = new Set(excludeHashes ?? [])
    const excludeTargetSet = new Set((excludeTargets ?? []).map(target => targetKey(target.instanceId, target.hash)))

    if (!isAllSelected && sanitizedHashes.length === 0) {
      return
    }

    setIsExporting(true)

    try {
      let targets: Torrent[]
      if (isAllSelected) {
        targets = await fetchAllMatchingTorrents({
          instanceId,
          instanceIds,
          filters,
          search,
          sortField,
          sortOrder,
          totalSelected,
          excludeSet,
          excludeTargetSet,
        })
      } else {
        targets = dedupeTorrents(torrents, sanitizedHashes, instanceId)
      }

      if (targets.length === 0) {
        toast.info(t("contextMenu.toast.noTorrentsFoundToExport"))
        return
      }

      const filenameCounts = new Map<string, number>()
      let exportedCount = 0

      for (const torrent of targets) {
        const targetInstanceId = getTorrentTargetInstanceId(torrent, instanceId)
        if (excludeSet.has(torrent.hash) || excludeTargetSet.has(targetKey(targetInstanceId, torrent.hash))) {
          continue
        }

        const { blob, filename } = await api.exportTorrent(targetInstanceId, torrent.hash)
        const fallbackName = filename || torrent.name || torrent.hash
        const downloadName = buildDownloadName(torrent.hash, fallbackName, incognitoMode)
        const uniqueName = ensureUniqueFilename(downloadName, filenameCounts)

        triggerBrowserDownload(blob, uniqueName)
        exportedCount += 1
      }

      if (exportedCount === 0) {
        toast.info(t("contextMenu.toast.noTorrentsExported"))
      } else {
        toast.success(exportedCount === 1? t("contextMenu.toast.torrentExported"): t("contextMenu.toast.torrentsExported", { count: exportedCount }))
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : t("contextMenu.toast.exportFailed")
      toast.error(message)
    } finally {
      setIsExporting(false)
    }
  }, [incognitoMode, instanceId, t])

  return { exportTorrents, isExporting }
}

async function fetchAllMatchingTorrents({
  instanceId,
  instanceIds,
  filters,
  search,
  sortField,
  sortOrder,
  totalSelected,
  excludeSet,
  excludeTargetSet,
}: {
  instanceId: number
  instanceIds?: number[]
  filters?: TorrentFilters
  search?: string
  sortField?: string
  sortOrder?: "asc" | "desc"
  totalSelected: number
  excludeSet: Set<string>
  excludeTargetSet: Set<string>
}): Promise<Torrent[]> {
  const results: Torrent[] = []
  const seen = new Set<string>()
  let page = 0
  const limit = 300
  const allInstances = isAllInstancesScope(instanceId)

  while (true) {
    const params = {
      page,
      limit,
      filters,
      search,
      sort: sortField,
      order: sortOrder,
    }
    const response = allInstances? await api.getCrossInstanceTorrents({ ...params, instanceIds }): await api.getTorrents(instanceId, params)

    const pageTorrents = allInstances? response.crossInstanceTorrents ?? response.cross_instance_torrents ?? []: response.torrents ?? []

    for (const torrent of pageTorrents) {
      const key = targetKey(getTorrentTargetInstanceId(torrent, instanceId), torrent.hash)
      if (seen.has(key) || excludeSet.has(torrent.hash) || excludeTargetSet.has(key)) {
        continue
      }
      seen.add(key)
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

// Identity is instanceId:hash, not hash alone: in the unified view the same
// infohash can live on several instances (cross-seeds) and each copy is a
// distinct export target.
function targetKey(instanceId: number, hash: string): string {
  return `${instanceId}:${hash.toLowerCase()}`
}

function dedupeTorrents(torrents: Torrent[], hashes: string[], fallbackInstanceId: number): Torrent[] {
  const wanted = new Set(hashes.map(hash => hash.toLowerCase()))
  const seen = new Set<string>()

  const results: Torrent[] = []
  for (const torrent of torrents) {
    const hash = torrent.hash?.trim()
    if (!hash || !wanted.has(hash.toLowerCase())) {
      continue
    }

    const key = targetKey(getTorrentTargetInstanceId(torrent, fallbackInstanceId), hash)
    if (seen.has(key)) {
      continue
    }

    seen.add(key)
    results.push(torrent)
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
