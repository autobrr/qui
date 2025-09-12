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
  progress?: number
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

interface TorrentPeersResponse {
  full_update?: boolean
  rid?: number
  peers?: Record<string, TorrentPeer>
  peers_removed?: string[]
  show_flags?: boolean
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
                  <div className="space-y-4">
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 sm:gap-4 text-sm">
                      <div>
                        <span className="text-muted-foreground">Total Size:</span>
                        <span className="ml-2">{formatBytes(properties.total_size || torrent.size)}</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Pieces:</span>
                        <span className="ml-2">{properties.pieces_have || 0} / {properties.pieces_num || 0} ({formatBytes(properties.piece_size || 0)})</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Downloaded:</span>
                        <span className="ml-2">{formatBytes(properties.total_downloaded || 0)}</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Uploaded:</span>
                        <span className="ml-2">{formatBytes(properties.total_uploaded || 0)}</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Share Ratio:</span>
                        <span className="ml-2">{(properties.share_ratio || 0).toFixed(2)}</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Seeds:</span>
                        <span className="ml-2">{properties.seeds || 0} ({properties.seeds_total || 0})</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Peers:</span>
                        <span className="ml-2">{properties.peers || 0} ({properties.peers_total || 0})</span>
                      </div>
                      <div>
                        <span className="text-muted-foreground">Wasted:</span>
                        <span className="ml-2">{formatBytes(properties.total_wasted || 0)}</span>
                      </div>
                    </div>

                    <div className="space-y-2">
                      <div>
                        <span className="text-sm text-muted-foreground">Download Speed:</span>
                        <span className="ml-2 text-sm">{formatSpeedWithUnit(properties.dl_speed || 0, speedUnit)} (avg: {formatSpeedWithUnit(properties.dl_speed_avg || 0, speedUnit)})</span>
                      </div>
                      <div>
                        <span className="text-sm text-muted-foreground">Upload Speed:</span>
                        <span className="ml-2 text-sm">{formatSpeedWithUnit(properties.up_speed || 0, speedUnit)} (avg: {formatSpeedWithUnit(properties.up_speed_avg || 0, speedUnit)})</span>
                      </div>
                    </div>

                    {/* Queue Information */}
                    {metadata?.preferences?.queueing_enabled && (
                      <div className="space-y-2">
                        <div>
                          <span className="text-sm text-muted-foreground">Priority:</span>
                          <span className="ml-2 text-sm">
                            {torrent?.priority > 0 ? (
                              <>
                                {torrent.priority}
                                {(torrent.state === "queuedDL" || torrent.state === "queuedUP") && (
                                  <Badge variant="secondary" className="ml-2 text-xs">
                                    Queued {torrent.state === "queuedDL" ? "DL" : "UP"}
                                  </Badge>
                                )}
                              </>
                            ) : (
                              "Normal"
                            )}
                          </span>
                        </div>
                        {metadata.preferences.max_active_downloads > 0 && (
                          <div>
                            <span className="text-sm text-muted-foreground">Max Active Downloads:</span>
                            <span className="ml-2 text-sm">{metadata.preferences.max_active_downloads}</span>
                          </div>
                        )}
                        {metadata.preferences.max_active_uploads > 0 && (
                          <div>
                            <span className="text-sm text-muted-foreground">Max Active Uploads:</span>
                            <span className="ml-2 text-sm">{metadata.preferences.max_active_uploads}</span>
                          </div>
                        )}
                        {metadata.preferences.max_active_torrents > 0 && (
                          <div>
                            <span className="text-sm text-muted-foreground">Max Active Torrents:</span>
                            <span className="ml-2 text-sm">{metadata.preferences.max_active_torrents}</span>
                          </div>
                        )}
                      </div>
                    )}

                    <div className="space-y-2">
                      <div>
                        <span className="text-sm text-muted-foreground">Time Active:</span>
                        <span className="ml-2 text-sm">{formatDuration(properties.time_elapsed || 0)}</span>
                      </div>
                      <div>
                        <span className="text-sm text-muted-foreground">Seeding Time:</span>
                        <span className="ml-2 text-sm">{formatDuration(properties.seeding_time || 0)}</span>
                      </div>
                    </div>

                    <div className="space-y-2">
                      <div>
                        <span className="text-sm text-muted-foreground">Save Path:</span>
                        <div className="text-xs sm:text-sm mt-1 font-mono bg-muted/50 hover:bg-muted transition-colors p-2 sm:p-3 rounded break-all">
                          {properties.save_path || "N/A"}
                        </div>
                      </div>
                    </div>

                    {/* Info Hash Display */}
                    <div className="space-y-2">
                      <div>
                        <span className="text-sm text-muted-foreground">Info Hash v1:</span>
                        <div className="flex items-center gap-2 mt-1">
                          <div className="text-xs sm:text-sm font-mono bg-muted/50 hover:bg-muted transition-colors p-2 sm:p-3 rounded break-all select-text flex-1">
                            {properties.infohash_v1 && properties.infohash_v1.length > 0 ? properties.infohash_v1 : "N/A"}
                          </div>
                          {properties.infohash_v1 && properties.infohash_v1.length > 0 && (
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-8 w-8 shrink-0"
                              onClick={() => copyToClipboard(properties.infohash_v1, "Info Hash v1")}
                            >
                              <Copy className="h-4 w-4" />
                            </Button>
                          )}
                        </div>
                      </div>
                      <div>
                        <span className="text-sm text-muted-foreground">Info Hash v2:</span>
                        <div className="flex items-center gap-2 mt-1">
                          <div className="text-xs sm:text-sm font-mono bg-muted/50 hover:bg-muted transition-colors p-2 sm:p-3 rounded break-all select-text flex-1">
                            {properties.infohash_v2 && properties.infohash_v2.length > 0 ? properties.infohash_v2 : "N/A"}
                          </div>
                          {properties.infohash_v2 && properties.infohash_v2.length > 0 && (
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-8 w-8 shrink-0"
                              onClick={() => copyToClipboard(properties.infohash_v2, "Info Hash v2")}
                            >
                              <Copy className="h-4 w-4" />
                            </Button>
                          )}
                        </div>
                      </div>
                    </div>

