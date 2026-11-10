/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useEffect, useMemo } from "react"

import { api } from "@/lib/api"
import type { QBittorrentAppInfo } from "@/types"

export interface QBittorrentVersionInfo {
  appVersion: string
  webAPIVersion: string
  libtorrentMajorVersion?: number
  isLibtorrent2?: boolean
}

export interface UseQBittorrentAppInfoOptions {
  initialData?: QBittorrentAppInfo | null
  fetchIfMissing?: boolean
}

function parseLibtorrentMajor(version: string | undefined): number | undefined {
  if (!version) {
    return undefined
  }

  const match = version.match(/^(\d+)\./)
  if (!match) {
    return undefined
  }

  const parsed = parseInt(match[1], 10)
  return Number.isNaN(parsed) ? undefined : parsed
}

/**
 * Hook to fetch qBittorrent application version and build information
 */
export function useQBittorrentAppInfo(
  instanceId: number | undefined,
  options: UseQBittorrentAppInfoOptions = {}
) {
  const queryClient = useQueryClient()
  const queryKey = useMemo(
    () => ["qbittorrent-app-info", instanceId] as const,
    [instanceId]
  )
  const cachedData = instanceId
    ? queryClient.getQueryData<QBittorrentAppInfo | undefined>(queryKey)
    : undefined
  const providedInitial = options.initialData ?? undefined
  const initialData = providedInitial ?? cachedData
  const hasInitialData = initialData !== undefined && initialData !== null
  const shouldFetch =
    !!instanceId && (options.fetchIfMissing ?? true) && !hasInitialData

  const query = useQuery({
    queryKey,
    queryFn: () => api.getQBittorrentAppInfo(instanceId!),
    enabled: shouldFetch,
    initialData: hasInitialData ? (initialData as QBittorrentAppInfo) : undefined,
    initialDataUpdatedAt: hasInitialData ? Date.now() : undefined,
    staleTime: 5 * 60 * 1000, // 5 minutes - app info doesn't change often
    refetchOnWindowFocus: false,
  })

  useEffect(() => {
    if (!instanceId || !options.initialData) {
      return
    }

    queryClient.setQueryData(queryKey, options.initialData)
  }, [instanceId, options.initialData, queryClient, queryKey])

  const versionInfo = useMemo<QBittorrentVersionInfo>(() => {
    const appVersion = query.data?.version || ""
    const webAPIVersion = query.data?.webAPIVersion || ""

    if (!query.data?.buildInfo) {
      return { appVersion, webAPIVersion }
    }

    const libtorrentMajorVersion = parseLibtorrentMajor(query.data.buildInfo.libtorrent)

    return {
      appVersion,
      webAPIVersion,
      libtorrentMajorVersion,
      isLibtorrent2: typeof libtorrentMajorVersion === "number" ? libtorrentMajorVersion >= 2 : undefined,
    }
  }, [query.data])

  return {
    ...query,
    versionInfo,
  }
}
