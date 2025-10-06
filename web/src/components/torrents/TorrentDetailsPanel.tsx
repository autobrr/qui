/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { Progress } from "@/components/ui/progress"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { api } from "@/lib/api"
import { getLinuxComment, getLinuxCreatedBy, getLinuxFileName, getLinuxHash, getLinuxIsoName, getLinuxSavePath, getLinuxTracker, useIncognitoMode } from "@/lib/incognito"
import { renderTextWithLinks } from "@/lib/linkUtils"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import { resolveTorrentHashes } from "@/lib/torrent-utils"
import { copyTextToClipboard, formatBytes, formatDuration } from "@/lib/utils"
import type { Torrent } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import "flag-icons/css/flag-icons.min.css"
import { Ban, Copy, Loader2, UserPlus } from "lucide-react"
import { memo, useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

interface TorrentPeer {
  ip: string
  port: number
  connection?: string
  flags?: string
  flags_desc?: string
  client?: string
  progress?: number  // Float 0-1, where 1 = 100% (seeder). Note: qBittorrent doesn't expose the actual seed status via API
  dl_speed?: number
  up_speed?: number
  downloaded?: number
  uploaded?: number
  relevance?: number
  files?: string
  country?: string
  country_code?: string
  peer_id_client?: string
}

interface SortedPeer extends TorrentPeer {
  key: string
}

interface TorrentPeersResponse {
  full_update?: boolean
  rid?: number
  peers?: Record<string, TorrentPeer>
  peers_removed?: string[]
  show_flags?: boolean
  sorted_peers?: SortedPeer[]
}

interface TorrentDetailsPanelProps {
  instanceId: number;
  torrent: Torrent | null;
}

function getTrackerStatusBadge(status: number, t: (key: string) => string) {
  switch (status) {
    case 0:
      return <Badge variant="secondary">{t("torrent_details.trackers.status.disabled")}</Badge>
    case 1:
      return <Badge variant="secondary">{t("torrent_details.trackers.status.not_contacted")}</Badge>
    case 2:
      return <Badge variant="default">{t("torrent_details.trackers.status.working")}</Badge>
    case 3:
      return <Badge variant="default">{t("torrent_details.trackers.status.updating")}</Badge>
    case 4:
      return <Badge variant="destructive">{t("torrent_details.trackers.status.error")}</Badge>
    default:
      return <Badge variant="outline">{t("torrent_details.trackers.status.unknown")}</Badge>
  }
}

export const TorrentDetailsPanel = memo(function TorrentDetailsPanel({ instanceId, torrent }: TorrentDetailsPanelProps) {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState("general")
  const [showAddPeersDialog, setShowAddPeersDialog] = useState(false)
  const { formatTimestamp } = useDateTimeFormatters()
  const [showBanPeerDialog, setShowBanPeerDialog] = useState(false)
  const [peersToAdd, setPeersToAdd] = useState("")
  const [peerToBan, setPeerToBan] = useState<TorrentPeer | null>(null)
  const [isReady, setIsReady] = useState(false)
  const { data: metadata } = useInstanceMetadata(instanceId)
  const queryClient = useQueryClient()
  const [speedUnit] = useSpeedUnits()
  const [incognitoMode] = useIncognitoMode()
  const displayName = incognitoMode ? getLinuxIsoName(torrent?.hash ?? "") : torrent?.name
  const incognitoHash = incognitoMode && torrent?.hash ? getLinuxHash(torrent.hash) : undefined

  const copyToClipboard = useCallback(async (text: string, type: string) => {
    try {
      await copyTextToClipboard(text)
      toast.success(t("torrent_details.toasts.copied_to_clipboard", { type }))
    } catch {
      toast.error(t("torrent_details.toasts.copy_failed"))
    }
  }, [t])
  // Reset tab when torrent changes and wait for component to be ready
  useEffect(() => {
    setActiveTab("general")
    setIsReady(false)
    // Small delay to ensure parent component animations complete
    const timer = setTimeout(() => setIsReady(true), 150)
    return () => clearTimeout(timer)
  }, [torrent?.hash])

  // Fetch torrent properties
  const { data: properties, isLoading: loadingProperties } = useQuery({
    queryKey: ["torrent-properties", instanceId, torrent?.hash],
    queryFn: () => api.getTorrentProperties(instanceId, torrent!.hash),
    enabled: !!torrent && isReady,
    staleTime: 30000, // Cache for 30 seconds
    gcTime: 5 * 60 * 1000, // Keep in cache for 5 minutes
  })

  const { infohashV1: resolvedInfohashV1, infohashV2: resolvedInfohashV2 } = resolveTorrentHashes(properties as { hash?: string; infohash_v1?: string; infohash_v2?: string } | undefined, torrent ?? undefined)

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
    enabled: !!torrent && isReady, // Fetch immediately, don't wait for tab
    staleTime: 30000,
    gcTime: 5 * 60 * 1000,
  })

  // Fetch torrent peers with optimized refetch
  const isPeersTabActive = activeTab === "peers"
  const peersQueryKey = ["torrent-peers", instanceId, torrent?.hash] as const

  const { data: peersData, isLoading: loadingPeers } = useQuery<TorrentPeersResponse>({
    queryKey: peersQueryKey,
    queryFn: async () => {
      const data = await api.getTorrentPeers(instanceId, torrent!.hash)
      return data as TorrentPeersResponse
    },
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

  // Add peers mutation
  const addPeersMutation = useMutation({
    mutationFn: async (peers: string[]) => {
      if (!torrent) throw new Error("No torrent selected")
      await api.addPeersToTorrents(instanceId, [torrent.hash], peers)
    },
    onSuccess: () => {
      toast.success(t("torrent_details.toasts.add_peers_success"))
      setShowAddPeersDialog(false)
      setPeersToAdd("")
      queryClient.invalidateQueries({ queryKey: ["torrent-peers", instanceId, torrent?.hash] })
    },
    onError: (error) => {
      toast.error(t("torrent_details.toasts.add_peers_failed", { error: error.message }))
    },
  })

  // Ban peer mutation
  const banPeerMutation = useMutation({
    mutationFn: async (peer: string) => {
      await api.banPeers(instanceId, [peer])
    },
    onSuccess: () => {
      toast.success(t("torrent_details.toasts.ban_peer_success"))
      setShowBanPeerDialog(false)
      setPeerToBan(null)
      queryClient.invalidateQueries({ queryKey: ["torrent-peers", instanceId, torrent?.hash] })
    },
    onError: (error) => {
      toast.error(t("torrent_details.toasts.ban_peer_failed", { error: error.message }))
    },
  })

  // Handle copy peer IP:port
  const handleCopyPeer = useCallback(async (peer: TorrentPeer) => {
    const peerAddress = `${peer.ip}:${peer.port}`
    try {
      await copyTextToClipboard(peerAddress)
      toast.success(t("torrent_details.toasts.copied_peer", { peer: peerAddress }))
    } catch (err) {
      console.error("Failed to copy to clipboard:", err)
      toast.error(t("torrent_details.toasts.copy_failed"))
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

  if (!torrent) return null

  const displayCreatedBy = incognitoMode && properties?.created_by ? getLinuxCreatedBy(torrent.hash) : properties?.created_by
  const displayComment = incognitoMode && properties?.comment ? getLinuxComment(torrent.hash) : properties?.comment
  const displayInfohashV1 = incognitoMode && resolvedInfohashV1 ? incognitoHash : resolvedInfohashV1
  const displayInfohashV2 = incognitoMode && resolvedInfohashV2 ? incognitoHash : resolvedInfohashV2
  const displaySavePath = incognitoMode && properties?.save_path ? getLinuxSavePath(torrent.hash) : properties?.save_path

  const formatLimitLabel = (limit: number | null | undefined) => {
    if (limit == null || !Number.isFinite(limit) || limit <= 0) {
      return "∞"
    }
    return formatSpeedWithUnit(limit, speedUnit)
  }

  const downloadLimitLabel = formatLimitLabel(properties?.dl_limit ?? torrent.dl_limit)
  const uploadLimitLabel = formatLimitLabel(properties?.up_limit ?? torrent.up_limit)

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
      <div className="flex items-center justify-between px-4 py-3 sm:px-6 border-b bg-muted/30">
        <h3 className="text-sm font-semibold truncate flex-1 pr-2" title={displayName}>
          {displayName}
        </h3>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab} className="flex-1 flex flex-col overflow-hidden">
        <TabsList className="w-full justify-start rounded-none border-b h-10 bg-background px-4 sm:px-6 py-0">
          <TabsTrigger
            value="general"
            className="relative text-xs rounded-none data-[state=active]:bg-transparent data-[state=active]:shadow-none hover:bg-accent/50 transition-all px-3 sm:px-4 cursor-pointer focus-visible:outline-none focus-visible:ring-0 after:absolute after:bottom-0 after:left-0 after:right-0 after:h-[2px] after:bg-primary after:scale-x-0 data-[state=active]:after:scale-x-100 after:transition-transform"
          >
            {t("torrent_details.tabs.general")}
          </TabsTrigger>
          <TabsTrigger
            value="trackers"
            className="relative text-xs rounded-none data-[state=active]:bg-transparent data-[state=active]:shadow-none hover:bg-accent/50 transition-all px-3 sm:px-4 cursor-pointer focus-visible:outline-none focus-visible:ring-0 after:absolute after:bottom-0 after:left-0 after:right-0 after:h-[2px] after:bg-primary after:scale-x-0 data-[state=active]:after:scale-x-100 after:transition-transform"
          >
            {t("torrent_details.tabs.trackers")}
          </TabsTrigger>
          <TabsTrigger
            value="peers"
            className="relative text-xs rounded-none data-[state=active]:bg-transparent data-[state=active]:shadow-none hover:bg-accent/50 transition-all px-3 sm:px-4 cursor-pointer focus-visible:outline-none focus-visible:ring-0 after:absolute after:bottom-0 after:left-0 after:right-0 after:h-[2px] after:bg-primary after:scale-x-0 data-[state=active]:after:scale-x-100 after:transition-transform"
          >
            {t("torrent_details.tabs.peers")}
          </TabsTrigger>
          <TabsTrigger
            value="content"
            className="relative text-xs rounded-none data-[state=active]:bg-transparent data-[state=active]:shadow-none hover:bg-accent/50 transition-all px-3 sm:px-4 cursor-pointer focus-visible:outline-none focus-visible:ring-0 after:absolute after:bottom-0 after:left-0 after:right-0 after:h-[2px] after:bg-primary after:scale-x-0 data-[state=active]:after:scale-x-100 after:transition-transform"
          >
            {t("torrent_details.tabs.content")}
          </TabsTrigger>
        </TabsList>

        <div className="flex-1 overflow-hidden">
          <TabsContent value="general" className="m-0 h-full">
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
                      <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("torrent_details.general.transfer_stats.title")}</h3>
                      <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 space-y-4 border border-border/50">
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.transfer_stats.total_size")}</p>
                            <p className="text-lg font-semibold">{formatBytes(properties.total_size || torrent.size)}</p>
                          </div>
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.transfer_stats.share_ratio")}</p>
                            <p className="text-lg font-semibold">{(properties.share_ratio || 0).toFixed(2)}</p>
                          </div>
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.transfer_stats.downloaded")}</p>
                            <p className="text-base font-medium">{formatBytes(properties.total_downloaded || 0)}</p>
                          </div>
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.transfer_stats.uploaded")}</p>
                            <p className="text-base font-medium">{formatBytes(properties.total_uploaded || 0)}</p>
                          </div>
                        </div>

                        <Separator className="opacity-50" />

                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.transfer_stats.pieces")}</p>
                            <p className="text-sm font-medium">{properties.pieces_have || 0} / {properties.pieces_num || 0}</p>
                            <p className="text-xs text-muted-foreground">({formatBytes(properties.piece_size || 0)} {t("torrent_details.general.transfer_stats.each")})</p>
                          </div>
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.transfer_stats.wasted")}</p>
                            <p className="text-sm font-medium">{formatBytes(properties.total_wasted || 0)}</p>
                          </div>
                        </div>
                      </div>
                    </div>

                    {/* Speed Section */}
                    <div className="space-y-3">
                      <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("torrent_details.general.speed.title")}</h3>
                      <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.speed.download")}</p>
                            <p className="text-base font-semibold text-green-500">{formatSpeedWithUnit(properties.dl_speed || 0, speedUnit)}</p>
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.speed.avg")}: {formatSpeedWithUnit(properties.dl_speed_avg || 0, speedUnit)}</p>
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.speed.limit")}: {downloadLimitLabel}</p>
                          </div>
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.speed.upload")}</p>
                            <p className="text-base font-semibold text-blue-500">{formatSpeedWithUnit(properties.up_speed || 0, speedUnit)}</p>
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.speed.avg")}: {formatSpeedWithUnit(properties.up_speed_avg || 0, speedUnit)}</p>
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.speed.limit")}: {uploadLimitLabel}</p>
                          </div>
                        </div>
                      </div>
                    </div>

                    {/* Peers Section */}
                    <div className="space-y-3">
                      <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("torrent_details.general.network.title")}</h3>
                      <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                        <div className="grid grid-cols-2 gap-4">
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.network.seeds")}</p>
                            <p className="text-base font-semibold">{properties.seeds || 0} <span className="text-sm font-normal text-muted-foreground">/ {properties.seeds_total || 0}</span></p>
                          </div>
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.network.peers")}</p>
                            <p className="text-base font-semibold">{properties.peers || 0} <span className="text-sm font-normal text-muted-foreground">/ {properties.peers_total || 0}</span></p>
                          </div>
                        </div>
                      </div>
                    </div>

                    {/* Queue Information */}
                    {metadata?.preferences?.queueing_enabled && (
                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("torrent_details.general.queue.title")}</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50 space-y-3">
                          <div className="flex items-center justify-between">
                            <span className="text-sm text-muted-foreground">{t("torrent_details.general.queue.priority")}</span>
                            <div className="flex items-center gap-2">
                              <span className="text-sm font-semibold">
                                {torrent?.priority > 0 ? torrent.priority : t("torrent_details.general.queue.normal")}
                              </span>
                              {(torrent?.state === "queuedDL" || torrent?.state === "queuedUP") && (
                                <Badge variant="secondary" className="text-xs">
                                  {t("torrent_details.general.queue.queued")} {torrent.state === "queuedDL" ? t("torrent_details.general.queue.dl") : t("torrent_details.general.queue.up")}
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
                                    <p className="text-muted-foreground">{t("torrent_details.general.queue.max_downloads")}</p>
                                    <p className="font-medium">{metadata.preferences.max_active_downloads}</p>
                                  </div>
                                )}
                                {metadata.preferences.max_active_uploads > 0 && (
                                  <div className="space-y-1">
                                    <p className="text-muted-foreground">{t("torrent_details.general.queue.max_uploads")}</p>
                                    <p className="font-medium">{metadata.preferences.max_active_uploads}</p>
                                  </div>
                                )}
                                {metadata.preferences.max_active_torrents > 0 && (
                                  <div className="space-y-1">
                                    <p className="text-muted-foreground">{t("torrent_details.general.queue.max_active")}</p>
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
                      <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("torrent_details.general.time.title")}</h3>
                      <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                        <div className="grid grid-cols-2 gap-4">
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.time.time_active")}</p>
                            <p className="text-sm font-medium">{formatDuration(properties.time_elapsed || 0)}</p>
                          </div>
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.time.seeding_time")}</p>
                            <p className="text-sm font-medium">{formatDuration(properties.seeding_time || 0)}</p>
                          </div>
                        </div>
                      </div>
                    </div>

                    {/* Save Path */}
                    <div className="space-y-3">
                      <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("torrent_details.general.location.title")}</h3>
                      <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                        <div className="font-mono text-xs sm:text-sm break-all text-muted-foreground">
                          {displaySavePath || t("common.not_available")}
                        </div>
                      </div>
                    </div>

                    {/* Info Hash Display */}
                    <div className="space-y-3">
                      <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("torrent_details.general.identifiers.title")}</h3>
                      <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50 space-y-4">
                        <div className="space-y-2">
                          <p className="text-xs text-muted-foreground">{t("torrent_details.general.identifiers.infohash_v1")}</p>
                          <div className="flex items-center gap-2">
                            <div className="text-xs font-mono bg-background/50 p-2.5 rounded flex-1 break-all select-text">
                              {displayInfohashV1 || t("common.not_available")}
                            </div>
                            {displayInfohashV1 && (
                              <Button
                                variant="ghost"
                                size="icon"
                                className="h-8 w-8 shrink-0"
                                onClick={() => copyToClipboard(displayInfohashV1, t("torrent_details.general.identifiers.infohash_v1"))}
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
                              <p className="text-xs text-muted-foreground">{t("torrent_details.general.identifiers.infohash_v2")}</p>
                              <div className="flex items-center gap-2">
                                <div className="text-xs font-mono bg-background/50 p-2.5 rounded flex-1 break-all select-text">
                                  {displayInfohashV2}
                                </div>
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  className="h-8 w-8 shrink-0"
                                  onClick={() => copyToClipboard(displayInfohashV2, t("torrent_details.general.identifiers.infohash_v2"))}
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
                      <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("torrent_details.general.timestamps.title")}</h3>
                      <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.timestamps.added")}</p>
                            <p className="text-sm">{formatTimestamp(properties.addition_date)}</p>
                          </div>
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.timestamps.completed")}</p>
                            <p className="text-sm">{formatTimestamp(properties.completion_date)}</p>
                          </div>
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">{t("torrent_details.general.timestamps.created")}</p>
                            <p className="text-sm">{formatTimestamp(properties.creation_date)}</p>
                          </div>
                        </div>
                      </div>
                    </div>

                    {/* Additional Information */}
                    {(displayComment || displayCreatedBy) && (
                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("torrent_details.general.additional_info.title")}</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50 space-y-3">
                          {displayCreatedBy && (
                            <div>
                              <p className="text-xs text-muted-foreground mb-1">{t("torrent_details.general.additional_info.created_by")}</p>
                              <div className="text-sm">{renderTextWithLinks(displayCreatedBy)}</div>
                            </div>
                          )}
                          {displayComment && (
                            <>
                              {displayCreatedBy && <Separator className="opacity-50" />}
                              <div>
                                <p className="text-xs text-muted-foreground mb-2">{t("torrent_details.general.additional_info.comment")}</p>
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
          </TabsContent>

          <TabsContent value="trackers" className="m-0 h-full">
            <ScrollArea className="h-full">
              <div className="p-4 sm:p-6">
                {activeTab === "trackers" && loadingTrackers && !trackers ? (
                  <div className="flex items-center justify-center p-8">
                    <Loader2 className="h-6 w-6 animate-spin" />
                  </div>
                ) : trackers && trackers.length > 0 ? (
                  <div className="space-y-3">
                    <div className="flex items-center justify-between mb-1">
                      <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("torrent_details.trackers.title")}</h3>
                      <span className="text-xs text-muted-foreground">{t("torrent_details.trackers.count", { count: trackers.length })}</span>
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
                          const messageContent = incognitoMode && shouldRenderMessage? t("torrent_details.trackers.incognito_message"): tracker.msg

                          return (
                            <div
                              key={index}
                              className={`backdrop-blur-sm border ${tracker.status === 0 ? "bg-card/30 border-border/30 opacity-60" : "bg-card/50 border-border/50"} hover:border-border transition-all rounded-lg p-4 space-y-3`}
                            >
                              <div className="flex flex-col sm:flex-row sm:items-start justify-between gap-2">
                                <div className="flex-1 space-y-1">
                                  <div className="flex items-center gap-2">
                                    {getTrackerStatusBadge(tracker.status, t)}
                                  </div>
                                  <p className="text-xs font-mono text-muted-foreground break-all">{displayUrl}</p>
                                </div>
                              </div>
                              <Separator className="opacity-50" />
                              <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
                                <div className="space-y-1">
                                  <p className="text-xs text-muted-foreground">{t("torrent_details.trackers.seeds")}</p>
                                  <p className="text-sm font-medium">{tracker.num_seeds}</p>
                                </div>
                                <div className="space-y-1">
                                  <p className="text-xs text-muted-foreground">{t("torrent_details.trackers.peers")}</p>
                                  <p className="text-sm font-medium">{tracker.num_peers}</p>
                                </div>
                                <div className="space-y-1">
                                  <p className="text-xs text-muted-foreground">{t("torrent_details.trackers.leechers")}</p>
                                  <p className="text-sm font-medium">{tracker.num_leechers}</p>
                                </div>
                                <div className="space-y-1">
                                  <p className="text-xs text-muted-foreground">{t("torrent_details.trackers.downloaded")}</p>
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
                    {t("torrent_details.trackers.no_trackers")}
                  </div>
                )}
              </div>
            </ScrollArea>
          </TabsContent>

          <TabsContent value="peers" className="m-0 h-full">
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
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("torrent_details.peers.title")}</h3>
                        <p className="text-xs text-muted-foreground mt-1">{t("torrent_details.peers.count", { count: Object.keys(peersData.peers).length })}</p>
                      </div>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setShowAddPeersDialog(true)}
                      >
                        <UserPlus className="h-4 w-4 mr-2" />
                        {t("torrent_details.peers.add_peers_button")}
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

                        return (
                          <ContextMenu key={peerKey}>
                            <ContextMenuTrigger asChild>
                              <div className={`bg-card/50 backdrop-blur-sm border ${isActive ? "border-border/70" : "border-border/30"} hover:border-border transition-all rounded-lg p-4 space-y-3 cursor-context-menu`}>
                                {/* Peer Header */}
                                <div className="flex items-start justify-between gap-3">
                                  <div className="flex-1 space-y-1">
                                    <div className="flex items-center gap-2 flex-wrap">
                                      <span className="font-mono text-sm">{peer.ip}:{peer.port}</span>
                                      {peer.country_code && (
                                        <span
                                          className={`fi fi-${peer.country_code.toLowerCase()} rounded text-sm`}
                                          title={peer.country || peer.country_code}
                                        />
                                      )}
                                      {isSeeder && (
                                        <Badge variant="secondary" className="text-xs">{t("torrent_details.peers.seeder")}</Badge>
                                      )}
                                    </div>
                                    <p className="text-xs text-muted-foreground">{peer.client || t("torrent_details.peers.unknown_client")}</p>
                                  </div>
                                </div>

                                <Separator className="opacity-50" />

                                {/* Progress Bar */}
                                <div className="space-y-1">
                                  <p className="text-xs text-muted-foreground">{t("torrent_details.peers.peer_progress")}</p>
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
                                    <p className="text-xs text-muted-foreground">{t("torrent_details.peers.download_speed")}</p>
                                    <p className={`text-sm font-medium ${peer.dl_speed && peer.dl_speed > 0 ? "text-green-500" : ""}`}>
                                      {formatSpeedWithUnit(peer.dl_speed || 0, speedUnit)}
                                    </p>
                                  </div>
                                  <div className="space-y-1">
                                    <p className="text-xs text-muted-foreground">{t("torrent_details.peers.upload_speed")}</p>
                                    <p className={`text-sm font-medium ${peer.up_speed && peer.up_speed > 0 ? "text-blue-500" : ""}`}>
                                      {formatSpeedWithUnit(peer.up_speed || 0, speedUnit)}
                                    </p>
                                  </div>
                                </div>

                                {/* Data Transfer Info */}
                                <div className="grid grid-cols-2 gap-3 text-xs">
                                  <div className="space-y-1">
                                    <p className="text-muted-foreground">{t("torrent_details.peers.downloaded")}</p>
                                    <p className="font-medium">{formatBytes(peer.downloaded || 0)}</p>
                                  </div>
                                  <div className="space-y-1">
                                    <p className="text-muted-foreground">{t("torrent_details.peers.uploaded")}</p>
                                    <p className="font-medium">{formatBytes(peer.uploaded || 0)}</p>
                                  </div>
                                </div>

                                {/* Connection Info */}
                                {(peer.connection || peer.flags) && (
                                  <>
                                    <Separator className="opacity-50" />
                                    <div className="flex flex-wrap gap-4 text-xs text-muted-foreground">
                                      {peer.connection && (
                                        <div>
                                          <span className="opacity-70">{t("torrent_details.peers.connection")}:</span> {peer.connection}
                                        </div>
                                      )}
                                      {peer.flags && (
                                        <div>
                                          <span className="opacity-70">{t("torrent_details.peers.flags")}:</span> {peer.flags}
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
                                {t("torrent_details.peers.copy_ip")}
                              </ContextMenuItem>
                              <ContextMenuSeparator />
                              <ContextMenuItem
                                onClick={() => handleBanPeerClick(peer)}
                                className="text-destructive focus:text-destructive"
                              >
                                <Ban className="h-4 w-4 mr-2" />
                                {t("torrent_details.peers.ban_peer")}
                              </ContextMenuItem>
                            </ContextMenuContent>
                          </ContextMenu>
                        )
                      })}
                    </div>
                  </div>
                ) : (
                  <div className="flex flex-col items-center justify-center h-32 text-sm text-muted-foreground gap-3">
                    <p>{t("torrent_details.peers.no_peers")}</p>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setShowAddPeersDialog(true)}
                    >
                      <UserPlus className="h-4 w-4 mr-2" />
                      {t("torrent_details.peers.add_peers_button")}
                    </Button>
                  </div>
                )}
              </div>
            </ScrollArea>
          </TabsContent>

          <TabsContent value="content" className="m-0 h-full">
            <ScrollArea className="h-full">
              <div className="p-4 sm:p-6">
                {activeTab === "content" && loadingFiles && !files ? (
                  <div className="flex items-center justify-center p-8">
                    <Loader2 className="h-6 w-6 animate-spin" />
                  </div>
                ) : files && files.length > 0 ? (
                  <div className="space-y-3">
                    <div className="flex items-center justify-between mb-1">
                      <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t("torrent_details.content.title")}</h3>
                      <span className="text-xs text-muted-foreground">{t("torrent_details.content.count", { count: files.length })}</span>
                    </div>
                    <div className="space-y-2">
                      {files.map((file, index) => {
                        const displayFileName = incognitoMode ? getLinuxFileName(torrent.hash, index) : file.name
                        const progressPercent = file.progress * 100
                        const isComplete = progressPercent === 100

                        return (
                          <div key={index} className="bg-card/50 backdrop-blur-sm border border-border/50 hover:border-border transition-all rounded-lg p-4">
                            <div className="space-y-3">
                              <div className="flex items-start justify-between gap-3">
                                <div className="flex-1 min-w-0">
                                  <p className="text-xs sm:text-sm font-mono text-muted-foreground break-all">{displayFileName}</p>
                                </div>
                                <Badge variant={isComplete ? "default" : "secondary"} className="shrink-0 text-xs">
                                  {formatBytes(file.size)}
                                </Badge>
                              </div>
                              <div className="flex items-center gap-3">
                                <Progress value={progressPercent} className="flex-1 h-1.5" />
                                <span className={`text-xs font-medium ${isComplete ? "text-green-500" : "text-muted-foreground"}`}>
                                  {Math.round(progressPercent)}%
                                </span>
                              </div>
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  </div>
                ) : (
                  <div className="flex items-center justify-center h-32 text-sm text-muted-foreground">
                    {t("torrent_details.content.no_files")}
                  </div>
                )}
              </div>
            </ScrollArea>
          </TabsContent>
        </div>
      </Tabs>

      {/* Add Peers Dialog */}
      <Dialog open={showAddPeersDialog} onOpenChange={setShowAddPeersDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("torrent_details.dialogs.add_peers.title")}</DialogTitle>
            <DialogDescription>
              {t("torrent_details.dialogs.add_peers.description")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="peers">{t("torrent_details.dialogs.add_peers.peers_label")}</Label>
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
              {t("common.cancel")}
            </Button>
            <Button
              onClick={handleAddPeersSubmit}
              disabled={!peersToAdd.trim() || addPeersMutation.isPending}
            >
              {addPeersMutation.isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              {t("torrent_details.dialogs.add_peers.add_button")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Ban Peer Confirmation Dialog */}
      <Dialog open={showBanPeerDialog} onOpenChange={setShowBanPeerDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("torrent_details.dialogs.ban_peer.title")}</DialogTitle>
            <DialogDescription>
              {t("torrent_details.dialogs.ban_peer.description")}
            </DialogDescription>
          </DialogHeader>
          {peerToBan && (
            <div className="space-y-2 text-sm">
              <div>
                <span className="text-muted-foreground">{t("torrent_details.dialogs.ban_peer.ip_address")}:</span>
                <span className="ml-2 font-mono">{peerToBan.ip}:{peerToBan.port}</span>
              </div>
              {peerToBan.client && (
                <div>
                  <span className="text-muted-foreground">{t("torrent_details.dialogs.ban_peer.client")}:</span>
                  <span className="ml-2">{peerToBan.client}</span>
                </div>
              )}
              {peerToBan.country && (
                <div>
                  <span className="text-muted-foreground">{t("torrent_details.dialogs.ban_peer.country")}:</span>
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
              {t("common.cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={handleBanPeerConfirm}
              disabled={banPeerMutation.isPending}
            >
              {banPeerMutation.isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              {t("torrent_details.dialogs.ban_peer.ban_button")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
});
