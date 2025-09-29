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
    const { isLibtorrent2, isWindows, isMacOS, isLinux } = versionInfo

    return {
      // Connection & Network fields
      showUpnpLeaseField: isLibtorrent2, // Only show UPNP lease duration for libtorrent 2.x
      showProtocolFields: true, // Always show protocol fields
      showInterfaceFields: true, // Always show interface fields (though they may be read-only)
      
      // Advanced Network fields
      showSocketBacklogField: isLibtorrent2, // Socket backlog size only for libtorrent 2.x
      showSendBufferFields: isLibtorrent2, // Send buffer settings only for libtorrent 2.x
      showRequestQueueField: isLibtorrent2, // Request queue size only for libtorrent 2.x
      
      // Performance & Disk I/O fields (based on qBittorrent WebUI logic)
      showMemoryWorkingSetLimit: isLibtorrent2 && isWindows, // Only for libtorrent 2.x AND Windows
      showHashingThreadsField: isLibtorrent2, // Only for libtorrent 2.x
      showDiskIoTypeField: isLibtorrent2, // Only for libtorrent 2.x
      showDiskCacheFields: !isLibtorrent2, // Hidden for libtorrent 2.x, shown for < 2.x
      showCoalesceReadsWritesField: !isLibtorrent2, // Hidden for libtorrent 2.x, shown for < 2.x
      
      // Platform-specific fields
      showWindowsSpecificFields: isWindows,
      showMacSpecificFields: isMacOS,
      showLinuxSpecificFields: isLinux,
      
      // Version info for debugging
      versionInfo,
    }
  }, [versionInfo])
}