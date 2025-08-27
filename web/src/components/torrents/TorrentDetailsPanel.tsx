/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { memo, useState, useEffect } from "react"
import { useQuery } from "@tanstack/react-query"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Badge } from "@/components/ui/badge"
import { Progress } from "@/components/ui/progress"
import { Loader2 } from "lucide-react"
import { api } from "@/lib/api"
import type { Torrent } from "@/types"
import { formatBytes, formatSpeed, formatTimestamp, formatDuration } from "@/lib/utils"

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
  const { data: metadata } = useInstanceMetadata(instanceId)

  // Reset tab when torrent changes
  useEffect(() => {
    setActiveTab("general")
  }, [torrent?.hash])

  // Fetch torrent properties
  const { data: properties, isLoading: loadingProperties } = useQuery({
    queryKey: ["torrent-properties", instanceId, torrent?.hash],
    queryFn: () => api.getTorrentProperties(instanceId, torrent!.hash),
    enabled: !!torrent,
  })

  // Fetch torrent trackers
  const { data: trackers, isLoading: loadingTrackers } = useQuery({
    queryKey: ["torrent-trackers", instanceId, torrent?.hash],
    queryFn: () => api.getTorrentTrackers(instanceId, torrent!.hash),
    enabled: !!torrent && activeTab === "trackers",
  })

  // Fetch torrent files
  const { data: files, isLoading: loadingFiles } = useQuery({
    queryKey: ["torrent-files", instanceId, torrent?.hash],
    queryFn: () => api.getTorrentFiles(instanceId, torrent!.hash),
    enabled: !!torrent && activeTab === "content",
  })

  if (!torrent) return null

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
                {loadingProperties ? (
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
                        <span className="ml-2 text-sm">{formatSpeed(properties.dl_speed || 0)} (avg: {formatSpeed(properties.dl_speed_avg || 0)})</span>
                      </div>
                      <div>
                        <span className="text-sm text-muted-foreground">Upload Speed:</span>
                        <span className="ml-2 text-sm">{formatSpeed(properties.up_speed || 0)} (avg: {formatSpeed(properties.up_speed_avg || 0)})</span>
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
                        <div>
                          <span className="text-sm text-muted-foreground">Queue Limits:</span>
                          <div className="ml-2 text-sm space-y-1">
                            {metadata.preferences.max_active_downloads > 0 && (
                              <div>Max Active Downloads: {metadata.preferences.max_active_downloads}</div>
                            )}
                            {metadata.preferences.max_active_uploads > 0 && (
                              <div>Max Active Uploads: {metadata.preferences.max_active_uploads}</div>
                            )}
                            {metadata.preferences.max_active_torrents > 0 && (
                              <div>Max Active Total: {metadata.preferences.max_active_torrents}</div>
                            )}
                          </div>
                        </div>
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
                {loadingTrackers ? (
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

          <TabsContent value="content" className="m-0 h-full">
            <ScrollArea className="h-full">
              <div className="p-4 sm:p-6">
                {loadingFiles ? (
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
    </div>
  )
});