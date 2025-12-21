/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { Progress } from "@/components/ui/progress"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { usePersistedTabState } from "@/hooks/usePersistedTabState"
import { api } from "@/lib/api"
import { useCrossSeedMatches } from "@/lib/cross-seed-utils"
import { getLinuxCategory, getLinuxComment, getLinuxCreatedBy, getLinuxFileName, getLinuxHash, getLinuxIsoName, getLinuxSavePath, getLinuxTags, getLinuxTracker, useIncognitoMode } from "@/lib/incognito"
import { renderTextWithLinks } from "@/lib/linkUtils"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import { getPeerFlagDetails } from "@/lib/torrent-peer-flags"
import { getStateLabel } from "@/lib/torrent-state-utils"
import { resolveTorrentHashes } from "@/lib/torrent-utils"
import { cn, copyTextToClipboard, formatBytes, formatDuration } from "@/lib/utils"
import type { SortedPeersResponse, Torrent, TorrentFile, TorrentPeer } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import "flag-icons/css/flag-icons.min.css"
import { Ban, Copy, Loader2, Trash2, UserPlus, X } from "lucide-react"
import { memo, useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"
import { CrossSeedTable, GeneralTabHorizontal, PeersTable, TorrentFileTable, TrackersTable, WebSeedsTable } from "./details"
import { RenameTorrentFileDialog, RenameTorrentFolderDialog } from "./TorrentDialogs"
import { TorrentFileTree } from "./TorrentFileTree"

interface TorrentDetailsPanelProps {
  instanceId: number;
  torrent: Torrent | null;
  initialTab?: string;
  onInitialTabConsumed?: () => void;
  layout?: "horizontal" | "vertical";
  onClose?: () => void;
}

const TAB_VALUES = ["general", "trackers", "peers", "webseeds", "content", "crossseed"] as const
type TabValue = typeof TAB_VALUES[number]
const DEFAULT_TAB: TabValue = "general"
const TAB_STORAGE_KEY = "torrent-details-last-tab"

function isTabValue(value: string): value is TabValue {
  return TAB_VALUES.includes(value as TabValue)
}



function getTrackerStatusBadge(status: number) {
  switch (status) {
    case 0:
      return <Badge variant="secondary">Disabled</Badge>
    case 1:
      return <Badge variant="secondary">Not contacted</Badge>
    case 2:
      return <Badge variant="default">Working</Badge>
    case 3:
      return <Badge variant="default">Updating</Badge>
    case 4:
      return <Badge variant="destructive">Error</Badge>
    default:
      return <Badge variant="outline">Unknown</Badge>
  }
}

export const TorrentDetailsPanel = memo(function TorrentDetailsPanel({ instanceId, torrent, initialTab, onInitialTabConsumed, layout = "vertical", onClose }: TorrentDetailsPanelProps) {
  const [activeTab, setActiveTab] = usePersistedTabState<TabValue>(TAB_STORAGE_KEY, DEFAULT_TAB, isTabValue)

  // Apply initialTab override when provided
  useEffect(() => {
    if (initialTab && isTabValue(initialTab)) {
      setActiveTab(initialTab)
      onInitialTabConsumed?.()
    }
  }, [initialTab, onInitialTabConsumed, setActiveTab])

  // Note: Escape key handling is now unified in Torrents.tsx
  // to close panel and clear selection atomically

  const [showAddPeersDialog, setShowAddPeersDialog] = useState(false)
  const { formatTimestamp } = useDateTimeFormatters()
  const [showBanPeerDialog, setShowBanPeerDialog] = useState(false)
  const [peersToAdd, setPeersToAdd] = useState("")
  const [peerToBan, setPeerToBan] = useState<TorrentPeer | null>(null)
  const [isReady, setIsReady] = useState(false)
  const { data: metadata } = useInstanceMetadata(instanceId)
  const { data: capabilities } = useInstanceCapabilities(instanceId)
  const queryClient = useQueryClient()
  const [speedUnit] = useSpeedUnits()
  const [incognitoMode] = useIncognitoMode()
  const displayName = incognitoMode ? getLinuxIsoName(torrent?.hash ?? "") : torrent?.name
  const incognitoHash = incognitoMode && torrent?.hash ? getLinuxHash(torrent.hash) : undefined
  const [pendingFileIndices, setPendingFileIndices] = useState<Set<number>>(() => new Set())
  const supportsFilePriority = capabilities?.supportsFilePriority ?? false
  const [selectedCrossSeedTorrents, setSelectedCrossSeedTorrents] = useState<Set<string>>(() => new Set())
  const [showDeleteCrossSeedDialog, setShowDeleteCrossSeedDialog] = useState(false)
  const [deleteCrossSeedFiles, setDeleteCrossSeedFiles] = useState(false)
  const [showDeleteCurrentDialog, setShowDeleteCurrentDialog] = useState(false)
  const [deleteCurrentFiles, setDeleteCurrentFiles] = useState(false)
  const copyToClipboard = useCallback(async (text: string, type: string) => {
    try {
      await copyTextToClipboard(text)
      toast.success(`${type} copied to clipboard`)
    } catch {
      toast.error("Failed to copy to clipboard")
    }
  }, [])
  // Wait for component animation before enabling queries when torrent changes
  useEffect(() => {
    setIsReady(false)
    // Small delay to ensure parent component animations complete
    const timer = setTimeout(() => setIsReady(true), 150)
    return () => clearTimeout(timer)
  }, [torrent?.hash])

  // Clear cross-seed selection when torrent changes
  useEffect(() => {
    setSelectedCrossSeedTorrents(new Set())
  }, [torrent?.hash])

  const handleTabChange = useCallback((value: string) => {
    const nextTab = isTabValue(value) ? value : DEFAULT_TAB
    setActiveTab(nextTab)
  }, [setActiveTab])

  const isContentTabActive = activeTab === "content"
  const isCrossSeedTabActive = activeTab === "crossseed"

  // Fetch torrent properties
  const { data: properties, isLoading: loadingProperties } = useQuery({
    queryKey: ["torrent-properties", instanceId, torrent?.hash],
    queryFn: () => api.getTorrentProperties(instanceId, torrent!.hash),
    enabled: !!torrent && isReady,
    staleTime: 30000, // Cache for 30 seconds
    gcTime: 5 * 60 * 1000, // Keep in cache for 5 minutes
  })

  const { infohashV1: resolvedInfohashV1, infohashV2: resolvedInfohashV2 } = resolveTorrentHashes(properties as { hash?: string; infohash_v1?: string; infohash_v2?: string } | undefined, torrent ?? undefined)



  // Use the cross-seed hook to find matching torrents
  const { matchingTorrents, isLoadingMatches } = useCrossSeedMatches(instanceId, torrent, isCrossSeedTabActive)

  // Create a stable key string for detecting changes in matching torrents
  const matchingTorrentsKeys = useMemo(() => {
    return matchingTorrents.map(t => `${t.instanceId}-${t.hash}`).sort().join(',')
  }, [matchingTorrents])

  // Prune stale selections when matching torrents change
  useEffect(() => {
    const validKeysArray = matchingTorrentsKeys.split(',').filter(k => k)

    setSelectedCrossSeedTorrents(prev => {
      if (validKeysArray.length === 0 && prev.size === 0) {
        // Already empty, no change needed
        return prev
      }

      if (validKeysArray.length === 0) {
        // No matches, clear all selections
        return new Set()
      }

      // Remove selections for torrents that no longer exist in matches
      const validKeys = new Set(validKeysArray)
      const updated = new Set(Array.from(prev).filter(key => validKeys.has(key)))

      // Only update if something changed to avoid infinite loops
      return updated.size !== prev.size ? updated : prev
    })
  }, [matchingTorrentsKeys])

  // Fetch torrent trackers
  const { data: trackers, isLoading: loadingTrackers } = useQuery({
    queryKey: ["torrent-trackers", instanceId, torrent?.hash],
    queryFn: () => api.getTorrentTrackers(instanceId, torrent!.hash),
    enabled: !!torrent && isReady, // Fetch immediately, don't wait for tab
    staleTime: 30000,
    gcTime: 5 * 60 * 1000,
  })

  // Fetch torrent files
  const { data: files, isLoading: loadingFiles } = useQuery({
    queryKey: ["torrent-files", instanceId, torrent?.hash],
    queryFn: () => api.getTorrentFiles(instanceId, torrent!.hash),
    enabled: !!torrent && isReady && isContentTabActive,
    staleTime: 30000,
    gcTime: 5 * 60 * 1000,
    refetchInterval: () => {
      if (!isContentTabActive) return false
      if (typeof document !== "undefined" && document.visibilityState === "visible") {
        return 3000
      }
      return false
    },
    refetchOnWindowFocus: isContentTabActive,
    refetchOnReconnect: isContentTabActive,
  })

  const setFilePriorityMutation = useMutation<void, unknown, { indices: number[]; priority: number; hash: string }>({
    mutationFn: async ({ indices, priority, hash }) => {
      await api.setTorrentFilePriority(instanceId, hash, indices, priority)
    },
    onMutate: ({ indices }) => {
      setPendingFileIndices(prev => {
        const next = new Set(prev)
        indices.forEach(index => next.add(index))
        return next
      })
    },
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: ["torrent-files", instanceId, variables.hash] })
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : "Failed to update file priorities"
      toast.error(message)
    },
    onSettled: (_, __, variables) => {
      if (!variables) {
        setPendingFileIndices(() => new Set())
        return
      }

      setPendingFileIndices(prev => {
        const next = new Set(prev)
        variables.indices.forEach(index => next.delete(index))
        return next
      })
    },
  })

  const fileSelectionStats = useMemo(() => {
    if (!files) {
      return { totalFiles: 0, selectedFiles: 0 }
    }

    let selected = 0
    for (const file of files) {
      if (file.priority !== 0) {
        selected += 1
      }
    }

    return { totalFiles: files.length, selectedFiles: selected }
  }, [files])

  const totalFiles = fileSelectionStats.totalFiles
  const selectedFileCount = fileSelectionStats.selectedFiles
  const canSelectAll = supportsFilePriority && (files?.some(file => file.priority === 0) ?? false)
  const canDeselectAll = supportsFilePriority && (files?.some(file => file.priority !== 0) ?? false)

  const handleToggleFileDownload = useCallback((file: TorrentFile, nextSelected: boolean) => {
    if (!torrent || !supportsFilePriority) {
      return
    }

    const desiredPriority = nextSelected ? Math.max(file.priority, 1) : 0
    if (file.priority === desiredPriority) {
      return
    }

    setFilePriorityMutation.mutate({ indices: [file.index], priority: desiredPriority, hash: torrent.hash })
  }, [setFilePriorityMutation, supportsFilePriority, torrent])

  const handleSelectAllFiles = useCallback(() => {
    if (!torrent || !supportsFilePriority || !files) {
      return
    }

    const indices = files.filter(file => file.priority === 0).map(file => file.index)
    if (indices.length === 0) {
      return
    }

    setFilePriorityMutation.mutate({ indices, priority: 1, hash: torrent.hash })
  }, [files, setFilePriorityMutation, supportsFilePriority, torrent])

  const handleDeselectAllFiles = useCallback(() => {
    if (!torrent || !supportsFilePriority || !files) {
      return
    }

    const indices = files.filter(file => file.priority !== 0).map(file => file.index)
    if (indices.length === 0) {
      return
    }

    setFilePriorityMutation.mutate({ indices, priority: 0, hash: torrent.hash })
  }, [files, setFilePriorityMutation, supportsFilePriority, torrent])

  const handleToggleFolderDownload = useCallback((folderPath: string, selected: boolean) => {
    if (!torrent || !supportsFilePriority || !files) {
      return
    }

    // Find all files under this folder
    const folderPrefix = folderPath + "/"
    const indices = files
      .filter(f => f.name.startsWith(folderPrefix))
      .filter(f => selected ? f.priority === 0 : f.priority !== 0)
      .map(f => f.index)

    if (indices.length === 0) {
      return
    }

    setFilePriorityMutation.mutate({
      indices,
      priority: selected ? 1 : 0,
      hash: torrent.hash
    })
  }, [files, setFilePriorityMutation, supportsFilePriority, torrent])

  // Fetch torrent peers with optimized refetch
  const isPeersTabActive = activeTab === "peers"
  const peersQueryKey = ["torrent-peers", instanceId, torrent?.hash] as const

  const { data: peersData, isLoading: loadingPeers } = useQuery<SortedPeersResponse>({
    queryKey: peersQueryKey,
    queryFn: () => api.getTorrentPeers(instanceId, torrent!.hash),
    enabled: !!torrent && isReady && isPeersTabActive,
    refetchInterval: () => {
      if (!isPeersTabActive) return false
      if (typeof document !== "undefined" && document.visibilityState === "visible") {
        return 2000
      }
      return false
    },
    staleTime: 0,
    gcTime: 5 * 60 * 1000,
  })

  // Fetch web seeds (HTTP sources) - always fetch to determine if tab should be shown
  const { data: webseedsData, isLoading: loadingWebseeds } = useQuery({
    queryKey: ["torrent-webseeds", instanceId, torrent?.hash],
    queryFn: () => api.getTorrentWebSeeds(instanceId, torrent!.hash),
    enabled: !!torrent && isReady,
    staleTime: 30000,
    gcTime: 5 * 60 * 1000,
  })
  const hasWebseeds = (webseedsData?.length ?? 0) > 0

  // Redirect away from webseeds tab if it becomes hidden (e.g., switching to a torrent without web seeds)
  useEffect(() => {
    if (activeTab === "webseeds" && !hasWebseeds && !loadingWebseeds) {
      setActiveTab("general")
    }
  }, [activeTab, hasWebseeds, loadingWebseeds, setActiveTab])

  // Add peers mutation
  const addPeersMutation = useMutation({
    mutationFn: async (peers: string[]) => {
      if (!torrent) throw new Error("No torrent selected")
      await api.addPeersToTorrents(instanceId, [torrent.hash], peers)
    },
    onSuccess: () => {
      toast.success("Peers added successfully")
      setShowAddPeersDialog(false)
      setPeersToAdd("")
      queryClient.invalidateQueries({ queryKey: ["torrent-peers", instanceId, torrent?.hash] })
    },
    onError: (error) => {
      toast.error(`Failed to add peers: ${error.message}`)
    },
  })

  // Ban peer mutation
  const banPeerMutation = useMutation({
    mutationFn: async (peer: string) => {
      await api.banPeers(instanceId, [peer])
    },
    onSuccess: () => {
      toast.success("Peer banned successfully")
      setShowBanPeerDialog(false)
      setPeerToBan(null)
      queryClient.invalidateQueries({ queryKey: ["torrent-peers", instanceId, torrent?.hash] })
    },
    onError: (error) => {
      toast.error(`Failed to ban peer: ${error.message}`)
    },
  })

  // Rename file state
  const [showRenameFileDialog, setShowRenameFileDialog] = useState(false)
  const [renameFilePath, setRenameFilePath] = useState<string | null>(null)

  // Rename file mutation
  const renameFileMutation = useMutation<void, unknown, { hash: string; oldPath: string; newPath: string }>({
    mutationFn: async ({ hash, oldPath, newPath }) => {
      await api.renameTorrentFile(instanceId, hash, oldPath, newPath)
    },
    onSuccess: async (_data, variables) => {
      toast.success("File renamed successfully")
      setShowRenameFileDialog(false)
      setRenameFilePath(null)
      // Small delay to let qBittorrent process the rename internally
      await new Promise(resolve => setTimeout(resolve, 500))
      // Force immediate refresh with cache bypass
      try {
        const freshFiles = await api.getTorrentFiles(instanceId, variables.hash, { refresh: true })
        queryClient.setQueryData(["torrent-files", instanceId, variables.hash], freshFiles)
      } catch {
        // Refresh failed, invalidate to trigger background refetch
        queryClient.invalidateQueries({ queryKey: ["torrent-files", instanceId, variables.hash] })
      }
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : "Failed to rename file"
      toast.error(message)
    },
  })

  // Rename folder state
  const [showRenameFolderDialog, setShowRenameFolderDialog] = useState(false)
  const [renameFolderPath, setRenameFolderPath] = useState<string | null>(null)

  // Rename folder mutation
  const renameFolderMutation = useMutation<void, unknown, { hash: string; oldPath: string; newPath: string }>({
    mutationFn: async ({ hash, oldPath, newPath }) => {
      await api.renameTorrentFolder(instanceId, hash, oldPath, newPath)
    },
    onSuccess: async (_data, variables) => {
      toast.success("Folder renamed successfully")
      setShowRenameFolderDialog(false)
      setRenameFolderPath(null)
      // Small delay to let qBittorrent process the rename internally
      await new Promise(resolve => setTimeout(resolve, 500))
      // Force immediate refresh with cache bypass
      try {
        const freshFiles = await api.getTorrentFiles(instanceId, variables.hash, { refresh: true })
        queryClient.setQueryData(["torrent-files", instanceId, variables.hash], freshFiles)
      } catch {
        // Refresh failed, invalidate to trigger background refetch
        queryClient.invalidateQueries({ queryKey: ["torrent-files", instanceId, variables.hash] })
      }
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : "Failed to rename folder"
      toast.error(message)
    },
  })

  const refreshTorrentFiles = useCallback(async () => {
    if (!torrent) return
    try {
      const freshFiles = await api.getTorrentFiles(instanceId, torrent.hash, { refresh: true })
      queryClient.setQueryData(["torrent-files", instanceId, torrent.hash], freshFiles)
    } catch (err) {
      console.warn("Failed to refresh torrent files", err)
    }
  }, [instanceId, queryClient, torrent])

  // Handle copy peer IP:port
  const handleCopyPeer = useCallback(async (peer: TorrentPeer) => {
    const peerAddress = `${peer.ip}:${peer.port}`
    try {
      await copyTextToClipboard(peerAddress)
      toast.success(`Copied ${peerAddress} to clipboard`)
    } catch (err) {
      console.error("Failed to copy to clipboard:", err)
      toast.error("Failed to copy to clipboard")
    }
  }, [])

  // Handle ban peer click
  const handleBanPeerClick = useCallback((peer: TorrentPeer) => {
    setPeerToBan(peer)
    setShowBanPeerDialog(true)
  }, [])

  // Handle ban peer confirmation
  const handleBanPeerConfirm = useCallback(() => {
    if (peerToBan) {
      const peerAddress = `${peerToBan.ip}:${peerToBan.port}`
      banPeerMutation.mutate(peerAddress)
    }
  }, [peerToBan, banPeerMutation])

  // Handle add peers submit
  const handleAddPeersSubmit = useCallback(() => {
    const peers = peersToAdd.split(/[\n,]/).map(p => p.trim()).filter(p => p)
    if (peers.length > 0) {
      addPeersMutation.mutate(peers)
    }
  }, [peersToAdd, addPeersMutation])

  // Handle cross-seed torrent selection
  const handleToggleCrossSeedSelection = useCallback((key: string) => {
    setSelectedCrossSeedTorrents(prev => {
      const next = new Set(prev)
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }, [])

  const handleSelectAllCrossSeed = useCallback(() => {
    const allKeys = matchingTorrents.map(m => `${m.instanceId}-${m.hash}`)
    setSelectedCrossSeedTorrents(new Set(allKeys))
  }, [matchingTorrents])

  const handleDeselectAllCrossSeed = useCallback(() => {
    setSelectedCrossSeedTorrents(new Set())
  }, [])

  // Handle cross-seed deletion
  const handleDeleteCrossSeed = useCallback(async () => {
    const torrentsToDelete = matchingTorrents.filter(m =>
      selectedCrossSeedTorrents.has(`${m.instanceId}-${m.hash}`)
    )

    if (torrentsToDelete.length === 0) return

    try {
      // Group by instance for efficient bulk deletion
      const byInstance = new Map<number, string[]>()
      for (const t of torrentsToDelete) {
        const hashes = byInstance.get(t.instanceId) || []
        hashes.push(t.hash)
        byInstance.set(t.instanceId, hashes)
      }

      // Delete from each instance
      await Promise.all(
        Array.from(byInstance.entries()).map(([instId, hashes]) =>
          api.bulkAction(instId, {
            hashes,
            action: "delete",
            deleteFiles: deleteCrossSeedFiles
          })
        )
      )

      toast.success(`Deleted ${torrentsToDelete.length} torrent${torrentsToDelete.length > 1 ? 's' : ''}`)

      // Refresh all instances
      for (const instId of byInstance.keys()) {
        queryClient.invalidateQueries({ queryKey: ["torrents", instId] })
      }

      setSelectedCrossSeedTorrents(new Set())
      setShowDeleteCrossSeedDialog(false)
    } catch (error) {
      toast.error(`Failed to delete: ${error instanceof Error ? error.message : 'Unknown error'}`)
    }
  }, [selectedCrossSeedTorrents, matchingTorrents, deleteCrossSeedFiles, queryClient])

  const handleDeleteCurrent = useCallback(async () => {
    if (!torrent) return

    try {
      await api.bulkAction(instanceId, {
        hashes: [torrent.hash],
        action: "delete",
        deleteFiles: deleteCurrentFiles
      })

      toast.success(`Deleted torrent: ${torrent.name}`)
      queryClient.invalidateQueries({ queryKey: ["torrents", instanceId] })
      setShowDeleteCurrentDialog(false)

      // Close the details panel by clearing selection (parent component should handle this)
      // The user will be returned to the torrent list
    } catch (error) {
      toast.error(`Failed to delete: ${error instanceof Error ? error.message : 'Unknown error'}`)
    }
  }, [torrent, instanceId, deleteCurrentFiles, queryClient])

  const handleRenameFileDialogOpenChange = useCallback((open: boolean) => {
    setShowRenameFileDialog(open)
    if (!open) {
      setRenameFilePath(null)
    }
  }, [])

  const handleRenameFileClick = useCallback(async (filePath: string) => {
    await refreshTorrentFiles()
    setRenameFilePath(filePath)
    setShowRenameFileDialog(true)
  }, [refreshTorrentFiles])

  // Handle rename file
  const handleRenameFileConfirm = useCallback(({ oldPath, newPath }: { oldPath: string; newPath: string }) => {
    if (!torrent) return
    renameFileMutation.mutate({ hash: torrent.hash, oldPath, newPath })
  }, [renameFileMutation, torrent])

  // Handle rename folder
  const handleRenameFolderConfirm = useCallback(({ oldPath, newPath }: { oldPath: string; newPath: string }) => {
    if (!torrent) return
    renameFolderMutation.mutate({ hash: torrent.hash, oldPath, newPath })
  }, [renameFolderMutation, torrent])

  const handleRenameFolderDialogOpen = useCallback(async (folderPath?: string) => {
    await refreshTorrentFiles()
    setRenameFolderPath(folderPath ?? null)
    setShowRenameFolderDialog(true)
  }, [refreshTorrentFiles])

  // Extract all unique folder paths (including subfolders) from file paths
  const folders = useMemo(() => {
    const folderSet = new Set<string>()
    if (files) {
      files.forEach(file => {
        const parts = file.name.split('/').filter(Boolean)
        if (parts.length <= 1) return

        // Build all folder paths progressively
        let current = ''
        for (let i = 0; i < parts.length - 1; i++) {
          current = current ? `${current}/${parts[i]}` : parts[i]
          folderSet.add(current)
        }
      })
    }
    return Array.from(folderSet)
      .sort((a, b) => a.localeCompare(b))
      .map(name => ({ name }))
  }, [files])

  if (!torrent) return null

  const displayCreatedBy = incognitoMode && properties?.created_by ? getLinuxCreatedBy(torrent.hash) : properties?.created_by
  const displayComment = incognitoMode && properties?.comment ? getLinuxComment(torrent.hash) : properties?.comment
  const displayInfohashV1 = incognitoMode && resolvedInfohashV1 ? incognitoHash : resolvedInfohashV1
  const displayInfohashV2 = incognitoMode && resolvedInfohashV2 ? incognitoHash : resolvedInfohashV2
  const displaySavePath = incognitoMode && properties?.save_path ? getLinuxSavePath(torrent.hash) : properties?.save_path
  const tempPathEnabled = Boolean(properties?.download_path)
  const displayTempPath = incognitoMode && properties?.download_path ? getLinuxSavePath(torrent.hash) : properties?.download_path

  const formatLimitLabel = (limit: number | null | undefined) => {
    if (limit == null || !Number.isFinite(limit) || limit <= 0) {
      return "∞"
    }
    return formatSpeedWithUnit(limit, speedUnit)
  }

  const downloadLimitLabel = formatLimitLabel(properties?.dl_limit ?? torrent.dl_limit)
  const uploadLimitLabel = formatLimitLabel(properties?.up_limit ?? torrent.up_limit)

  // Determine layout mode
  const isHorizontal = layout === "horizontal"

  // Show minimal loading state while waiting for initial data
  const isInitialLoad = !isReady || (loadingProperties && !properties)
  if (isInitialLoad) {
    return (
      <div className="h-full flex items-center justify-center">
        <Loader2 className="h-6 w-6 animate-spin" />
      </div>
    )
  }

  return (
    <div className="h-full flex flex-col">
      <Tabs value={activeTab} onValueChange={handleTabChange} className="flex-1 flex flex-col overflow-hidden">
        <TabsList className="w-full justify-start rounded-none border-b h-8 bg-background px-4 sm:px-6 py-0">
          <TabsTrigger
            value="general"
            className="relative text-xs rounded-none data-[state=active]:bg-transparent data-[state=active]:shadow-none hover:bg-accent/50 transition-all px-3 sm:px-4 cursor-pointer focus-visible:outline-none focus-visible:ring-0 after:absolute after:bottom-0 after:left-0 after:right-0 after:h-[2px] after:bg-primary after:scale-x-0 data-[state=active]:after:scale-x-100 after:transition-transform"
          >
            General
          </TabsTrigger>
          <TabsTrigger
            value="trackers"
            className="relative text-xs rounded-none data-[state=active]:bg-transparent data-[state=active]:shadow-none hover:bg-accent/50 transition-all px-3 sm:px-4 cursor-pointer focus-visible:outline-none focus-visible:ring-0 after:absolute after:bottom-0 after:left-0 after:right-0 after:h-[2px] after:bg-primary after:scale-x-0 data-[state=active]:after:scale-x-100 after:transition-transform"
          >
            Trackers
          </TabsTrigger>
          <TabsTrigger
            value="peers"
            className="relative text-xs rounded-none data-[state=active]:bg-transparent data-[state=active]:shadow-none hover:bg-accent/50 transition-all px-3 sm:px-4 cursor-pointer focus-visible:outline-none focus-visible:ring-0 after:absolute after:bottom-0 after:left-0 after:right-0 after:h-[2px] after:bg-primary after:scale-x-0 data-[state=active]:after:scale-x-100 after:transition-transform"
          >
            Peers
          </TabsTrigger>
          {hasWebseeds && (
            <TabsTrigger
              value="webseeds"
              className="relative text-xs rounded-none data-[state=active]:bg-transparent data-[state=active]:shadow-none hover:bg-accent/50 transition-all px-3 sm:px-4 cursor-pointer focus-visible:outline-none focus-visible:ring-0 after:absolute after:bottom-0 after:left-0 after:right-0 after:h-[2px] after:bg-primary after:scale-x-0 data-[state=active]:after:scale-x-100 after:transition-transform"
            >
              HTTP Sources
            </TabsTrigger>
          )}
          <TabsTrigger
            value="content"
            className="relative text-xs rounded-none data-[state=active]:bg-transparent data-[state=active]:shadow-none hover:bg-accent/50 transition-all px-3 sm:px-4 cursor-pointer focus-visible:outline-none focus-visible:ring-0 after:absolute after:bottom-0 after:left-0 after:right-0 after:h-[2px] after:bg-primary after:scale-x-0 data-[state=active]:after:scale-x-100 after:transition-transform"
          >
            Content
          </TabsTrigger>
          <TabsTrigger
            value="crossseed"
            className="relative text-xs rounded-none data-[state=active]:bg-transparent data-[state=active]:shadow-none hover:bg-accent/50 transition-all px-3 sm:px-4 cursor-pointer focus-visible:outline-none focus-visible:ring-0 after:absolute after:bottom-0 after:left-0 after:right-0 after:h-[2px] after:bg-primary after:scale-x-0 data-[state=active]:after:scale-x-100 after:transition-transform"
          >
            Cross-Seed
          </TabsTrigger>
          {onClose && (
            <Button
              variant="ghost"
              size="icon"
              className="h-5 w-10 shrink-0"
              onClick={onClose}
              aria-label="Close details panel"
            >
              <X className="h-3 w-3" />
            </Button>
          )}
        </TabsList>


        <div className="flex-1 min-h-0 overflow-hidden">
          <TabsContent value="general" className="m-0 h-full">
            {isHorizontal ? (
              <GeneralTabHorizontal
                torrent={torrent}
                properties={properties}
                loading={loadingProperties}
                speedUnit={speedUnit}
                downloadLimit={properties?.dl_limit ?? torrent.dl_limit ?? 0}
                uploadLimit={properties?.up_limit ?? torrent.up_limit ?? 0}
                displayName={displayName}
                displaySavePath={displaySavePath || ""}
                displayTempPath={displayTempPath}
                tempPathEnabled={tempPathEnabled}
                displayInfohashV1={displayInfohashV1 || ""}
                displayInfohashV2={displayInfohashV2}
                displayComment={displayComment}
                displayCreatedBy={displayCreatedBy}
                queueingEnabled={metadata?.preferences?.queueing_enabled}
                maxActiveDownloads={metadata?.preferences?.max_active_downloads}
                maxActiveUploads={metadata?.preferences?.max_active_uploads}
                maxActiveTorrents={metadata?.preferences?.max_active_torrents}
              />
            ) : (
              <ScrollArea className="h-full">
                <div className="p-4 sm:p-6">
                  {loadingProperties && !properties ? (
                    <div className="flex items-center justify-center p-8">
                      <Loader2 className="h-6 w-6 animate-spin" />
                    </div>
                  ) : properties ? (
                    <div className="space-y-6">
                      {/* Transfer Statistics Section */}
                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Transfer Statistics</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 space-y-4 border border-border/50">
                          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">Total Size</p>
                              <p className="text-lg font-semibold">{formatBytes(properties.total_size || torrent.size)}</p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">Share Ratio</p>
                              <p className="text-lg font-semibold">{(properties.share_ratio || 0).toFixed(2)}</p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">Downloaded</p>
                              <p className="text-base font-medium">{formatBytes(properties.total_downloaded || 0)}</p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">Uploaded</p>
                              <p className="text-base font-medium">{formatBytes(properties.total_uploaded || 0)}</p>
                            </div>
                          </div>

                          <Separator className="opacity-50" />

                          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">Pieces</p>
                              <p className="text-sm font-medium">{properties.pieces_have || 0} / {properties.pieces_num || 0}</p>
                              <p className="text-xs text-muted-foreground">({formatBytes(properties.piece_size || 0)} each)</p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">Wasted</p>
                              <p className="text-sm font-medium">{formatBytes(properties.total_wasted || 0)}</p>
                            </div>
                          </div>
                        </div>
                      </div>

                      {/* Speed Section */}
                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Speed</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">Download Speed</p>
                              <p className="text-base font-semibold text-green-500">{formatSpeedWithUnit(properties.dl_speed || 0, speedUnit)}</p>
                              <p className="text-xs text-muted-foreground">avg: {formatSpeedWithUnit(properties.dl_speed_avg || 0, speedUnit)}</p>
                              <p className="text-xs text-muted-foreground">Limit: {downloadLimitLabel}</p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">Upload Speed</p>
                              <p className="text-base font-semibold text-blue-500">{formatSpeedWithUnit(properties.up_speed || 0, speedUnit)}</p>
                              <p className="text-xs text-muted-foreground">avg: {formatSpeedWithUnit(properties.up_speed_avg || 0, speedUnit)}</p>
                              <p className="text-xs text-muted-foreground">Limit: {uploadLimitLabel}</p>
                            </div>
                          </div>
                        </div>
                      </div>

                      {/* Peers Section */}
                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Network</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                          <div className="grid grid-cols-2 gap-4">
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">Seeds</p>
                              <p className="text-base font-semibold">{properties.seeds || 0} <span className="text-sm font-normal text-muted-foreground">/ {properties.seeds_total || 0}</span></p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">Peers</p>
                              <p className="text-base font-semibold">{properties.peers || 0} <span className="text-sm font-normal text-muted-foreground">/ {properties.peers_total || 0}</span></p>
                            </div>
                          </div>
                        </div>
                      </div>

                      {/* Queue Information */}
                      {metadata?.preferences?.queueing_enabled && (
                        <div className="space-y-3">
                          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Queue Management</h3>
                          <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50 space-y-3">
                            <div className="flex items-center justify-between">
                              <span className="text-sm text-muted-foreground">Priority</span>
                              <div className="flex items-center gap-2">
                                <span className="text-sm font-semibold">
                                  {torrent?.priority > 0 ? torrent.priority : "Normal"}
                                </span>
                                {(torrent?.state === "queuedDL" || torrent?.state === "queuedUP") && (
                                  <Badge variant="secondary" className="text-xs">
                                    Queued {torrent.state === "queuedDL" ? "DL" : "UP"}
                                  </Badge>
                                )}
                              </div>
                            </div>
                            {(metadata.preferences.max_active_downloads > 0 ||
                              metadata.preferences.max_active_uploads > 0 ||
                              metadata.preferences.max_active_torrents > 0) && (
                                <>
                                  <Separator className="opacity-50" />
                                  <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 text-xs">
                                    {metadata.preferences.max_active_downloads > 0 && (
                                      <div className="space-y-1">
                                        <p className="text-muted-foreground">Max Downloads</p>
                                        <p className="font-medium">{metadata.preferences.max_active_downloads}</p>
                                      </div>
                                    )}
                                    {metadata.preferences.max_active_uploads > 0 && (
                                      <div className="space-y-1">
                                        <p className="text-muted-foreground">Max Uploads</p>
                                        <p className="font-medium">{metadata.preferences.max_active_uploads}</p>
                                      </div>
                                    )}
                                    {metadata.preferences.max_active_torrents > 0 && (
                                      <div className="space-y-1">
                                        <p className="text-muted-foreground">Max Active</p>
                                        <p className="font-medium">{metadata.preferences.max_active_torrents}</p>
                                      </div>
                                    )}
                                  </div>
                                </>
                              )}
                          </div>
                        </div>
                      )}

                      {/* Time Information */}
                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Time Information</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                          <div className="grid grid-cols-2 gap-4">
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">Time Active</p>
                              <p className="text-sm font-medium">{formatDuration(properties.time_elapsed || 0)}</p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">Seeding Time</p>
                              <p className="text-sm font-medium">{formatDuration(properties.seeding_time || 0)}</p>
                            </div>
                          </div>
                        </div>
                      </div>

                      {/* Save Path */}
                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Save Path</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                          <div className="flex items-center gap-2">
                            <div className="font-mono text-xs sm:text-sm break-all text-muted-foreground bg-background/50 rounded px-2.5 py-2 select-text flex-1">
                              {displaySavePath || "N/A"}
                            </div>
                            {displaySavePath && (
                              <Button
                                variant="ghost"
                                size="icon"
                                className="h-8 w-8 shrink-0"
                                onClick={() => copyToClipboard(displaySavePath, "File location")}
                              >
                                <Copy className="h-3.5 w-3.5" />
                              </Button>
                            )}
                          </div>
                        </div>
                      </div>

                      {/* Temporary Download Path - shown if temp_path_enabled */}
                      {tempPathEnabled && (
                        <div className="space-y-3">
                          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Download Path</h3>
                          <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                            <div className="flex items-center gap-2">
                              <div className="font-mono text-xs sm:text-sm break-all text-muted-foreground bg-background/50 rounded px-2.5 py-2 select-text flex-1">
                                {displayTempPath || "N/A"}
                              </div>
                              {displayTempPath && (
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  className="h-8 w-8 shrink-0"
                                  onClick={() => copyToClipboard(displayTempPath, "Temporary path")}
                                >
                                  <Copy className="h-3.5 w-3.5" />
                                </Button>
                              )}
                            </div>
                          </div>
                        </div>
                      )}

                      {/* Info Hash Display */}
                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Torrent Identifiers</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50 space-y-4">
                          <div className="space-y-2">
                            <p className="text-xs text-muted-foreground">Info Hash v1</p>
                            <div className="flex items-center gap-2">
                              <div className="text-xs font-mono bg-background/50 p-2.5 rounded flex-1 break-all select-text">
                                {displayInfohashV1 || "N/A"}
                              </div>
                              {displayInfohashV1 && (
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  className="h-8 w-8 shrink-0"
                                  onClick={() => copyToClipboard(displayInfohashV1, "Info Hash v1")}
                                >
                                  <Copy className="h-3.5 w-3.5" />
                                </Button>
                              )}
                            </div>
                          </div>
                          {displayInfohashV2 && (
                            <>
                              <Separator className="opacity-50" />
                              <div className="space-y-2">
                                <p className="text-xs text-muted-foreground">Info Hash v2</p>
                                <div className="flex items-center gap-2">
                                  <div className="text-xs font-mono bg-background/50 p-2.5 rounded flex-1 break-all select-text">
                                    {displayInfohashV2}
                                  </div>
                                  <Button
                                    variant="ghost"
                                    size="icon"
                                    className="h-8 w-8 shrink-0"
                                    onClick={() => copyToClipboard(displayInfohashV2, "Info Hash v2")}
                                  >
                                    <Copy className="h-3.5 w-3.5" />
                                  </Button>
                                </div>
                              </div>
                            </>
                          )}
                        </div>
                      </div>

                      {/* Timestamps */}
                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Timestamps</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">Added</p>
                              <p className="text-sm">{formatTimestamp(properties.addition_date)}</p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">Completed</p>
                              <p className="text-sm">{formatTimestamp(properties.completion_date)}</p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">Created</p>
                              <p className="text-sm">{formatTimestamp(properties.creation_date)}</p>
                            </div>
                          </div>
                        </div>
                      </div>

                      {/* Additional Information */}
                      {(displayComment || displayCreatedBy) && (
                        <div className="space-y-3">
                          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Additional Information</h3>
                          <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50 space-y-3">
                            {displayCreatedBy && (
                              <div>
                                <p className="text-xs text-muted-foreground mb-1">Created By</p>
                                <div className="text-sm">{renderTextWithLinks(displayCreatedBy)}</div>
                              </div>
                            )}
                            {displayComment && (
                              <>
                                {displayCreatedBy && <Separator className="opacity-50" />}
                                <div>
                                  <p className="text-xs text-muted-foreground mb-2">Comment</p>
                                  <div className="text-sm bg-background/50 p-3 rounded break-words">
                                    {renderTextWithLinks(displayComment)}
                                  </div>
                                </div>
                              </>
                            )}
                          </div>
                        </div>
                      )}
                    </div>
                  ) : null}
                </div>
              </ScrollArea>
            )}
          </TabsContent>

          <TabsContent value="trackers" className="m-0 h-full">
            {isHorizontal ? (
              <TrackersTable
                trackers={trackers}
                loading={loadingTrackers}
                incognitoMode={incognitoMode}
              />
            ) : (
              <ScrollArea className="h-full">
                <div className="p-4 sm:p-6">
                  {activeTab === "trackers" && loadingTrackers && !trackers ? (
                    <div className="flex items-center justify-center p-8">
                      <Loader2 className="h-6 w-6 animate-spin" />
                    </div>
                  ) : trackers && trackers.length > 0 ? (
                    <div className="space-y-3">
                      <div className="flex items-center justify-between mb-1">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Active Trackers</h3>
                        <span className="text-xs text-muted-foreground">{trackers.length} tracker{trackers.length !== 1 ? "s" : ""}</span>
                      </div>
                      <div className="space-y-2">
                        {trackers
                          .sort((a, b) => {
                            // Sort disabled trackers (status 0) to the end
                            if (a.status === 0 && b.status !== 0) return 1
                            if (a.status !== 0 && b.status === 0) return -1
                            // Then sort by status (working trackers first)
                            if (a.status === 2 && b.status !== 2) return -1
                            if (a.status !== 2 && b.status === 2) return 1
                            return 0
                          })
                          .map((tracker, index) => {
                            const displayUrl = incognitoMode ? getLinuxTracker(`${torrent.hash}-${index}`) : tracker.url
                            const shouldRenderMessage = Boolean(tracker.msg)
                            const messageContent = incognitoMode && shouldRenderMessage ? "Tracker message hidden in incognito mode" : tracker.msg

                            return (
                              <div
                                key={index}
                                className={`backdrop-blur-sm border ${tracker.status === 0 ? "bg-card/30 border-border/30 opacity-60" : "bg-card/50 border-border/50"} hover:border-border transition-all rounded-lg p-4 space-y-3`}
                              >
                                <div className="flex flex-col sm:flex-row sm:items-start justify-between gap-2">
                                  <div className="flex-1 space-y-1">
                                    <div className="flex items-center gap-2">
                                      {getTrackerStatusBadge(tracker.status)}
                                    </div>
                                    <p className="text-xs font-mono text-muted-foreground break-all">{displayUrl}</p>
                                  </div>
                                </div>
                                <Separator className="opacity-50" />
                                <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
                                  <div className="space-y-1">
                                    <p className="text-xs text-muted-foreground">Seeds</p>
                                    <p className="text-sm font-medium">{tracker.num_seeds}</p>
                                  </div>
                                  <div className="space-y-1">
                                    <p className="text-xs text-muted-foreground">Peers</p>
                                    <p className="text-sm font-medium">{tracker.num_peers}</p>
                                  </div>
                                  <div className="space-y-1">
                                    <p className="text-xs text-muted-foreground">Leechers</p>
                                    <p className="text-sm font-medium">{tracker.num_leeches}</p>
                                  </div>
                                  <div className="space-y-1">
                                    <p className="text-xs text-muted-foreground">Downloaded</p>
                                    <p className="text-sm font-medium">{tracker.num_downloaded}</p>
                                  </div>
                                </div>
                                {shouldRenderMessage && messageContent && (
                                  <>
                                    <Separator className="opacity-50" />
                                    <div className="bg-background/50 p-2 rounded">
                                      <div className="text-xs text-muted-foreground break-words">
                                        {renderTextWithLinks(messageContent)}
                                      </div>
                                    </div>
                                  </>
                                )}
                              </div>
                            )
                          })}
                      </div>
                    </div>
                  ) : (
                    <div className="flex items-center justify-center h-32 text-sm text-muted-foreground">
                      No trackers found
                    </div>
                  )}
                </div>
              </ScrollArea>
            )}
          </TabsContent>

          <TabsContent value="peers" className="m-0 h-full">
            {isHorizontal ? (
              <div className="h-full flex flex-col">
                <div className="flex items-center justify-between px-3 py-1.5 border-b text-xs">
                  <span className="text-muted-foreground">
                    {peersData?.sorted_peers?.length ?? 0} peer{(peersData?.sorted_peers?.length ?? 0) !== 1 ? "s" : ""} connected
                  </span>
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-6 text-xs"
                    onClick={() => setShowAddPeersDialog(true)}
                  >
                    <UserPlus className="h-3 w-3 mr-1.5" />
                    Add Peers
                  </Button>
                </div>
                <div className="flex-1 overflow-hidden">
                  <PeersTable
                    peers={peersData?.sorted_peers}
                    loading={loadingPeers}
                    speedUnit={speedUnit}
                    showFlags={true}
                    incognitoMode={incognitoMode}
                    onBanPeer={handleBanPeerClick}
                  />
                </div>
              </div>
            ) : (
              <ScrollArea className="h-full">
                <div className="p-4 sm:p-6">
                  {activeTab === "peers" && loadingPeers && !peersData ? (
                    <div className="flex items-center justify-center p-8">
                      <Loader2 className="h-6 w-6 animate-spin" />
                    </div>
                  ) : peersData && peersData.peers && typeof peersData.peers === "object" && Object.keys(peersData.peers).length > 0 ? (
                    <div className="space-y-3">
                      <div className="flex items-center justify-between mb-1">
                        <div>
                          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Connected Peers</h3>
                          <p className="text-xs text-muted-foreground mt-1">{Object.keys(peersData.peers).length} peer{Object.keys(peersData.peers).length !== 1 ? "s" : ""} connected</p>
                        </div>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setShowAddPeersDialog(true)}
                        >
                          <UserPlus className="h-4 w-4 mr-2" />
                          Add Peers
                        </Button>
                      </div>
                      <div className="space-y-4 mt-4">
                        {(peersData.sorted_peers ||
                          Object.entries(peersData.peers).map(([key, peer]) => ({ key, ...peer }))
                        ).map((peerWithKey) => {
                          const peerKey = peerWithKey.key
                          const peer = peerWithKey
                          const isActive = (peer.dl_speed || 0) > 0 || (peer.up_speed || 0) > 0
                          // Progress is a float between 0 and 1, where 1 = 100%
                          // Note: qBittorrent API doesn't expose the actual seed status, so we rely on progress
                          const progressValue = peer.progress || 0

                          // Match qBittorrent's own WebUI logic for displaying progress
                          let progressPercent = Math.round(progressValue * 100 * 10) / 10 // Round to 1 decimal
                          // If progress rounds to 100% but isn't exactly 1.0, show as 99.9%
                          if (progressPercent === 100.0 && progressValue !== 1.0) {
                            progressPercent = 99.9
                          }

                          // A seeder has exactly 1.0 progress
                          const isSeeder = progressValue === 1.0
                          const flagDetails = getPeerFlagDetails(peer.flags, peer.flags_desc)
                          const hasFlagDetails = flagDetails.length > 0

                          return (
                            <ContextMenu key={peerKey}>
                              <ContextMenuTrigger asChild>
                                <div className={`bg-card/50 backdrop-blur-sm border ${isActive ? "border-border/70" : "border-border/30"} hover:border-border transition-all rounded-lg p-4 space-y-3`}>
                                  {/* Peer Header */}
                                  <div className="flex items-start justify-between gap-3">
                                    <div className="flex-1 space-y-1">
                                      <div className="flex items-center gap-2 flex-wrap">
                                        <span className="font-mono text-sm cursor-context-menu">{peer.ip}:{peer.port}</span>
                                        {peer.country_code && (
                                          <span
                                            className={`fi fi-${peer.country_code.toLowerCase()} rounded text-sm`}
                                            title={peer.country || peer.country_code}
                                          />
                                        )}
                                        {isSeeder && (
                                          <Badge variant="secondary" className="text-xs">Seeder</Badge>
                                        )}
                                      </div>
                                      <p className="text-xs text-muted-foreground">{peer.client || "Unknown client"}</p>
                                    </div>
                                  </div>

                                  <Separator className="opacity-50" />

                                  {/* Progress Bar */}
                                  <div className="space-y-1">
                                    <p className="text-xs text-muted-foreground">Peer Progress</p>
                                    <div className="flex items-center gap-2">
                                      <Progress value={progressPercent} className="flex-1 h-1.5" />
                                      <span className={`text-xs font-medium ${isSeeder ? "text-green-500" : ""}`}>
                                        {progressPercent}%
                                      </span>
                                    </div>
                                  </div>

                                  {/* Transfer Speeds */}
                                  <div className="grid grid-cols-2 gap-3">
                                    <div className="space-y-1">
                                      <p className="text-xs text-muted-foreground">Download Speed</p>
                                      <p className={`text-sm font-medium ${peer.dl_speed && peer.dl_speed > 0 ? "text-green-500" : ""}`}>
                                        {formatSpeedWithUnit(peer.dl_speed || 0, speedUnit)}
                                      </p>
                                    </div>
                                    <div className="space-y-1">
                                      <p className="text-xs text-muted-foreground">Upload Speed</p>
                                      <p className={`text-sm font-medium ${peer.up_speed && peer.up_speed > 0 ? "text-blue-500" : ""}`}>
                                        {formatSpeedWithUnit(peer.up_speed || 0, speedUnit)}
                                      </p>
                                    </div>
                                  </div>

                                  {/* Data Transfer Info */}
                                  <div className="grid grid-cols-2 gap-3 text-xs">
                                    <div className="space-y-1">
                                      <p className="text-muted-foreground">Downloaded</p>
                                      <p className="font-medium">{formatBytes(peer.downloaded || 0)}</p>
                                    </div>
                                    <div className="space-y-1">
                                      <p className="text-muted-foreground">Uploaded</p>
                                      <p className="font-medium">{formatBytes(peer.uploaded || 0)}</p>
                                    </div>
                                  </div>

                                  {/* Connection Info */}
                                  {(peer.connection || hasFlagDetails) && (
                                    <>
                                      <Separator className="opacity-50" />
                                      <div className="flex flex-wrap gap-4 text-xs text-muted-foreground">
                                        {peer.connection && (
                                          <div>
                                            <span className="opacity-70">Connection:</span> {peer.connection}
                                          </div>
                                        )}
                                        {hasFlagDetails && (
                                          <div className="flex items-center gap-2">
                                            <span className="opacity-70">Flags:</span>
                                            <span className="inline-flex flex-wrap gap-1">
                                              {flagDetails.map(({ flag, description }, index) => {
                                                const flagKey = `${flag}-${index}`
                                                const badgeClass =
                                                  "inline-flex items-center justify-center rounded border border-border/60 bg-muted/20 px-1 text-[12px] font-semibold leading-none text-foreground cursor-pointer"

                                                if (!description) {
                                                  return (
                                                    <span
                                                      key={flagKey}
                                                      className={badgeClass}
                                                      aria-label={`Flag ${flag}`}
                                                    >
                                                      {flag}
                                                    </span>
                                                  )
                                                }

                                                return (
                                                  <Tooltip key={flagKey}>
                                                    <TooltipTrigger asChild>
                                                      <span
                                                        className={badgeClass}
                                                        aria-label={description}
                                                      >
                                                        {flag}
                                                      </span>
                                                    </TooltipTrigger>
                                                    <TooltipContent side="top">
                                                      {description}
                                                    </TooltipContent>
                                                  </Tooltip>
                                                )
                                              })}
                                            </span>
                                          </div>
                                        )}
                                      </div>
                                    </>
                                  )}
                                </div>
                              </ContextMenuTrigger>
                              <ContextMenuContent>
                                <ContextMenuItem
                                  onClick={() => handleCopyPeer(peer)}
                                >
                                  <Copy className="h-4 w-4 mr-2" />
                                  Copy IP:port
                                </ContextMenuItem>
                                <ContextMenuSeparator />
                                <ContextMenuItem
                                  onClick={() => handleBanPeerClick(peer)}
                                  className="text-destructive focus:text-destructive"
                                >
                                  <Ban className="h-4 w-4 mr-2" />
                                  Ban peer permanently
                                </ContextMenuItem>
                              </ContextMenuContent>
                            </ContextMenu>
                          )
                        })}
                      </div>
                    </div>
                  ) : (
                    <div className="flex flex-col items-center justify-center h-32 text-sm text-muted-foreground gap-3">
                      <p>No peers connected</p>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setShowAddPeersDialog(true)}
                      >
                        <UserPlus className="h-4 w-4 mr-2" />
                        Add Peers
                      </Button>
                    </div>
                  )}
                </div>
              </ScrollArea>
            )}
          </TabsContent>

          <TabsContent value="webseeds" className="m-0 h-full">
            {isHorizontal ? (
              <WebSeedsTable
                webseeds={webseedsData}
                loading={loadingWebseeds}
                incognitoMode={incognitoMode}
              />
            ) : (
              <ScrollArea className="h-full">
                <div className="p-4 sm:p-6">
                  {activeTab === "webseeds" && loadingWebseeds && !webseedsData ? (
                    <div className="flex items-center justify-center p-8">
                      <Loader2 className="h-6 w-6 animate-spin" />
                    </div>
                  ) : webseedsData && webseedsData.length > 0 ? (
                    <div className="space-y-3">
                      <div>
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">HTTP Sources</h3>
                        <p className="text-xs text-muted-foreground mt-1">{webseedsData.length} source{webseedsData.length !== 1 ? "s" : ""}</p>
                      </div>
                      <div className="space-y-2 mt-4">
                        {webseedsData.map((webseed, index) => (
                          <ContextMenu key={index}>
                            <ContextMenuTrigger asChild>
                              <div className="p-3 rounded-lg border bg-card hover:bg-muted/30 transition-colors cursor-default">
                                <p className="font-mono text-xs break-all">
                                  {incognitoMode ? "***masked***" : renderTextWithLinks(webseed.url)}
                                </p>
                              </div>
                            </ContextMenuTrigger>
                            <ContextMenuContent>
                              <ContextMenuItem
                                onClick={() => {
                                  if (!incognitoMode) {
                                    copyTextToClipboard(webseed.url)
                                    toast.success("URL copied to clipboard")
                                  }
                                }}
                                disabled={incognitoMode}
                              >
                                <Copy className="h-3.5 w-3.5 mr-2" />
                                Copy URL
                              </ContextMenuItem>
                            </ContextMenuContent>
                          </ContextMenu>
                        ))}
                      </div>
                    </div>
                  ) : (
                    <div className="flex items-center justify-center h-32 text-sm text-muted-foreground">
                      No HTTP sources
                    </div>
                  )}
                </div>
              </ScrollArea>
            )}
          </TabsContent>

          <TabsContent value="content" className="m-0 h-full flex flex-col overflow-hidden">
            {isHorizontal ? (
              <TorrentFileTable
                files={files}
                loading={loadingFiles}
                supportsFilePriority={supportsFilePriority}
                pendingFileIndices={pendingFileIndices}
                incognitoMode={incognitoMode}
                torrentHash={torrent.hash}
                onToggleFile={handleToggleFileDownload}
                onToggleFolder={handleToggleFolderDownload}
                onRenameFile={handleRenameFileClick}
                onRenameFolder={(folderPath) => { void handleRenameFolderDialogOpen(folderPath) }}
              />
            ) : activeTab === "content" && loadingFiles && !files ? (
              <div className="flex items-center justify-center p-8 flex-1">
                <Loader2 className="h-6 w-6 animate-spin" />
              </div>
            ) : files && files.length > 0 ? (
              <>
                <div className="flex items-start justify-between gap-3 px-4 sm:px-6 py-3 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
                  <div>
                    <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">File Contents</h3>
                    <span className="text-xs text-muted-foreground">
                      {supportsFilePriority
                        ? `${selectedFileCount} of ${totalFiles} selected`
                        : `${files.length} file${files.length !== 1 ? "s" : ""}`}
                    </span>
                  </div>
                  <div className="flex items-center gap-1">
                    {supportsFilePriority && (
                      <>
                        <Button
                          variant="outline"
                          size="sm"
                          className="h-7 px-2 text-xs"
                          onClick={handleSelectAllFiles}
                          disabled={!canSelectAll || setFilePriorityMutation.isPending}
                        >
                          All
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 px-2 text-xs"
                          onClick={handleDeselectAllFiles}
                          disabled={!canDeselectAll || setFilePriorityMutation.isPending}
                        >
                          None
                        </Button>
                      </>
                    )}
                  </div>
                </div>
                <ScrollArea className="flex-1 min-h-0 w-full [&>[data-slot=scroll-area-viewport]]:!overflow-x-hidden">
                  <div className="p-4 sm:p-6 pb-8">
                    <TorrentFileTree
                      key={torrent.hash}
                      files={files}
                      supportsFilePriority={supportsFilePriority}
                      pendingFileIndices={pendingFileIndices}
                      incognitoMode={incognitoMode}
                      torrentHash={torrent.hash}
                      onToggleFile={handleToggleFileDownload}
                      onToggleFolder={handleToggleFolderDownload}
                      onRenameFile={handleRenameFileClick}
                      onRenameFolder={(folderPath) => { void handleRenameFolderDialogOpen(folderPath) }}
                    />
                  </div>
                </ScrollArea>
              </>
            ) : (
              <div className="flex items-center justify-center h-32 text-sm text-muted-foreground">
                No files found
              </div>
            )}
          </TabsContent>

          <TabsContent value="crossseed" className="m-0 h-full">
            {isHorizontal ? (
              <CrossSeedTable
                matches={matchingTorrents}
                loading={isLoadingMatches}
                speedUnit={speedUnit}
                incognitoMode={incognitoMode}
                selectedTorrents={selectedCrossSeedTorrents}
                onToggleSelection={handleToggleCrossSeedSelection}
                onSelectAll={handleSelectAllCrossSeed}
                onDeselectAll={handleDeselectAllCrossSeed}
                onDeleteMatches={() => setShowDeleteCrossSeedDialog(true)}
                onDeleteCurrent={() => setShowDeleteCurrentDialog(true)}
              />
            ) : (
              <ScrollArea className="h-full">
                <div className="p-4 sm:p-6">
                  {isLoadingMatches ? (
                    <div className="flex items-center justify-center p-8">
                      <Loader2 className="h-6 w-6 animate-spin" />
                    </div>
                  ) : matchingTorrents.length > 0 ? (
                    <div className="space-y-4">
                      <div className="flex flex-col gap-3">
                        <div className="flex flex-col gap-1">
                          <div className="flex items-center gap-2">
                            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Cross-Seed Matches</h3>
                            {isLoadingMatches && (
                              <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
                            )}
                          </div>
                          <span className="text-xs text-muted-foreground">
                            {selectedCrossSeedTorrents.size > 0
                              ? `${selectedCrossSeedTorrents.size} of ${matchingTorrents.length} selected`
                              : isLoadingMatches
                                ? `${matchingTorrents.length} matching torrent${matchingTorrents.length !== 1 ? 's' : ''} found, checking more instances...`
                                : `${matchingTorrents.length} matching torrent${matchingTorrents.length !== 1 ? 's' : ''} found across all instances`}
                          </span>
                        </div>
                        <div className="flex flex-wrap items-center gap-2">
                          {selectedCrossSeedTorrents.size > 0 ? (
                            <>
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={handleDeselectAllCrossSeed}
                              >
                                Deselect All
                              </Button>
                              <Button
                                variant="destructive"
                                size="sm"
                                onClick={() => setShowDeleteCrossSeedDialog(true)}
                              >
                                <Trash2 className="h-4 w-4 mr-2" />
                                Delete Matches ({selectedCrossSeedTorrents.size})
                              </Button>
                            </>
                          ) : (
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={handleSelectAllCrossSeed}
                            >
                              Select All
                            </Button>
                          )}
                          <Button
                            variant="destructive"
                            size="sm"
                            onClick={() => setShowDeleteCurrentDialog(true)}
                          >
                            <Trash2 className="h-4 w-4 mr-2" />
                            Delete This Torrent
                          </Button>
                        </div>
                      </div>
                      <div className="space-y-2">
                        {matchingTorrents.map((match) => {
                          const displayName = incognitoMode ? getLinuxFileName(match.hash, 0) : match.name
                          const progressPercent = match.progress * 100
                          const isComplete = progressPercent === 100
                          const torrentKey = `${match.instanceId}-${match.hash}`
                          const isSelected = selectedCrossSeedTorrents.has(torrentKey)

                          // Extract tracker hostname
                          let trackerHostname = match.tracker
                          if (match.tracker) {
                            try {
                              trackerHostname = new URL(match.tracker).hostname
                            } catch {
                              // Keep original if parsing fails
                            }
                          }

                          // Get enriched status (tracker-aware)
                          const trackerHealth = match.tracker_health ?? null
                          let statusLabel = getStateLabel(match.state)
                          let statusVariant: "default" | "secondary" | "destructive" | "outline" = "outline"
                          let statusClass = ""

                          // Check tracker health first (if supported)
                          if (trackerHealth === "unregistered") {
                            statusLabel = "Unregistered"
                            statusVariant = "outline"
                            statusClass = "text-destructive border-destructive/40 bg-destructive/10"
                          } else if (trackerHealth === "tracker_down") {
                            statusLabel = "Tracker Down"
                            statusVariant = "outline"
                            statusClass = "text-yellow-500 border-yellow-500/40 bg-yellow-500/10"
                          } else {
                            // Normal state-based styling
                            if (match.state === "downloading" || match.state === "uploading") {
                              statusVariant = "default"
                            } else if (
                              match.state === "stalledDL" ||
                              match.state === "stalledUP" ||
                              match.state === "pausedDL" ||
                              match.state === "pausedUP" ||
                              match.state === "queuedDL" ||
                              match.state === "queuedUP"
                            ) {
                              statusVariant = "secondary"
                            } else if (match.state === "error" || match.state === "missingFiles") {
                              statusVariant = "destructive"
                            }
                          }

                          // Match type display
                          const matchType = match.matchType as 'infohash' | 'content_path' | 'save_path' | 'name'
                          const matchLabel = matchType === 'infohash' ? 'Info Hash'
                            : matchType === 'content_path' ? 'Content Path'
                              : matchType === 'save_path' ? 'Save Path'
                                : 'Name'
                          const matchDescription = matchType === 'infohash' ? 'Exact same torrent (same info hash)'
                            : matchType === 'content_path' ? 'Same content location on disk'
                              : matchType === 'save_path' ? 'Same save directory and filename'
                                : 'Same torrent name'

                          return (
                            <div key={torrentKey} className="rounded-lg border bg-card p-4 space-y-3">
                              <div className="space-y-2">
                                <div className="flex items-start gap-3">
                                  <Checkbox
                                    checked={isSelected}
                                    onCheckedChange={() => handleToggleCrossSeedSelection(torrentKey)}
                                    className="mt-0.5 shrink-0"
                                    aria-label={`Select ${displayName}`}
                                  />
                                  <div className="flex-1 min-w-0 space-y-1">
                                    <p className="text-sm font-medium break-words" title={displayName}>{displayName}</p>
                                    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
                                      <span className="shrink-0">Instance: {match.instanceName}</span>
                                      <span className="shrink-0">•</span>
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <span className="cursor-help underline decoration-dotted shrink-0">
                                            Match: {matchLabel}
                                          </span>
                                        </TooltipTrigger>
                                        <TooltipContent>
                                          <p>{matchDescription}</p>
                                        </TooltipContent>
                                      </Tooltip>
                                      {trackerHostname && (
                                        <>
                                          <span className="shrink-0">•</span>
                                          <span className="break-all">Tracker: {incognitoMode ? getLinuxTracker(`${match.hash}-0`) : trackerHostname}</span>
                                        </>
                                      )}
                                      {match.category && (
                                        <>
                                          <span className="shrink-0">•</span>
                                          <span className="break-all">Category: {incognitoMode ? getLinuxCategory(match.hash) : match.category}</span>
                                        </>
                                      )}
                                      {match.tags && (
                                        <>
                                          <span className="shrink-0">•</span>
                                          <span className="break-all">Tags: {incognitoMode ? getLinuxTags(match.hash) : match.tags}</span>
                                        </>
                                      )}
                                    </div>
                                  </div>
                                  <div className="flex flex-col gap-1.5 shrink-0">
                                    <Badge variant={statusVariant} className={cn("text-xs whitespace-nowrap", statusClass)}>
                                      {statusLabel}
                                    </Badge>
                                    <Badge variant="outline" className="text-xs whitespace-nowrap">
                                      {formatBytes(match.size)}
                                    </Badge>
                                  </div>
                                </div>
                                <div className="flex items-center gap-3">
                                  <Progress value={progressPercent} className="flex-1 h-1.5" />
                                  <span className={cn("text-xs font-medium", isComplete ? "text-green-500" : "text-muted-foreground")}>
                                    {Math.round(progressPercent)}%
                                  </span>
                                </div>
                                {(match.upspeed > 0 || match.dlspeed > 0) && (
                                  <div className="flex gap-4 text-xs text-muted-foreground">
                                    {match.dlspeed > 0 && (
                                      <span>↓ {formatSpeedWithUnit(match.dlspeed, speedUnit)}</span>
                                    )}
                                    {match.upspeed > 0 && (
                                      <span>↑ {formatSpeedWithUnit(match.upspeed, speedUnit)}</span>
                                    )}
                                  </div>
                                )}
                              </div>
                            </div>
                          )
                        })}
                      </div>
                      {isLoadingMatches && (
                        <div className="flex items-center justify-center gap-2 p-3 rounded-lg bg-muted/50 text-xs text-muted-foreground">
                          <Loader2 className="h-3 w-3 animate-spin" />
                          <span>
                            Checking more instances...
                          </span>
                        </div>
                      )}
                    </div>
                  ) : (
                    <div className="flex items-center justify-center h-32 text-sm text-muted-foreground">
                      No matching torrents found on other instances
                    </div>
                  )}
                </div>
              </ScrollArea>
            )}
          </TabsContent>
        </div>
      </Tabs>

      {/* Add Peers Dialog */}
      <Dialog open={showAddPeersDialog} onOpenChange={setShowAddPeersDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add Peers</DialogTitle>
            <DialogDescription>
              Add one or more peers to this torrent. Enter each peer as IP:port, one per line or comma-separated.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="peers">Peers</Label>
              <Textarea
                id="peers"
                className="min-h-[100px]"
                placeholder={`192.168.1.100:51413
10.0.0.5:6881
tracker.example.com:8080
[2001:db8::1]:6881`}
                value={peersToAdd}
                onChange={(e) => setPeersToAdd(e.target.value)}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowAddPeersDialog(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleAddPeersSubmit}
              disabled={!peersToAdd.trim() || addPeersMutation.isPending}
            >
              {addPeersMutation.isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              Add Peers
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Ban Peer Confirmation Dialog */}
      <Dialog open={showBanPeerDialog} onOpenChange={setShowBanPeerDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Ban Peer Permanently</DialogTitle>
            <DialogDescription>
              Are you sure you want to permanently ban this peer? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          {peerToBan && (
            <div className="space-y-2 text-sm">
              <div>
                <span className="text-muted-foreground">IP Address:</span>
                <span className="ml-2 font-mono">{peerToBan.ip}:{peerToBan.port}</span>
              </div>
              {peerToBan.client && (
                <div>
                  <span className="text-muted-foreground">Client:</span>
                  <span className="ml-2">{peerToBan.client}</span>
                </div>
              )}
              {peerToBan.country && (
                <div>
                  <span className="text-muted-foreground">Country:</span>
                  <span className="ml-2">{peerToBan.country}</span>
                </div>
              )}
            </div>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setShowBanPeerDialog(false)
                setPeerToBan(null)
              }}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleBanPeerConfirm}
              disabled={banPeerMutation.isPending}
            >
              {banPeerMutation.isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              Ban Peer
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Cross-Seed Torrents Dialog */}
      <Dialog open={showDeleteCrossSeedDialog} onOpenChange={setShowDeleteCrossSeedDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Selected Torrents</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete {selectedCrossSeedTorrents.size} torrent{selectedCrossSeedTorrents.size !== 1 ? 's' : ''}?
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="flex items-center space-x-2">
              <Checkbox
                id="delete-files"
                checked={deleteCrossSeedFiles}
                onCheckedChange={(checked) => setDeleteCrossSeedFiles(checked === true)}
              />
              <Label
                htmlFor="delete-files"
                className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
              >
                Also delete files from disk
              </Label>
            </div>
            <div className="text-sm text-muted-foreground">
              {deleteCrossSeedFiles ? (
                <p className="text-destructive">⚠️ This will permanently delete the torrent files from disk!</p>
              ) : (
                <p>Torrents will be removed but files will remain on disk.</p>
              )}
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setShowDeleteCrossSeedDialog(false)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleDeleteCrossSeed}
            >
              <Trash2 className="h-4 w-4 mr-2" />
              Delete {selectedCrossSeedTorrents.size} Torrent{selectedCrossSeedTorrents.size !== 1 ? 's' : ''}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Current Torrent Dialog */}
      <Dialog open={showDeleteCurrentDialog} onOpenChange={setShowDeleteCurrentDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete This Torrent</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete "{incognitoMode ? getLinuxFileName(torrent?.hash ?? "", 0) : torrent?.name}"?
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="flex items-center space-x-2">
              <Checkbox
                id="delete-current-files"
                checked={deleteCurrentFiles}
                onCheckedChange={(checked) => setDeleteCurrentFiles(checked === true)}
              />
              <Label
                htmlFor="delete-current-files"
                className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
              >
                Also delete files from disk
              </Label>
            </div>
            <div className="text-sm text-muted-foreground">
              {deleteCurrentFiles ? (
                <p className="text-destructive">⚠️ This will permanently delete the torrent files from disk!</p>
              ) : (
                <p>Torrent will be removed but files will remain on disk.</p>
              )}
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setShowDeleteCurrentDialog(false)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleDeleteCurrent}
            >
              <Trash2 className="h-4 w-4 mr-2" />
              Delete Torrent
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Rename File Dialog */}
      <RenameTorrentFileDialog
        open={showRenameFileDialog}
        onOpenChange={handleRenameFileDialogOpenChange}
        files={files || []}
        isLoading={loadingFiles}
        onConfirm={handleRenameFileConfirm}
        isPending={renameFileMutation.isPending}
        initialPath={renameFilePath ?? undefined}
      />

      {/* Rename Folder Dialog */}
      <RenameTorrentFolderDialog
        open={showRenameFolderDialog}
        onOpenChange={setShowRenameFolderDialog}
        folders={folders}
        isLoading={loadingFiles}
        onConfirm={handleRenameFolderConfirm}
        isPending={renameFolderMutation.isPending}
        initialPath={renameFolderPath ?? undefined}
      />
    </div>
  )
});
