/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { useMemo } from "react"

/**
 * Hook to fetch qBittorrent application version and build information
 */
export function useQBittorrentAppInfo(instanceId: number | undefined) {
  const query = useQuery({
    queryKey: ["qbittorrent-app-info", instanceId],
    queryFn: () => api.getQBittorrentAppInfo(instanceId!),
    enabled: !!instanceId,
    staleTime: 5 * 60 * 1000, // 5 minutes - app info doesn't change often
    refetchOnWindowFocus: false,
  })

  const versionInfo = useMemo(() => {
    if (!query.data?.buildInfo) {
      return {
        libtorrentMajorVersion: 2, // Default to v2 for modern clients
        isLibtorrent2: true,
        platform: "linux", // Default platform
        isWindows: false,
        isMacOS: false,
        isLinux: true,
      }
    }

    const libtorrentVersion = query.data.buildInfo.libtorrent || "2.0.0"
    const platform = query.data.buildInfo.platform?.toLowerCase() || "linux"
    
    // Parse libtorrent version to get major version
    const versionMatch = libtorrentVersion.match(/^(\d+)\./)
    const libtorrentMajorVersion = versionMatch ? parseInt(versionMatch[1], 10) : 2

    return {
      libtorrentMajorVersion,
      isLibtorrent2: libtorrentMajorVersion >= 2,
      platform,
      isWindows: platform.includes("windows") || platform.includes("win"),
      isMacOS: platform.includes("darwin") || platform.includes("mac"),
      isLinux: platform.includes("linux") || (!platform.includes("windows") && !platform.includes("darwin") && !platform.includes("mac")),
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
  const { versionInfo } = useQBittorrentAppInfo(instanceId)

  return useMemo(() => {
    const { isLibtorrent2, isWindows, isMacOS, isLinux, platform } = versionInfo

    // Fields that are HIDDEN based on version/platform conditions
    // All other fields are always shown
    return {
      // libtorrent >= 2.x: These fields are HIDDEN
      showDiskCacheFields: !isLibtorrent2, // rowDiskCache + rowDiskCacheExpiryInterval hidden for lt>=2
      showCoalesceReadsWritesField: !isLibtorrent2, // rowCoalesceReadsAndWrites hidden for lt>=2
      
      // libtorrent < 2.x: These fields are HIDDEN  
      showI2pFields: isLibtorrent2, // fieldsetI2p hidden for lt<2
      showMemoryWorkingSetLimit: isLibtorrent2 && !(platform === "linux" || platform === "macos"), // Hidden for lt<2 OR linux/macos
      showHashingThreadsField: isLibtorrent2, // rowHashingThreads hidden for lt<2
      showDiskIoTypeField: isLibtorrent2, // rowDiskIOType hidden for lt<2
      showI2pInboundQuantity: isLibtorrent2, // rowI2pInboundQuantity hidden for lt<2
      showI2pOutboundQuantity: isLibtorrent2, // rowI2pOutboundQuantity hidden for lt<2
      showI2pInboundLength: isLibtorrent2, // rowI2pInboundLength hidden for lt<2
      showI2pOutboundLength: isLibtorrent2, // rowI2pOutboundLength hidden for lt<2
      
      // Platform-specific visibility
      showMarkOfTheWeb: platform === "macos" || platform === "windows", // Hidden unless macos/windows
      
      // Fields that are always shown
      // These include most network, peer management, and basic settings
      showSocketBacklogField: true,
      showSendBufferFields: true, 
      showRequestQueueField: true,
      showProtocolFields: true,
      showInterfaceFields: true,
      showUpnpLeaseField: true,
      showWindowsSpecificFields: isWindows,
      showMacSpecificFields: isMacOS,
      showLinuxSpecificFields: isLinux,
      
      // Version info for debugging
      versionInfo,
    }
  }, [versionInfo])
}