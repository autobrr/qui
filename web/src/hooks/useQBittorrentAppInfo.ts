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
  platform?: string
  isWindows: boolean
  isMacOS: boolean
  isLinux: boolean
  hasBuildInfo: boolean
}

export interface QBittorrentFieldVisibility {
  showDiskCacheFields: boolean
  showCoalesceReadsWritesField: boolean
  showI2pFields: boolean
  showMemoryWorkingSetLimit: boolean
  showHashingThreadsField: boolean
  showDiskIoTypeField: boolean
  showI2pInboundQuantity: boolean
  showI2pOutboundQuantity: boolean
  showI2pInboundLength: boolean
  showI2pOutboundLength: boolean
  showMarkOfTheWeb: boolean
  showSocketBacklogField: boolean
  showSendBufferFields: boolean
  showRequestQueueField: boolean
  showProtocolFields: boolean
  showInterfaceFields: boolean
  showUpnpLeaseField: boolean
  showWindowsSpecificFields: boolean
  showMacSpecificFields: boolean
  showLinuxSpecificFields: boolean
  versionInfo: QBittorrentVersionInfo
  isUnknown: boolean
  isLoading: boolean
  isError: boolean
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

export function getFieldVisibility(
  versionInfo: QBittorrentVersionInfo,
  options: { hasBuildInfo: boolean; isLoading: boolean; isError: boolean }
): QBittorrentFieldVisibility {
  const libtorrentKnown = typeof versionInfo.isLibtorrent2 === "boolean"
  const isLibtorrent2 = versionInfo.isLibtorrent2 === true
  const platformKnown = typeof versionInfo.platform === "string" && versionInfo.platform.length > 0
  const isUnknown = (!options.hasBuildInfo || options.isError) && !options.isLoading

  // Prefer showing fields when we cannot confidently determine availability.
  const showWhenUnknown = (predicate: boolean) => predicate || !libtorrentKnown
  const showWhenUnknownPlatform = (predicate: boolean) => predicate || !platformKnown

  return {
    showDiskCacheFields: !isLibtorrent2 || !libtorrentKnown,
    showCoalesceReadsWritesField: !isLibtorrent2 || !libtorrentKnown,

    showI2pFields: showWhenUnknown(isLibtorrent2),
    showMemoryWorkingSetLimit: showWhenUnknown(isLibtorrent2 && !(versionInfo.isLinux || versionInfo.isMacOS)),
    showHashingThreadsField: showWhenUnknown(isLibtorrent2),
    showDiskIoTypeField: showWhenUnknown(isLibtorrent2),
    showI2pInboundQuantity: showWhenUnknown(isLibtorrent2),
    showI2pOutboundQuantity: showWhenUnknown(isLibtorrent2),
    showI2pInboundLength: showWhenUnknown(isLibtorrent2),
    showI2pOutboundLength: showWhenUnknown(isLibtorrent2),

    showMarkOfTheWeb: showWhenUnknownPlatform(versionInfo.isMacOS || versionInfo.isWindows),

    showSocketBacklogField: true,
    showSendBufferFields: true,
    showRequestQueueField: true,
    showProtocolFields: true,
    showInterfaceFields: true,
    showUpnpLeaseField: true,
    showWindowsSpecificFields: showWhenUnknownPlatform(versionInfo.isWindows),
    showMacSpecificFields: showWhenUnknownPlatform(versionInfo.isMacOS),
    showLinuxSpecificFields: showWhenUnknownPlatform(versionInfo.isLinux),

    versionInfo,
    isUnknown,
    isLoading: options.isLoading,
    isError: options.isError,
  }
}

/**
 * Hook to fetch qBittorrent application version and build information
 */
export function useQBittorrentAppInfo(
  instanceId: number | undefined,
  options: UseQBittorrentAppInfoOptions = {}
) {
  return useQBittorrentAppInfoImpl(instanceId, options)
}

function useQBittorrentAppInfoImpl(
  instanceId: number | undefined,
  options: UseQBittorrentAppInfoOptions
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
      return {
        appVersion,
        webAPIVersion,
        isWindows: false,
        isMacOS: false,
        isLinux: false,
        hasBuildInfo: false,
      }
    }

    const platform = query.data.buildInfo.platform?.toLowerCase()
    const libtorrentMajorVersion = parseLibtorrentMajor(query.data.buildInfo.libtorrent)

    const isWindows = platform?.includes("windows") || platform?.includes("win") || false
    const isMacOS = platform?.includes("darwin") || platform?.includes("mac") || false
    const hasPlatformString = Boolean(platform)
    const isLinux = platform?.includes("linux") || (hasPlatformString && !isWindows && !isMacOS)

    return {
      appVersion,
      webAPIVersion,
      libtorrentMajorVersion,
      isLibtorrent2: typeof libtorrentMajorVersion === "number" ? libtorrentMajorVersion >= 2 : undefined,
      platform,
      isWindows,
      isMacOS,
      isLinux,
      hasBuildInfo: true,
    }
  }, [query.data])

  return {
    ...query,
    versionInfo,
  }
}

/**
 * Hook to determine field visibility based on qBittorrent version and platform
 */
export function useQBittorrentFieldVisibility(instanceId: number | undefined) {
  const query = useQBittorrentAppInfo(instanceId)

  return useMemo(() => {
    return getFieldVisibility(query.versionInfo, {
      hasBuildInfo: query.versionInfo.hasBuildInfo,
      isLoading: query.isLoading,
      isError: query.isError,
    })
  }, [query.versionInfo, query.isLoading, query.isError])
}
