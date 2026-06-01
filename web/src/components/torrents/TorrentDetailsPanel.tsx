/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
import { useSyncStream } from "@/contexts/SyncStreamContext"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { usePersistedTabState } from "@/hooks/usePersistedTabState"
import { api } from "@/lib/api"
import { isHardlinkManaged, useLocalCrossSeedMatches } from "@/lib/cross-seed-utils"
import { getLinuxCategory, getLinuxComment, getLinuxCreatedBy, getLinuxFileName, getLinuxHash, getLinuxIsoName, getLinuxSavePath, getLinuxTags, getLinuxTracker, useIncognitoMode } from "@/lib/incognito"
import { renderTextWithLinks } from "@/lib/linkUtils"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import { getPeerFlagDetails } from "@/lib/torrent-peer-flags"
import { getStateLabel } from "@/lib/torrent-state-utils"
import { resolveTorrentHashes } from "@/lib/torrent-utils"
import { getTrackerStatusBadge } from "@/lib/tracker-utils"
import { cn, copyTextToClipboard, formatBytes, formatDuration } from "@/lib/utils"
import type { SortedPeersResponse, Torrent, TorrentFile, TorrentFilters, TorrentStreamPayload, TorrentTracker, TorrentPeer } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import "flag-icons/css/flag-icons.min.css"
import { Ban, Copy, Loader2, Trash2, UserPlus, X } from "lucide-react"
import { memo, useCallback, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { CrossSeedTable, GeneralTabHorizontal, PeersTable, TorrentFileTable, TrackerContextMenu, TrackersTable, WebSeedsTable } from "./details"
import { EditTrackerDialog, RenameTorrentFileDialog, RenameTorrentFolderDialog } from "./TorrentDialogs"
import { TorrentFileMediaInfoDialog } from "./TorrentFileMediaInfoDialog"
import { TorrentFileTree } from "./TorrentFileTree"

interface TorrentDetailsPanelProps {
  instanceId: number;
  torrent: Torrent | null;
  initialTab?: string;
  onInitialTabConsumed?: () => void;
  layout?: "horizontal" | "vertical";
  onClose?: () => void;
  onNavigateToTorrent?: (instanceId: number, torrentHash: string) => void;
}

const TAB_VALUES = ["general", "trackers", "peers", "webseeds", "content", "crossseed"] as const
type TabValue = typeof TAB_VALUES[number]
const DEFAULT_TAB: TabValue = "general"
const TAB_STORAGE_KEY = "torrent-details-last-tab"

function isTabValue(value: string): value is TabValue {
  return TAB_VALUES.includes(value as TabValue)
}



export const TorrentDetailsPanel = memo(function TorrentDetailsPanel({ instanceId, torrent, initialTab, onInitialTabConsumed, layout = "vertical", onClose, onNavigateToTorrent }: TorrentDetailsPanelProps) {
  const { t } = useTranslation("torrents")
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
  const { data: instances } = useQuery({ queryKey: ["instances"], queryFn: () => api.getInstances(), staleTime: 60000 })
  const hasLocalFilesystemAccess = instances?.find(i => i.id === instanceId)?.hasLocalFilesystemAccess ?? false
  const [selectedCrossSeedTorrents, setSelectedCrossSeedTorrents] = useState<Set<string>>(() => new Set())
  const [showDeleteCrossSeedDialog, setShowDeleteCrossSeedDialog] = useState(false)
  const [deleteCrossSeedFiles, setDeleteCrossSeedFiles] = useState(false)
  const [showDeleteCurrentDialog, setShowDeleteCurrentDialog] = useState(false)
  const [deleteCurrentFiles, setDeleteCurrentFiles] = useState(false)
  const [showEditTrackerDialog, setShowEditTrackerDialog] = useState(false)
  const [trackerToEdit, setTrackerToEdit] = useState<TorrentTracker | null>(null)
  const supportsTrackerEditing = capabilities?.supportsTrackerEditing ?? false
  const copyToClipboard = useCallback(async (text: string, type: string) => {
    try {
      await copyTextToClipboard(text)
      toast.success(t("detailsPanel.toast.copied", { type }))
    } catch {
      toast.error(t("detailsPanel.toast.copyFailed"))
    }
  }, [t])
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
  const hashFilter = useMemo<TorrentFilters | undefined>(() => {
    if (!torrent?.hash) {
      return undefined
    }

    return {
      expr: `Hash == "${torrent.hash}"`,
      status: [],
      excludeStatus: [],
      categories: [],
      excludeCategories: [],
      tags: [],
      excludeTags: [],
      trackers: [],
      excludeTrackers: [],
    }
  }, [torrent?.hash])
  const streamParams = useMemo(() => {
    if (!hashFilter || !isReady) {
      return null
    }

    return {
      instanceId,
      page: 0,
      limit: 1,
      sort: "added_on",
      order: "desc" as const,
      filters: hashFilter,
    }
  }, [hashFilter, instanceId, isReady])
  const [streamTorrent, setStreamTorrent] = useState<Torrent | null>(null)
  const handleStreamPayload = useCallback(
    (payload: TorrentStreamPayload) => {
      if (!payload?.data || !torrent?.hash) {
        return
      }

      const nextTorrent = payload.data.torrents?.find(item => item.hash === torrent.hash) ?? null
      if (!nextTorrent && payload.data.total === 0) {
        setStreamTorrent(null)
        return
      }
      if (nextTorrent) {
        setStreamTorrent(nextTorrent)
      }
    },
    [torrent?.hash]
  )
  const streamState = useSyncStream(streamParams, {
    enabled: Boolean(streamParams),
    onMessage: handleStreamPayload,
  })

  useEffect(() => {
    setStreamTorrent(null)
  }, [torrent?.hash])

  // Drop the streamed snapshot when the stream is not live so the merge below falls
  // back to fresh poll data instead of freezing on a stale pre-disconnect snapshot.
  useEffect(() => {
    if (!streamState.connected || streamState.error) {
      setStreamTorrent(null)
    }
  }, [streamState.connected, streamState.error])

  // Fetch torrent properties
  const { data: properties, isLoading: loadingProperties } = useQuery({
    queryKey: ["torrent-properties", instanceId, torrent?.hash],
    queryFn: () => api.getTorrentProperties(instanceId, torrent!.hash),
    enabled: !!torrent && isReady,
    staleTime: 30000, // Cache for 30 seconds
    gcTime: 5 * 60 * 1000, // Keep in cache for 5 minutes
  })

  const { infohashV1: resolvedInfohashV1, infohashV2: resolvedInfohashV2 } = resolveTorrentHashes(properties as { hash?: string; infohash_v1?: string; infohash_v2?: string } | undefined, torrent ?? undefined)



  // Use the cross-seed hook to find matching torrents (uses backend API with rls library)
  const { matchingTorrents, isLoadingMatches, allInstances } = useLocalCrossSeedMatches(instanceId, torrent, isCrossSeedTabActive)

  // Build instance lookup map for CrossSeedTable
  const instanceById = useMemo(
    () => new Map(allInstances.map(i => [i.id, i])),
    [allInstances]
  )

  // Create a stable key string for detecting changes in matching torrents
  const matchingTorrentsKeys = useMemo(() => {
    return matchingTorrents.map(t => `${t.instanceId}-${t.hash}`).sort().join(",")
  }, [matchingTorrents])

  // Prune stale selections when matching torrents change
  useEffect(() => {
    const validKeysArray = matchingTorrentsKeys.split(",").filter(k => k)

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

  const shouldUseFallbackPolling = !!torrent && isReady && (!streamState.connected || !!streamState.error)

  // SSE is primary for live row state; polling only runs while stream is unavailable.
  const { data: polledLiveTorrent } = useQuery({
    queryKey: ["torrent-live-state", instanceId, torrent?.hash],
    queryFn: async () => {
      const response = await api.getTorrents(instanceId, {
        filters: hashFilter!,
        limit: 1,
      })
      return response.torrents[0] ?? null
    },
    enabled: shouldUseFallbackPolling && !!hashFilter,
    staleTime: 1000,
    refetchInterval: shouldUseFallbackPolling ? 2000 : false,
  })

  // Merge live data with prop, preferring live values for frequently-changing fields
  const liveTorrent = streamTorrent ?? polledLiveTorrent ?? null
  const displayTorrent = useMemo(() => {
    if (!torrent) return null
    if (!liveTorrent) return torrent
    return { ...torrent, ...liveTorrent }
  }, [torrent, liveTorrent])

  // Fetch torrent files
  // Bypass cache during recheck so progress bars update in real-time
  const currentState = displayTorrent?.state
  const isChecking = !!currentState && ["checkingDL", "checkingUP", "checkingResumeData"].includes(currentState)
  const { data: files, isLoading: loadingFiles } = useQuery({
    queryKey: ["torrent-files", instanceId, torrent?.hash],
    queryFn: () => api.getTorrentFiles(instanceId, torrent!.hash, { refresh: isChecking }),
    enabled: !!torrent && isReady && isContentTabActive,
    staleTime: 3000,
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
      const message = error instanceof Error ? error.message : t("details.failedToUpdatePriorities")
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
      hash: torrent.hash,
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
      toast.success(t("detailsPanel.toast.peersAdded"))
      setShowAddPeersDialog(false)
      setPeersToAdd("")
      queryClient.invalidateQueries({ queryKey: ["torrent-peers", instanceId, torrent?.hash] })
    },
    onError: (error) => {
      toast.error(t("detailsPanel.toast.peersFailed", { error: error.message }))
    },
  })

  // Ban peer mutation
  const banPeerMutation = useMutation({
    mutationFn: async (peer: string) => {
      await api.banPeers(instanceId, [peer])
    },
    onSuccess: () => {
      toast.success(t("detailsPanel.toast.peerBanned"))
      setShowBanPeerDialog(false)
      setPeerToBan(null)
      queryClient.invalidateQueries({ queryKey: ["torrent-peers", instanceId, torrent?.hash] })
    },
    onError: (error) => {
      toast.error(t("detailsPanel.toast.peerBanFailed", { error: error.message }))
    },
  })

  // Edit tracker mutation - for single torrent tracker URL editing
  const editTrackerMutation = useMutation({
    mutationFn: async ({ oldURL, newURL }: { oldURL: string; newURL: string }) => {
      if (!torrent) throw new Error("No torrent selected")
      await api.bulkAction(instanceId, {
        hashes: [torrent.hash],
        action: "editTrackers",
        trackerOldURL: oldURL,
        trackerNewURL: newURL,
      })
    },
    onSuccess: () => {
      toast.success(t("detailsPanel.toast.trackerUpdated"))
      setShowEditTrackerDialog(false)
      setTrackerToEdit(null)
      queryClient.invalidateQueries({ queryKey: ["torrent-trackers", instanceId, torrent?.hash] })
    },
    onError: (error: Error) => {
      toast.error(t("detailsPanel.toast.trackerUpdateFailed"), {
        description: error.message,
      })
    },
  })

  // Handle edit tracker click
  const handleEditTrackerClick = useCallback((tracker: TorrentTracker) => {
    setTrackerToEdit(tracker)
    setShowEditTrackerDialog(true)
  }, [])

  // Get tracker domain from URL for display
  const getTrackerDomain = useCallback((url: string): string => {
    try {
      return new URL(url).hostname
    } catch {
      return url
    }
  }, [])

  // Rename file state
  const [showRenameFileDialog, setShowRenameFileDialog] = useState(false)
  const [renameFilePath, setRenameFilePath] = useState<string | null>(null)

  // Rename file mutation
  const renameFileMutation = useMutation<void, unknown, { hash: string; oldPath: string; newPath: string }>({
    mutationFn: async ({ hash, oldPath, newPath }) => {
      await api.renameTorrentFile(instanceId, hash, oldPath, newPath)
    },
    onSuccess: async (_data, variables) => {
      toast.success(t("detailsPanel.toast.fileRenamed"))
      setShowRenameFileDialog(false)
      setRenameFilePath(null)
      // Small delay to let qBittorrent process the rename internally
      await new Promise(resolve => setTimeout(resolve, 500))
      // Invalidate to trigger refetch with fresh data
      await queryClient.invalidateQueries({ queryKey: ["torrent-files", instanceId, variables.hash] })
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : t("details.failedToRenameFile")
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
      toast.success(t("detailsPanel.toast.folderRenamed"))
      setShowRenameFolderDialog(false)
      setRenameFolderPath(null)
      // Small delay to let qBittorrent process the rename internally
      await new Promise(resolve => setTimeout(resolve, 500))
      // Invalidate to trigger refetch with fresh data
      await queryClient.invalidateQueries({ queryKey: ["torrent-files", instanceId, variables.hash] })
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : t("details.failedToRenameFolder")
      toast.error(message)
    },
  })

  const refreshTorrentFiles = useCallback(async () => {
    if (!torrent) return
    await queryClient.invalidateQueries({ queryKey: ["torrent-files", instanceId, torrent.hash] })
  }, [instanceId, queryClient, torrent])

  // Handle copy peer IP:port
  const handleCopyPeer = useCallback(async (peer: TorrentPeer) => {
    const peerAddress = `${peer.ip}:${peer.port}`
    try {
      await copyTextToClipboard(peerAddress)
      toast.success(t("detailsPanel.toast.copied", { type: t("peersTable.address") }))
    } catch (err) {
      console.error("Failed to copy to clipboard:", err)
      toast.error(t("detailsPanel.toast.copyFailed"))
    }
  }, [t])

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
            deleteFiles: deleteCrossSeedFiles,
          })
        )
      )

      toast.success(t("detailsPanel.toast.deletedTorrents", {
        count: torrentsToDelete.length,
        plural: torrentsToDelete.length > 1 ? "s" : "",
      }))

      // Refresh all instances
      for (const instId of byInstance.keys()) {
        queryClient.invalidateQueries({ queryKey: ["torrents", instId] })
      }

      setSelectedCrossSeedTorrents(new Set())
      setShowDeleteCrossSeedDialog(false)
    } catch (error) {
      toast.error(t("detailsPanel.toast.deleteFailed", {
        error: error instanceof Error ? error.message : t("detailsPanel.unknownError"),
      }))
    }
  }, [selectedCrossSeedTorrents, matchingTorrents, deleteCrossSeedFiles, queryClient, t])

  const handleDeleteCurrent = useCallback(async () => {
    if (!torrent) return

    try {
      await api.bulkAction(instanceId, {
        hashes: [torrent.hash],
        action: "delete",
        deleteFiles: deleteCurrentFiles,
      })

      toast.success(t("detailsPanel.toast.deletedTorrent", { name: torrent.name }))
      queryClient.invalidateQueries({ queryKey: ["torrents", instanceId] })
      setShowDeleteCurrentDialog(false)

      // Close the details panel by clearing selection (parent component should handle this)
      // The user will be returned to the torrent list
    } catch (error) {
      toast.error(t("detailsPanel.toast.deleteFailed", {
        error: error instanceof Error ? error.message : t("detailsPanel.unknownError"),
      }))
    }
  }, [torrent, instanceId, deleteCurrentFiles, queryClient, t])

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

  // Handle download content file
  const handleDownloadFile = useCallback((file: TorrentFile) => {
    if (!torrent || incognitoMode) return
    api.downloadContentFile(instanceId, torrent.hash, file.index)
  }, [instanceId, torrent, incognitoMode])

  const [showMediaInfoDialog, setShowMediaInfoDialog] = useState(false)
  const [mediaInfoFile, setMediaInfoFile] = useState<TorrentFile | null>(null)
  const [mediaInfoTorrentHash, setMediaInfoTorrentHash] = useState<string | null>(null)

  const handleShowMediaInfo = useCallback((file: TorrentFile) => {
    if (!torrent) return
    setMediaInfoFile(file)
    setMediaInfoTorrentHash(torrent.hash)
    setShowMediaInfoDialog(true)
  }, [torrent])

  const handleMediaInfoDialogOpenChange = useCallback((open: boolean) => {
    setShowMediaInfoDialog(open)
    if (!open) {
      setMediaInfoFile(null)
      setMediaInfoTorrentHash(null)
    }
  }, [])

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
        const parts = file.name.split("/").filter(Boolean)
        if (parts.length <= 1) return

        // Build all folder paths progressively
        let current = ""
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
        <div className="border-b flex items-center">
          <div className="flex-1 overflow-x-auto scroll-smooth">
            <TabsList className="w-full justify-start rounded-none h-8 bg-background px-4 sm:px-2 flex-nowrap">
              <TabsTrigger value="general" className="text-xs shrink-0">
                {t("detailsPanel.tabs.general")}
              </TabsTrigger>
              <TabsTrigger value="trackers" className="text-xs shrink-0">
                {t("detailsPanel.tabs.trackers")}
              </TabsTrigger>
              <TabsTrigger value="peers" className="text-xs shrink-0">
                {t("detailsPanel.tabs.peers")}
              </TabsTrigger>
              {hasWebseeds && (
                <TabsTrigger value="webseeds" className="text-xs shrink-0">
                  {t("detailsPanel.tabs.httpSources")}
                </TabsTrigger>
              )}
              <TabsTrigger value="content" className="text-xs shrink-0">
                {t("detailsPanel.tabs.content")}
              </TabsTrigger>
              <TabsTrigger value="crossseed" className="text-xs shrink-0">
                {t("detailsPanel.tabs.crossSeed")}
              </TabsTrigger>
            </TabsList>
          </div>
          {onClose && (
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-10 shrink-0 rounded-none"
              onClick={onClose}
              aria-label={t("detailsPanel.closePanel")}
            >
              <X className="h-3 w-3" />
            </Button>
          )}
        </div>


        <div className="flex-1 min-h-0 overflow-hidden">
          <TabsContent value="general" className="m-0 h-full">
            {isHorizontal ? (
              <GeneralTabHorizontal
                instanceId={instanceId}
                torrent={displayTorrent!}
                properties={properties}
                loading={loadingProperties}
                speedUnit={speedUnit}
                downloadLimit={properties?.dl_limit ?? displayTorrent!.dl_limit ?? 0}
                uploadLimit={properties?.up_limit ?? displayTorrent!.up_limit ?? 0}
                displayName={displayName}
                displaySavePath={displaySavePath || ""}
                displayTempPath={displayTempPath}
                tempPathEnabled={tempPathEnabled}
                displayInfohashV1={displayInfohashV1 || ""}
                displayInfohashV2={displayInfohashV2}
                displayComment={displayComment}
                displayCreatedBy={displayCreatedBy}
                queueingEnabled={metadata?.preferences?.queueing_enabled}
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
                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("detailsPanel.sections.generalInformation")}</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.torrentName")}</p>
                              <div className="flex items-center gap-2">
                                <p className="text-xs flex-1 break-all">{displayName || t("generalTab.na")}</p>
                                {displayName && (
                                  <Button
                                    variant="ghost"
                                    size="icon"
                                    className="h-8 w-8 shrink-0"
                                    onClick={() => copyToClipboard(displayName, t("detailsPanel.labels.torrentName"))}
                                  >
                                    <Copy className="h-3.5 w-3.5" />
                                  </Button>
                                )}
                              </div>
                            </div>

                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.infoHashV1")}</p>
                              <div className="flex items-center gap-2">
                                <p className="text-xs flex-1 break-all font-mono">{displayInfohashV1 || t("generalTab.na")}</p>
                                {displayInfohashV1 && (
                                  <Button
                                    variant="ghost"
                                    size="icon"
                                    className="h-8 w-8 shrink-0"
                                    onClick={() => copyToClipboard(displayInfohashV1, t("detailsPanel.labels.infoHashV1"))}
                                  >
                                    <Copy className="h-3.5 w-3.5" />
                                  </Button>
                                )}
                              </div>
                            </div>

                            {displayInfohashV2 && (
                              <div className="space-y-1">
                                <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.infoHashV2")}</p>
                                <div className="flex items-center gap-2">
                                  <p className="text-xs flex-1 break-all font-mono">{displayInfohashV2}</p>
                                  <Button
                                    variant="ghost"
                                    size="icon"
                                    className="h-8 w-8 shrink-0"
                                    onClick={() => copyToClipboard(displayInfohashV2, t("detailsPanel.labels.infoHashV2"))}
                                  >
                                    <Copy className="h-3.5 w-3.5" />
                                  </Button>
                                </div>
                              </div>
                            )}

                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.savePath")}</p>
                              <div className="flex items-center gap-2">
                                <p className="text-xs flex-1 break-all font-mono">{displaySavePath || t("generalTab.na")}</p>
                                {displaySavePath && (
                                  <Button
                                    variant="ghost"
                                    size="icon"
                                    className="h-8 w-8 shrink-0"
                                    onClick={() => copyToClipboard(displaySavePath, t("detailsPanel.labels.savePath"))}
                                  >
                                    <Copy className="h-3.5 w-3.5" />
                                  </Button>
                                )}
                              </div>
                            </div>

                            {tempPathEnabled && displayTempPath && (
                              <div className="space-y-1">
                                <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.downloadPath")}</p>
                                <div className="flex items-center gap-2">
                                  <p className="text-xs flex-1 break-all font-mono">{displayTempPath || t("generalTab.na")}</p>
                                  {displayTempPath && (
                                    <Button
                                      variant="ghost"
                                      size="icon"
                                      className="h-8 w-8 shrink-0"
                                      onClick={() => copyToClipboard(displayTempPath, t("detailsPanel.labels.downloadPath"))}
                                    >
                                      <Copy className="h-3.5 w-3.5" />
                                    </Button>
                                  )}
                                </div>
                              </div>
                            )}

                            {displayCreatedBy && (
                              <div className="space-y-1">
                                <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.createdBy")}</p>
                                <div className="text-xs">{renderTextWithLinks(displayCreatedBy)}</div>
                              </div>
                            )}

                            {displayComment && (
                              <div className="space-y-1">
                                <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.comment")}</p>
                                <div className="font-mono text-xs whitespace-pre-wrap break-words">{renderTextWithLinks(displayComment)}</div>
                              </div>
                            )}
                          </div>
                        </div>
                      </div>

                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("detailsPanel.sections.transferStatistics")}</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 space-y-4 border border-border/50">
                          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.totalSize")}</p>
                              <p className="text-lg font-semibold">{formatBytes(properties.total_size || torrent.size)}</p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.shareRatio")}</p>
                              <p className="text-lg font-semibold">{(properties.share_ratio || 0).toFixed(2)}</p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("generalTab.downloaded")}</p>
                              <p className="text-base font-medium">{formatBytes(properties.total_downloaded || 0)}</p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("generalTab.uploaded")}</p>
                              <p className="text-base font-medium">{formatBytes(properties.total_uploaded || 0)}</p>
                            </div>
                          </div>

                          <Separator className="opacity-50" />

                          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("generalTab.pieces")}</p>
                              <p className="text-sm font-medium">{properties.pieces_have || 0} / {properties.pieces_num || 0}</p>
                              <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.pieceSizeEach", { size: formatBytes(properties.piece_size || 0) })}</p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("generalTab.wasted")}</p>
                              <p className="text-sm font-medium">{formatBytes(properties.total_wasted || 0)}</p>
                            </div>
                          </div>
                        </div>
                      </div>

                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("detailsPanel.sections.speed")}</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.downloadSpeed")}</p>
                              <p className="text-base font-semibold text-green-500">{formatSpeedWithUnit(properties.dl_speed || 0, speedUnit)}</p>
                              <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.average", { value: formatSpeedWithUnit(properties.dl_speed_avg || 0, speedUnit) })}</p>
                              <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.limit", { value: downloadLimitLabel })}</p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.uploadSpeed")}</p>
                              <p className="text-base font-semibold text-blue-500">{formatSpeedWithUnit(properties.up_speed || 0, speedUnit)}</p>
                              <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.average", { value: formatSpeedWithUnit(properties.up_speed_avg || 0, speedUnit) })}</p>
                              <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.limit", { value: uploadLimitLabel })}</p>
                            </div>
                          </div>
                        </div>
                      </div>

                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("generalTab.network")}</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                          <div className="grid grid-cols-2 gap-4">
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("generalTab.seeds")}</p>
                              <p className="text-base font-semibold">{properties.seeds || 0} <span className="text-sm font-normal text-muted-foreground">/ {properties.seeds_total || 0}</span></p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("generalTab.peers")}</p>
                              <p className="text-base font-semibold">{properties.peers || 0} <span className="text-sm font-normal text-muted-foreground">/ {properties.peers_total || 0}</span></p>
                            </div>
                          </div>
                        </div>
                      </div>

                      {metadata?.preferences?.queueing_enabled && (
                        <div className="space-y-3">
                          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("detailsPanel.sections.queueManagement")}</h3>
                          <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50 space-y-3">
                            <div className="flex items-center justify-between">
                              <span className="text-sm text-muted-foreground">{t("detailsPanel.labels.priority")}</span>
                              <div className="flex items-center gap-2">
                                <span className="text-sm font-semibold">
                                  {displayTorrent?.priority && displayTorrent.priority > 0 ? displayTorrent.priority : t("detailsPanel.values.normal")}
                                </span>
                                {(displayTorrent?.state === "queuedDL" || displayTorrent?.state === "queuedUP") && (
                                  <Badge variant="secondary" className="text-xs">
                                    {t("detailsPanel.values.queued", { state: displayTorrent.state === "queuedDL" ? "DL" : "UP" })}
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
                                      <p className="text-muted-foreground">{t("detailsPanel.labels.maxDownloads")}</p>
                                      <p className="font-medium">{metadata.preferences.max_active_downloads}</p>
                                    </div>
                                  )}
                                  {metadata.preferences.max_active_uploads > 0 && (
                                    <div className="space-y-1">
                                      <p className="text-muted-foreground">{t("detailsPanel.labels.maxUploads")}</p>
                                      <p className="font-medium">{metadata.preferences.max_active_uploads}</p>
                                    </div>
                                  )}
                                  {metadata.preferences.max_active_torrents > 0 && (
                                    <div className="space-y-1">
                                      <p className="text-muted-foreground">{t("detailsPanel.labels.maxActive")}</p>
                                      <p className="font-medium">{metadata.preferences.max_active_torrents}</p>
                                    </div>
                                  )}
                                </div>
                              </>
                            )}
                          </div>
                        </div>
                      )}

                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("detailsPanel.sections.timeInformation")}</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                          <div className="grid grid-cols-2 gap-4">
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("generalTab.timeActive")}</p>
                              <p className="text-sm font-medium">{formatDuration(properties.time_elapsed || 0)}</p>
                            </div>
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("generalTab.seedingTime")}</p>
                              <p className="text-sm font-medium">{formatDuration(properties.seeding_time || 0)}</p>
                            </div>
                          </div>
                        </div>
                      </div>

                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("detailsPanel.sections.timestamps")}</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                            <div className="space-y-1">
                              <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.added")}</p>
                              <p className="text-sm">{formatTimestamp(properties.addition_date, true)}</p>
                            </div>
                            {properties.completion_date && properties.completion_date !== -1 && (
                              <div className="space-y-1">
                                <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.completed")}</p>
                                <p className="text-sm">{formatTimestamp(properties.completion_date, true)}</p>
                              </div>
                            )}
                            {properties.creation_date && properties.creation_date !== -1 && (
                              <div className="space-y-1">
                                <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.created")}</p>
                                <p className="text-sm">{formatTimestamp(properties.creation_date, true)}</p>
                              </div>
                            )}
                          </div>
                        </div>
                      </div>
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
                onEditTracker={handleEditTrackerClick}
                supportsTrackerEditing={supportsTrackerEditing}
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
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("detailsPanel.sections.activeTrackers")}</h3>
                        <span className="text-xs text-muted-foreground">{t("detailsPanel.counts.trackers", { count: trackers.length })}</span>
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
                              <TrackerContextMenu
                                key={index}
                                tracker={tracker}
                                onEditTracker={handleEditTrackerClick}
                                supportsTrackerEditing={supportsTrackerEditing}
                              >
                                <div
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
                                      <p className="text-xs text-muted-foreground">{t("trackersTable.seeds")}</p>
                                      <p className="text-sm font-medium">{tracker.num_seeds}</p>
                                    </div>
                                    <div className="space-y-1">
                                      <p className="text-xs text-muted-foreground">{t("generalTab.peers")}</p>
                                      <p className="text-sm font-medium">{tracker.num_peers}</p>
                                    </div>
                                    <div className="space-y-1">
                                      <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.leechers")}</p>
                                      <p className="text-sm font-medium">{tracker.num_leeches}</p>
                                    </div>
                                    <div className="space-y-1">
                                      <p className="text-xs text-muted-foreground">{t("trackersTable.downloaded")}</p>
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
                              </TrackerContextMenu>
                            )
                          })}
                      </div>
                    </div>
                  ) : (
                    <div className="flex items-center justify-center h-32 text-sm text-muted-foreground">
                      {t("trackersTable.noTrackersFound")}
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
                    {t("detailsPanel.counts.connectedPeers", { count: peersData?.sorted_peers?.length ?? 0 })}
                  </span>
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-6 text-xs"
                    onClick={() => setShowAddPeersDialog(true)}
                  >
                    <UserPlus className="h-3 w-3 mr-1.5" />
                    {t("detailsPanel.addPeers.title")}
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
                          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("detailsPanel.sections.connectedPeers")}</h3>
                          <p className="text-xs text-muted-foreground mt-1">{t("detailsPanel.counts.connectedPeers", { count: Object.keys(peersData.peers).length })}</p>
                        </div>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setShowAddPeersDialog(true)}
                        >
                          <UserPlus className="h-4 w-4 mr-2" />
                          {t("detailsPanel.addPeers.title")}
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
                                          <Badge variant="secondary" className="text-xs">{t("detailsPanel.values.seeder")}</Badge>
                                        )}
                                      </div>
                                      <p className="text-xs text-muted-foreground">{peer.client || t("detailsPanel.values.unknownClient")}</p>
                                    </div>
                                  </div>

                                  <Separator className="opacity-50" />

                                  {/* Progress Bar */}
                                  <div className="space-y-1">
                                    <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.peerProgress")}</p>
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
                                      <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.downloadSpeed")}</p>
                                      <p className={`text-sm font-medium ${peer.dl_speed && peer.dl_speed > 0 ? "text-green-500" : ""}`}>
                                        {formatSpeedWithUnit(peer.dl_speed || 0, speedUnit)}
                                      </p>
                                    </div>
                                    <div className="space-y-1">
                                      <p className="text-xs text-muted-foreground">{t("detailsPanel.labels.uploadSpeed")}</p>
                                      <p className={`text-sm font-medium ${peer.up_speed && peer.up_speed > 0 ? "text-blue-500" : ""}`}>
                                        {formatSpeedWithUnit(peer.up_speed || 0, speedUnit)}
                                      </p>
                                    </div>
                                  </div>

                                  {/* Data Transfer Info */}
                                  <div className="grid grid-cols-2 gap-3 text-xs">
                                    <div className="space-y-1">
                                      <p className="text-muted-foreground">{t("generalTab.downloaded")}</p>
                                      <p className="font-medium">{formatBytes(peer.downloaded || 0)}</p>
                                    </div>
                                    <div className="space-y-1">
                                      <p className="text-muted-foreground">{t("generalTab.uploaded")}</p>
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
                                            <span className="opacity-70">{t("detailsPanel.labels.connection")}</span> {peer.connection}
                                          </div>
                                        )}
                                        {hasFlagDetails && (
                                          <div className="flex items-center gap-2">
                                            <span className="opacity-70">{t("detailsPanel.labels.flags")}</span>
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
                                                      aria-label={t("detailsPanel.peer.flagLabel", { flag })}
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
                                  {t("detailsPanel.actions.copyIpPort")}
                                </ContextMenuItem>
                                <ContextMenuSeparator />
                                <ContextMenuItem
                                  onClick={() => handleBanPeerClick(peer)}
                                  className="text-destructive focus:text-destructive"
                                >
                                  <Ban className="h-4 w-4 mr-2" />
                                  {t("detailsPanel.actions.banPeerPermanently")}
                                </ContextMenuItem>
                              </ContextMenuContent>
                            </ContextMenu>
                          )
                        })}
                      </div>
                    </div>
                  ) : (
                    <div className="flex flex-col items-center justify-center h-32 text-sm text-muted-foreground gap-3">
                      <p>{t("peersTable.noPeersConnected")}</p>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setShowAddPeersDialog(true)}
                      >
                        <UserPlus className="h-4 w-4 mr-2" />
                        {t("detailsPanel.addPeers.title")}
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
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("webSeedsTable.httpSources", { count: webseedsData.length, plural: webseedsData.length === 1 ? "" : "s" })}</h3>
                        <p className="text-xs text-muted-foreground mt-1">{t("detailsPanel.counts.httpSources", { count: webseedsData.length })}</p>
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
                                    toast.success(t("webSeedsTable.toast.urlCopied"))
                                  }
                                }}
                                disabled={incognitoMode}
                              >
                                <Copy className="h-3.5 w-3.5 mr-2" />
                                {t("webSeedsTable.copyUrl")}
                              </ContextMenuItem>
                            </ContextMenuContent>
                          </ContextMenu>
                        ))}
                      </div>
                    </div>
                  ) : (
                    <div className="flex items-center justify-center h-32 text-sm text-muted-foreground">
                      {t("webSeedsTable.noHttpSources")}
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
                onDownloadFile={hasLocalFilesystemAccess ? handleDownloadFile : undefined}
                onShowMediaInfo={hasLocalFilesystemAccess ? handleShowMediaInfo : undefined}
              />
            ) : activeTab === "content" && loadingFiles && !files ? (
              <div className="flex items-center justify-center p-8 flex-1">
                <Loader2 className="h-6 w-6 animate-spin" />
              </div>
            ) : files && files.length > 0 ? (
              <>
                <div className="flex items-start justify-between gap-3 px-4 sm:px-6 py-3 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
                  <div>
                    <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("detailsPanel.sections.fileContents")}</h3>
                    <span className="text-xs text-muted-foreground">
                      {supportsFilePriority? t("detailsPanel.counts.selectedFiles", { selected: selectedFileCount, total: totalFiles }): t("detailsPanel.counts.files", { count: files.length })}
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
                          {t("detailsPanel.actions.selectAll")}
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 px-2 text-xs"
                          onClick={handleDeselectAllFiles}
                          disabled={!canDeselectAll || setFilePriorityMutation.isPending}
                        >
                          {t("detailsPanel.actions.selectNone")}
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
                      onDownloadFile={hasLocalFilesystemAccess ? handleDownloadFile : undefined}
                      onShowMediaInfo={hasLocalFilesystemAccess ? handleShowMediaInfo : undefined}
                    />
                  </div>
                </ScrollArea>
              </>
            ) : (
              <div className="flex items-center justify-center h-32 text-sm text-muted-foreground">
                {t("detailsPanel.emptyStates.noFilesFound")}
              </div>
            )}
          </TabsContent>

          <TabsContent value="crossseed" className="m-0 h-full">
            {isHorizontal ? (
              <CrossSeedTable
                matches={matchingTorrents}
                loading={isLoadingMatches}
                incognitoMode={incognitoMode}
                selectedTorrents={selectedCrossSeedTorrents}
                onToggleSelection={handleToggleCrossSeedSelection}
                onSelectAll={handleSelectAllCrossSeed}
                onDeselectAll={handleDeselectAllCrossSeed}
                onDeleteMatches={() => setShowDeleteCrossSeedDialog(true)}
                onDeleteCurrent={() => setShowDeleteCurrentDialog(true)}
                instanceById={instanceById}
                onNavigateToTorrent={onNavigateToTorrent}
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
                            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("detailsPanel.crossSeed.title")}</h3>
                            {isLoadingMatches && (
                              <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
                            )}
                          </div>
                          <span className="text-xs text-muted-foreground">
                            {selectedCrossSeedTorrents.size > 0? t("detailsPanel.crossSeed.selectedSummary", { selected: selectedCrossSeedTorrents.size, total: matchingTorrents.length }): isLoadingMatches? t("detailsPanel.crossSeed.loadingSummary", { count: matchingTorrents.length }): t("detailsPanel.crossSeed.loadedSummary", { count: matchingTorrents.length })}
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
                                {t("detailsPanel.deselectAll")}
                              </Button>
                              <Button
                                variant="destructive"
                                size="sm"
                                onClick={() => setShowDeleteCrossSeedDialog(true)}
                              >
                                <Trash2 className="h-4 w-4 mr-2" />
                                {t("detailsPanel.crossSeed.deleteMatches", { count: selectedCrossSeedTorrents.size })}
                              </Button>
                            </>
                          ) : (
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={handleSelectAllCrossSeed}
                            >
                              {t("detailsPanel.selectAll")}
                            </Button>
                          )}
                          <Button
                            variant="destructive"
                            size="sm"
                            onClick={() => setShowDeleteCurrentDialog(true)}
                          >
                            <Trash2 className="h-4 w-4 mr-2" />
                            {t("detailsPanel.crossSeed.deleteThisTorrent")}
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
                          let statusLabel = getStateLabel(match.state, t)
                          let statusVariant: "default" | "secondary" | "destructive" | "outline" = "outline"
                          let statusClass = ""

                          // Check tracker health first (if supported)
                          if (trackerHealth === "unregistered") {
                            statusLabel = t("crossSeedTable.statusLabels.unregistered")
                            statusVariant = "outline"
                            statusClass = "text-destructive border-destructive/40 bg-destructive/10"
                          } else if (trackerHealth === "tracker_down") {
                            statusLabel = t("crossSeedTable.statusLabels.trackerDown")
                            statusVariant = "outline"
                            statusClass = "text-yellow-500 border-yellow-500/40 bg-yellow-500/10"
                          } else if (trackerHealth === "tracker_error") {
                            statusLabel = t("crossSeedTable.statusLabels.trackerError")
                            statusVariant = "outline"
                            statusClass = "text-orange-500 border-orange-500/40 bg-orange-500/10"
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
                          const matchType = match.matchType as "infohash" | "content_path" | "save_path" | "name"
                          const matchLabel = matchType === "infohash"? t("detailsPanel.crossSeed.infoHashMatch"): matchType === "content_path"? t("crossSeedTable.matchTypes.contentPath.label"): matchType === "save_path"? t("detailsPanel.crossSeed.savePathMatch"): t("crossSeedTable.matchTypes.name.label")
                          const matchDescription = matchType === "infohash"? t("detailsPanel.crossSeed.infoHashDescription"): matchType === "content_path"? t("crossSeedTable.matchTypes.contentPath.description"): matchType === "save_path"? t("detailsPanel.crossSeed.savePathDescription"): t("crossSeedTable.matchTypes.name.description")

                          return (
                            <div
                              key={torrentKey}
                              className={cn(
                                "rounded-lg border bg-card p-4 space-y-3",
                                onNavigateToTorrent && "cursor-pointer hover:bg-muted/30"
                              )}
                              onClick={(e) => {
                                // Don't navigate if clicking checkbox
                                if ((e.target as HTMLElement).closest("[role=\"checkbox\"]")) return
                                onNavigateToTorrent?.(match.instanceId, match.hash)
                              }}
                            >
                              <div className="space-y-2">
                                <div className="flex items-start gap-3">
                                  <Checkbox
                                    checked={isSelected}
                                    onCheckedChange={() => handleToggleCrossSeedSelection(torrentKey)}
                                    className="mt-0.5 shrink-0"
                                    aria-label={t("detailsPanel.crossSeed.selectTorrent", { name: displayName })}
                                  />
                                  <div className="flex-1 min-w-0 space-y-1">
                                    <div className="flex items-start gap-2">
                                      <p className="text-sm font-medium break-words" title={displayName}>{displayName}</p>
                                      {isHardlinkManaged(match, instanceById.get(match.instanceId)) && (
                                        <Tooltip>
                                          <TooltipTrigger asChild>
                                            <Badge variant="outline" className="text-[9px] px-1 py-0 shrink-0 text-blue-500 border-blue-500/40">
                                              {t("crossSeedTable.hardlink")}
                                            </Badge>
                                          </TooltipTrigger>
                                          <TooltipContent>
                                            <p className="text-xs">{t("crossSeedTable.hardlinkTooltip")}</p>
                                          </TooltipContent>
                                        </Tooltip>
                                      )}
                                    </div>
                                    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
                                      <span className="shrink-0">{t("crossSeedTable.instance")}: {match.instanceName}</span>
                                      <span className="shrink-0">•</span>
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <span className="cursor-help underline decoration-dotted shrink-0">
                                            {t("crossSeedTable.match")}: {matchLabel}
                                          </span>
                                        </TooltipTrigger>
                                        <TooltipContent>
                                          <p>{matchDescription}</p>
                                        </TooltipContent>
                                      </Tooltip>
                                      {trackerHostname && (
                                        <>
                                          <span className="shrink-0">•</span>
                                          <span className="break-all">{t("crossSeedTable.tracker")}: {incognitoMode ? getLinuxTracker(`${match.hash}-0`) : trackerHostname}</span>
                                        </>
                                      )}
                                      {match.category && (
                                        <>
                                          <span className="shrink-0">•</span>
                                          <span className="break-all">{t("tableColumns.category")}: {incognitoMode ? getLinuxCategory(match.hash) : match.category}</span>
                                        </>
                                      )}
                                      {match.tags && (
                                        <>
                                          <span className="shrink-0">•</span>
                                          <span className="break-all">{t("tableColumns.tags")}: {incognitoMode ? getLinuxTags(match.hash) : match.tags}</span>
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
                            {t("detailsPanel.crossSeed.checkingMoreInstances")}
                          </span>
                        </div>
                      )}
                    </div>
                  ) : (
                    <div className="flex items-center justify-center h-32 text-sm text-muted-foreground">
                      {t("crossSeedTable.noMatches")}
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
            <DialogTitle>{t("detailsPanel.addPeers.title")}</DialogTitle>
            <DialogDescription>
              {t("detailsPanel.addPeers.description")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="peers">{t("detailsPanel.addPeers.label")}</Label>
              <Textarea
                id="peers"
                className="min-h-[100px]"
                placeholder={t("detailsPanel.addPeers.placeholder")}
                value={peersToAdd}
                onChange={(e) => setPeersToAdd(e.target.value)}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowAddPeersDialog(false)}>
              {t("common:actions.cancel")}
            </Button>
            <Button
              onClick={handleAddPeersSubmit}
              disabled={!peersToAdd.trim() || addPeersMutation.isPending}
            >
              {addPeersMutation.isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              {t("detailsPanel.addPeers.title")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Ban Peer Confirmation Dialog */}
      <Dialog open={showBanPeerDialog} onOpenChange={setShowBanPeerDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("detailsPanel.banPeerPermanent.title")}</DialogTitle>
            <DialogDescription>
              {t("detailsPanel.banPeerPermanent.description")}
            </DialogDescription>
          </DialogHeader>
          {peerToBan && (
            <div className="space-y-2 text-sm">
              <div>
                <span className="text-muted-foreground">{t("detailsPanel.banPeerPermanent.ipAddress")}</span>
                <span className="ml-2 font-mono">{peerToBan.ip}:{peerToBan.port}</span>
              </div>
              {peerToBan.client && (
                <div>
                  <span className="text-muted-foreground">{t("peersTable.client")}:</span>
                  <span className="ml-2">{peerToBan.client}</span>
                </div>
              )}
              {peerToBan.country && (
                <div>
                  <span className="text-muted-foreground">{t("detailsPanel.banPeerPermanent.country")}</span>
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
              {t("common:actions.cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={handleBanPeerConfirm}
              disabled={banPeerMutation.isPending}
            >
              {banPeerMutation.isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              {t("peersTable.banPeer")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Cross-Seed Torrents Dialog */}
      <Dialog open={showDeleteCrossSeedDialog} onOpenChange={setShowDeleteCrossSeedDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("detailsPanel.deleteSelected.title")}</DialogTitle>
            <DialogDescription>
              {t("detailsPanel.deleteSelected.description", { count: selectedCrossSeedTorrents.size })}
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
                {t("deleteFilesPreference.label")}
              </Label>
            </div>
            <div className="text-sm text-muted-foreground">
              {deleteCrossSeedFiles ? (
                <p className="text-destructive">{t("detailsPanel.deleteWarnings.deleteFiles")}</p>
              ) : (
                <p>{t("detailsPanel.deleteWarnings.keepFilesPlural")}</p>
              )}
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setShowDeleteCrossSeedDialog(false)}
            >
              {t("common:actions.cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={handleDeleteCrossSeed}
            >
              <Trash2 className="h-4 w-4 mr-2" />
              {t("detailsPanel.deleteSelected.confirm", { count: selectedCrossSeedTorrents.size })}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Current Torrent Dialog */}
      <Dialog open={showDeleteCurrentDialog} onOpenChange={setShowDeleteCurrentDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("detailsPanel.crossSeed.deleteThisTorrent")}</DialogTitle>
            <DialogDescription>
              {t("detailsPanel.deleteCurrent.description", { name: incognitoMode ? getLinuxFileName(torrent?.hash ?? "", 0) : torrent?.name })}
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
                {t("deleteFilesPreference.label")}
              </Label>
            </div>
            <div className="text-sm text-muted-foreground">
              {deleteCurrentFiles ? (
                <p className="text-destructive">{t("detailsPanel.deleteWarnings.deleteFiles")}</p>
              ) : (
                <p>{t("detailsPanel.deleteWarnings.keepFilesSingle")}</p>
              )}
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setShowDeleteCurrentDialog(false)}
            >
              {t("common:actions.cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={handleDeleteCurrent}
            >
              <Trash2 className="h-4 w-4 mr-2" />
              {t("detailsPanel.deleteCurrent.confirm")}
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

      {/* Edit Tracker Dialog */}
      <EditTrackerDialog
        open={showEditTrackerDialog}
        onOpenChange={setShowEditTrackerDialog}
        instanceId={instanceId}
        tracker={trackerToEdit ? getTrackerDomain(trackerToEdit.url) : ""}
        trackerURLs={trackerToEdit ? [trackerToEdit.url] : []}
        selectedHashes={torrent ? [torrent.hash] : []}
        onConfirm={(oldURL, newURL) => editTrackerMutation.mutate({ oldURL, newURL })}
        isPending={editTrackerMutation.isPending}
      />

      <TorrentFileMediaInfoDialog
        open={showMediaInfoDialog}
        onOpenChange={handleMediaInfoDialogOpenChange}
        instanceId={instanceId}
        torrentHash={mediaInfoTorrentHash ?? ""}
        file={mediaInfoFile}
      />
    </div>
  )
});