                    <div className="space-y-2">
                      <div>
                        <span className="text-sm text-muted-foreground">Added On:</span>
                        <span className="ml-2 text-sm">{formatTimestamp(properties.addition_date)}</span>
                      </div>
                      <div>
                        <span className="text-sm text-muted-foreground">Completed On:</span>
                        <span className="ml-2 text-sm">{formatTimestamp(properties.completion_date)}</span>
                      </div>
                      <div>
                        <span className="text-sm text-muted-foreground">Created On:</span>
                        <span className="ml-2 text-sm">{formatTimestamp(properties.creation_date)}</span>
                      </div>
                    </div>

                    {properties.comment && (
                      <div>
                        <span className="text-sm text-muted-foreground">Comment:</span>
                        <div className="text-xs sm:text-sm mt-1 bg-muted/50 hover:bg-muted transition-colors p-2 sm:p-3 rounded break-words">
                          {properties.comment}
                        </div>
                      </div>
                    )}

                    {properties.created_by && (
                      <div>
                        <span className="text-sm text-muted-foreground">Created By:</span>
                        <span className="ml-2 text-sm">{properties.created_by}</span>
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
                  <div className="space-y-2">
                    {trackers.map((tracker, index) => (
                      <div key={index} className="border border-border/50 hover:border-border bg-card/50 hover:bg-card transition-all rounded-lg p-3 sm:p-4 space-y-2">
                        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2">
                          <span className="text-xs sm:text-sm font-mono break-all">{tracker.url}</span>
                          {getTrackerStatusBadge(tracker.status)}
                        </div>
                        <div className="grid grid-cols-2 gap-2 text-xs text-muted-foreground">
                          <div>Seeds: {tracker.num_seeds}</div>
                          <div>Peers: {tracker.num_peers}</div>
                          <div>Leechers: {tracker.num_leechers}</div>
                          <div>Downloaded: {tracker.num_downloaded}</div>
                        </div>
                        {tracker.msg && (
                          <div className="text-xs text-muted-foreground">{tracker.msg}</div>
                        )}
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="text-sm text-muted-foreground text-center p-4">
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
                  <>
                    <div className="flex justify-end mb-4">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setShowAddPeersDialog(true)}
                      >
                        <UserPlus className="h-4 w-4 mr-2" />
                        Add Peers
                      </Button>
                    </div>
                    <div className="space-y-2">
                      {Object.entries(peersData.peers).map(([peerKey, peer]) => (
                        <ContextMenu key={peerKey}>
                          <ContextMenuTrigger>
                            <div className="border border-border/50 hover:border-border bg-card/50 hover:bg-card transition-all rounded-lg p-3 sm:p-4 space-y-2 cursor-context-menu">
                              <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2">
                                <div className="flex items-center gap-2">
                                  <span className="text-xs sm:text-sm font-mono">{peer.ip}:{peer.port}</span>
                                  {peer.country_code && (
                                    <span
                                      className={`fi fi-${peer.country_code.toLowerCase()} rounded text-xs`}
                                      title={peer.country || peer.country_code}
                                    />
                                  )}
                                </div>
                                <span className="text-xs text-muted-foreground">{peer.client || "Unknown"}</span>
                              </div>
                              <div className="grid grid-cols-2 gap-2 text-xs text-muted-foreground">
                                <div>Progress: {Math.round((peer.progress || 0) * 100)}%</div>
                                <div>Connection: {peer.connection || "N/A"}</div>
                                <div>DL: {formatSpeedWithUnit(peer.dl_speed || 0, speedUnit)}</div>
                                <div>UL: {formatSpeedWithUnit(peer.up_speed || 0, speedUnit)}</div>
                                <div>Downloaded: {formatBytes(peer.downloaded || 0)}</div>
                                <div>Uploaded: {formatBytes(peer.uploaded || 0)}</div>
                              </div>
                              {peer.flags && (
                                <div className="text-xs text-muted-foreground">Flags: {peer.flags}</div>
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
                      ))}
                    </div>
                  </>
                ) : (
                  <div className="text-sm text-muted-foreground text-center p-4">
                    No peers connected
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
                  <div className="space-y-1">
                    {files.map((file, index) => (
                      <div key={index} className="border border-border/50 hover:border-border bg-card/50 hover:bg-card transition-all rounded p-3 sm:p-2 space-y-2 sm:space-y-1">
                        <div className="text-xs sm:text-sm font-mono break-all">{file.name}</div>
                        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 sm:gap-0 text-xs text-muted-foreground">
                          <span>{formatBytes(file.size)}</span>
                          <div className="flex items-center gap-2">
                            {(() => {
                              const progressPercent = file.progress * 100
                              return (
                                <>
                                  <Progress value={progressPercent} className="w-20 h-2" />
                                  <span>{Math.round(progressPercent)}%</span>
                                </>
                              )
                            })()}
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="text-sm text-muted-foreground text-center p-4">
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