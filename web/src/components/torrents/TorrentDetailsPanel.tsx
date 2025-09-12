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
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { api } from "@/lib/api"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import { formatBytes, formatDuration, formatTimestamp } from "@/lib/utils"
import type { Torrent } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import "flag-icons/css/flag-icons.min.css"
import { Ban, Copy, Loader2, UserPlus } from "lucide-react"
import { memo, useCallback, useEffect, useState } from "react"
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

export const TorrentDetailsPanel = memo(function TorrentDetailsPanel({ instanceId, torrent }: TorrentDetailsPanelProps) {
  const [activeTab, setActiveTab] = useState("general")
  const [showAddPeersDialog, setShowAddPeersDialog] = useState(false)
  const [showBanPeerDialog, setShowBanPeerDialog] = useState(false)
  const [peersToAdd, setPeersToAdd] = useState("")
  const [peerToBan, setPeerToBan] = useState<TorrentPeer | null>(null)
  const [isReady, setIsReady] = useState(false)
  const { data: metadata } = useInstanceMetadata(instanceId)
  const queryClient = useQueryClient()
  const [speedUnit] = useSpeedUnits()

  const copyToClipboard = useCallback(async (text: string, type: string) => {
    try {
      await navigator.clipboard.writeText(text)
      toast.success(`${type} copied!`)
    } catch {
      toast.error("Failed to copy to clipboard")
    }
  }, [])
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
  const { data: peersData, isLoading: loadingPeers } = useQuery<TorrentPeersResponse>({
    queryKey: ["torrent-peers", instanceId, torrent?.hash],
    queryFn: async () => {
      const data = await api.getTorrentPeers(instanceId, torrent!.hash)
      return data as TorrentPeersResponse
    },
    enabled: !!torrent && isReady,
    // Only refetch when tab is active and document is visible
    refetchInterval: () => {
      if (activeTab === "peers" && document.visibilityState === "visible" && isReady) {
        return 2000
      }
      return false
    },
    staleTime: activeTab === "peers" ? 0 : 30000, // No stale time when viewing peers
    gcTime: 5 * 60 * 1000,
  })

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

  // Handle copy peer IP:port
  const handleCopyPeer = useCallback((peer: TorrentPeer) => {
    const peerAddress = `${peer.ip}:${peer.port}`
    navigator.clipboard.writeText(peerAddress).then(() => {
      toast.success(`Copied ${peerAddress} to clipboard`)
    }).catch(() => {
      toast.error("Failed to copy to clipboard")
    })
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

  if (!torrent) return null

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
        <h3 className="text-sm font-semibold truncate flex-1 pr-2" title={torrent.name}>
          {torrent.name}
        </h3>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab} className="flex-1 flex flex-col overflow-hidden">
        <TabsList className="w-full justify-start rounded-none border-b h-10 bg-background px-4 sm:px-6 py-0">
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
          <TabsTrigger
            value="content"
            className="relative text-xs rounded-none data-[state=active]:bg-transparent data-[state=active]:shadow-none hover:bg-accent/50 transition-all px-3 sm:px-4 cursor-pointer focus-visible:outline-none focus-visible:ring-0 after:absolute after:bottom-0 after:left-0 after:right-0 after:h-[2px] after:bg-primary after:scale-x-0 data-[state=active]:after:scale-x-100 after:transition-transform"
          >
            Content
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
                          </div>
                          <div className="space-y-1">
                            <p className="text-xs text-muted-foreground">Upload Speed</p>
                            <p className="text-base font-semibold text-blue-500">{formatSpeedWithUnit(properties.up_speed || 0, speedUnit)}</p>
                            <p className="text-xs text-muted-foreground">avg: {formatSpeedWithUnit(properties.up_speed_avg || 0, speedUnit)}</p>
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
                      <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">File Location</h3>
                      <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50">
                        <div className="font-mono text-xs sm:text-sm break-all text-muted-foreground">
                          {properties.save_path || "N/A"}
                        </div>
                      </div>
                    </div>

                    {/* Info Hash Display */}
                    <div className="space-y-3">
                      <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Torrent Identifiers</h3>
                      <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50 space-y-4">
                        <div className="space-y-2">
                          <p className="text-xs text-muted-foreground">Info Hash v1</p>
                          <div className="flex items-center gap-2">
                            <div className="text-xs font-mono bg-background/50 p-2.5 rounded flex-1 break-all select-text">
                              {properties.infohash_v1 && properties.infohash_v1.length > 0 ? properties.infohash_v1 : "N/A"}
                            </div>
                            {properties.infohash_v1 && properties.infohash_v1.length > 0 && (
                              <Button
                                variant="ghost"
                                size="icon"
                                className="h-8 w-8 shrink-0"
                                onClick={() => copyToClipboard(properties.infohash_v1, "Info Hash v1")}
                              >
                                <Copy className="h-3.5 w-3.5" />
                              </Button>
                            )}
                          </div>
                        </div>
                        {properties.infohash_v2 && properties.infohash_v2.length > 0 && (
                          <>
                            <Separator className="opacity-50" />
                            <div className="space-y-2">
                              <p className="text-xs text-muted-foreground">Info Hash v2</p>
                              <div className="flex items-center gap-2">
                                <div className="text-xs font-mono bg-background/50 p-2.5 rounded flex-1 break-all select-text">
                                  {properties.infohash_v2}
                                </div>
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  className="h-8 w-8 shrink-0"
                                  onClick={() => copyToClipboard(properties.infohash_v2, "Info Hash v2")}
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
                    {(properties.comment || properties.created_by) && (
                      <div className="space-y-3">
                        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Additional Information</h3>
                        <div className="bg-card/50 backdrop-blur-sm rounded-lg p-4 border border-border/50 space-y-3">
                          {properties.created_by && (
                            <div>
                              <p className="text-xs text-muted-foreground mb-1">Created By</p>
                              <p className="text-sm">{properties.created_by}</p>
                            </div>
                          )}
                          {properties.comment && (
                            <>
                              {properties.created_by && <Separator className="opacity-50" />}
                              <div>
                                <p className="text-xs text-muted-foreground mb-2">Comment</p>
                                <div className="text-sm bg-background/50 p-3 rounded break-words">
                                  {properties.comment}
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
                        .map((tracker, index) => (
                          <div key={index} className={`backdrop-blur-sm border ${tracker.status === 0 ? "bg-card/30 border-border/30 opacity-60" : "bg-card/50 border-border/50"} hover:border-border transition-all rounded-lg p-4 space-y-3`}>
                            <div className="flex flex-col sm:flex-row sm:items-start justify-between gap-2">
                              <div className="flex-1 space-y-1">
                                <div className="flex items-center gap-2">
                                  {getTrackerStatusBadge(tracker.status)}
                                </div>
                                <p className="text-xs font-mono text-muted-foreground break-all">{tracker.url}</p>
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
                                <p className="text-sm font-medium">{tracker.num_leechers}</p>
                              </div>
                              <div className="space-y-1">
                                <p className="text-xs text-muted-foreground">Downloaded</p>
                                <p className="text-sm font-medium">{tracker.num_downloaded}</p>
                              </div>
                            </div>
                            {tracker.msg && (
                              <>
                                <Separator className="opacity-50" />
                                <div className="bg-background/50 p-2 rounded">
                                  <p className="text-xs text-muted-foreground">{tracker.msg}</p>
                                </div>
                              </>
                            )}
                          </div>
                        ))}
                    </div>
                  </div>
                ) : (
                  <div className="flex items-center justify-center h-32 text-sm text-muted-foreground">
                    No trackers found
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
                                {(peer.connection || peer.flags) && (
                                  <>
                                    <Separator className="opacity-50" />
                                    <div className="flex flex-wrap gap-4 text-xs text-muted-foreground">
                                      {peer.connection && (
                                        <div>
                                          <span className="opacity-70">Connection:</span> {peer.connection}
                                        </div>
                                      )}
                                      {peer.flags && (
                                        <div>
                                          <span className="opacity-70">Flags:</span> {peer.flags}
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
                      <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">File Contents</h3>
                      <span className="text-xs text-muted-foreground">{files.length} file{files.length !== 1 ? "s" : ""}</span>
                    </div>
                    <div className="space-y-2">
                      {files.map((file, index) => {
                        const progressPercent = file.progress * 100
                        const isComplete = progressPercent === 100

                        return (
                          <div key={index} className="bg-card/50 backdrop-blur-sm border border-border/50 hover:border-border transition-all rounded-lg p-4">
                            <div className="space-y-3">
                              <div className="flex items-start justify-between gap-3">
                                <div className="flex-1 min-w-0">
                                  <p className="text-xs sm:text-sm font-mono text-muted-foreground break-all">{file.name}</p>
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
                    No files found
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
    </div>
  )
});