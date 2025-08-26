/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useInstances } from "@/hooks/useInstances"
import { useInstanceStats } from "@/hooks/useInstanceStats"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Badge } from "@/components/ui/badge"
import { Progress } from "@/components/ui/progress"
import { Button } from "@/components/ui/button"
import { PasswordIssuesBanner } from "@/components/instances/PasswordIssuesBanner"
import { InstanceErrorDisplay } from "@/components/instances/InstanceErrorDisplay"
import { HardDrive, Download, Upload, Activity, Plus, Minus, Zap, ChevronDown, ChevronUp, Eye, EyeOff } from "lucide-react"
import { Link } from "@tanstack/react-router"
import { useMemo, useState } from "react"
import { formatSpeed, formatBytes, getRatioColor } from "@/lib/utils"
import { useQuery, useQueries } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { ServerState, InstanceResponse, TorrentCounts } from "@/types"

type InstanceStats = Awaited<ReturnType<typeof api.getInstanceStats>>
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { useIncognitoMode } from "@/lib/incognito"


// Custom hook to get all instance stats using dynamic queries
function useAllInstanceStats(instances: InstanceResponse[]) {
  const statsQueries = useQueries({
    queries: instances.map(instance => ({
      queryKey: ["instance-stats", instance.id],
      queryFn: () => api.getInstanceStats(instance.id),
      enabled: true,
      refetchInterval: 5000,
      staleTime: 2000,
      gcTime: 1800000,
      placeholderData: (previousData: InstanceStats | undefined) => previousData,
      retry: 1,
      retryDelay: 1000,
    })),
  })
  
  const serverStateQueries = useQueries({
    queries: instances.map(instance => ({
      queryKey: ["server-state", instance.id],
      queryFn: async () => {
        try {
          const data = await api.syncMainData(instance.id, 0)
          const syncData = data as { server_state?: ServerState; serverState?: ServerState }
          return syncData.server_state || syncData.serverState || null
        } catch (error) {
          console.error("Error fetching server state for instance", instance.id, error)
          return null
        }
      },
      staleTime: 30000,
      refetchInterval: 30000,
      enabled: true,
    })),
  })
  
  const torrentCountsQueries = useQueries({
    queries: instances.map(instance => ({
      queryKey: ["torrent-counts", instance.id],
      queryFn: async () => {
        try {
          const data = await api.getTorrents(instance.id, { 
            page: 0, 
            limit: 1, 
          })
          return data.counts || null
        } catch (error) {
          console.error("Error fetching torrent counts for instance", instance.id, error)
          return null
        }
      },
      staleTime: 10000,
      refetchInterval: 10000,
      enabled: true,
    })),
  })
  
  return instances.map((instance, index) => ({
    instance,
    stats: statsQueries[index].data,
    serverState: serverStateQueries[index].data as ServerState | null,
    torrentCounts: torrentCountsQueries[index].data,
  }))
}


function InstanceCard({ instance }: { instance: InstanceResponse }) {
  const { data: stats, isLoading, error } = useInstanceStats(instance.id, { 
    enabled: true, // Always fetch stats, regardless of isActive status
    pollingInterval: 5000, // Slower polling for dashboard
  })
  const { data: torrentCounts } = useQuery({
    queryKey: ["torrent-counts", instance.id],
    queryFn: async () => {
      try {
        const data = await api.getTorrents(instance.id, { 
          page: 0, 
          limit: 1, 
        })
        return data.counts || null
      } catch (error) {
        console.error("Error fetching torrent counts for instance", instance.id, error)
        return null
      }
    },
    staleTime: 10000,
    refetchInterval: 10000,
    enabled: true,
  })
  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
  const displayUrl = instance.host
  
  // Show loading only on first load
  if (isLoading && !stats) {
    return (
      <Link to="/instances/$instanceId" params={{ instanceId: instance.id.toString() }}>
        <Card className="hover:shadow-lg transition-shadow cursor-pointer opacity-60">
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle className="text-lg">{instance.name}</CardTitle>
              <Badge variant="secondary">
                Loading...
              </Badge>
            </div>
            <CardDescription className="flex items-center gap-1">
              <span className={incognitoMode ? "blur-sm select-none" : ""}>{displayUrl}</span>
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 p-0 hover:bg-muted/50"
                onClick={(e) => {
                  e.preventDefault()
                  e.stopPropagation()
                  setIncognitoMode(!incognitoMode)
                }}
              >
                {incognitoMode ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
              </Button>
            </CardDescription>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">Loading stats...</p>
          </CardContent>
        </Card>
      </Link>
    )
  }
  
  // If we have stats but instance is not connected, show with zero values
  if (stats && !stats.connected) {
    const hasErrors = instance.hasDecryptionError || instance.connectionError
    return (
      <Link to={hasErrors ? "/instances" : "/instances/$instanceId"} params={hasErrors ? {} : { instanceId: instance.id.toString() }}>
        <Card className="hover:shadow-lg transition-shadow cursor-pointer">
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle className="text-lg">{instance.name}</CardTitle>
              <Badge variant="destructive">Disconnected</Badge>
            </div>
            <CardDescription className="flex items-center gap-1">
              <span className={incognitoMode ? "blur-sm select-none" : ""}>{displayUrl}</span>
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 p-0 hover:bg-muted/50"
                onClick={(e) => {
                  e.preventDefault()
                  e.stopPropagation()
                  setIncognitoMode(!incognitoMode)
                }}
              >
                {incognitoMode ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
              </Button>
            </CardDescription>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground text-center">
              Instance is disconnected
            </p>
            
            <InstanceErrorDisplay instance={instance} />
          </CardContent>
        </Card>
      </Link>
    )
  }
  
  // If we have an error or no stats data, show error state
  if (error || !stats || !stats.torrents) {
    const hasErrors = instance.hasDecryptionError || instance.connectionError
    return (
      <Link to={hasErrors ? "/instances" : "/instances/$instanceId"} params={hasErrors ? {} : { instanceId: instance.id.toString() }}>
        <Card className="hover:shadow-lg transition-shadow cursor-pointer opacity-60">
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle className="text-lg">{instance.name}</CardTitle>
              <Badge variant="destructive">Error</Badge>
            </div>
            <CardDescription className="flex items-center gap-1">
              <span className={incognitoMode ? "blur-sm select-none" : ""}>{displayUrl}</span>
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 p-0 hover:bg-muted/50"
                onClick={(e) => {
                  e.preventDefault()
                  e.stopPropagation()
                  setIncognitoMode(!incognitoMode)
                }}
              >
                {incognitoMode ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
              </Button>
            </CardDescription>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">
              Failed to load stats
            </p>
            
            <InstanceErrorDisplay instance={instance} />
          </CardContent>
        </Card>
      </Link>
    )
  }
  
  const hasErrors = instance.hasDecryptionError || instance.connectionError
  return (
    <Link to={hasErrors ? "/instances" : "/instances/$instanceId"} params={hasErrors ? {} : { instanceId: instance.id.toString() }}>
      <Card className="hover:shadow-lg transition-shadow cursor-pointer">
        <CardHeader className='gap-0'>
          <div className="flex items-center justify-between">
            <CardTitle className="text-lg">{instance.name}</CardTitle>
            <Badge variant={stats.connected ? "default" : "destructive"}>
              {stats.connected ? "Connected" : "Disconnected"}
            </Badge>
          </div>
          <CardDescription className="flex items-center gap-1 text-xs">
            <span className={incognitoMode ? "blur-sm select-none truncate" : "truncate"}>{displayUrl}</span>
            <Button
              variant="ghost"
              size="icon"
              className="h-4 w-4 p-0"
              onClick={(e) => {
                e.preventDefault()
                e.stopPropagation()
                setIncognitoMode(!incognitoMode)
              }}
            >
              {incognitoMode ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
            </Button>
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-3">
            <div className="mb-6">
              <div className="flex items-center justify-center mb-1">
                <span className="flex-1 text-center text-xs text-muted-foreground">Downloading</span>
                <span className="flex-1 text-center text-xs text-muted-foreground">Active</span>
                <span className="flex-1 text-center text-xs text-muted-foreground">Error</span>
                <span className="flex-1 text-center text-xs text-muted-foreground">Total</span>
              </div>
              <div className="flex items-center justify-center">
                <span className="flex-1 text-center text-lg font-semibold">
                  {torrentCounts?.status?.downloading || 0}
                </span>
                <span className="flex-1 text-center text-lg font-semibold">{torrentCounts?.status?.active || 0}</span>
                <span className={`flex-1 text-center text-lg font-semibold ${(torrentCounts?.status?.errored || 0) > 0 ? "text-destructive" : ""}`}>
                  {torrentCounts?.status?.errored || 0}
                </span>
                <span className="flex-1 text-center text-lg font-semibold">{torrentCounts?.total || 0}</span>
              </div>
            </div>
            
            <div className="flex items-center gap-2 text-xs">
              <Download className="h-3 w-3 text-muted-foreground" />
              <span className="text-muted-foreground">Download</span>
              <span className="ml-auto font-medium">{formatSpeed(stats.speeds?.download || 0)}</span>
            </div>
            
            <div className="flex items-center gap-2 text-xs">
              <Upload className="h-3 w-3 text-muted-foreground" />
              <span className="text-muted-foreground">Upload</span>
              <span className="ml-auto font-medium">{formatSpeed(stats.speeds?.upload || 0)}</span>
            </div>
          </div>
          
          <InstanceErrorDisplay instance={instance} />
        </CardContent>
      </Card>
    </Link>
  )
}

function GlobalStatsCards({ statsData }: { statsData: Array<{ instance: InstanceResponse, stats: InstanceStats | undefined, serverState: ServerState | null, torrentCounts: TorrentCounts | null | undefined }> }) {
  const globalStats = useMemo(() => {
    const connected = statsData.filter(({ stats }) => stats?.connected).length
    const totalTorrents = statsData.reduce((sum, { torrentCounts }) => 
      sum + (torrentCounts?.total || 0), 0)
    const activeTorrents = statsData.reduce((sum, { torrentCounts }) => 
      sum + (torrentCounts?.status?.active || 0), 0)
    const totalDownload = statsData.reduce((sum, { stats }) => 
      sum + (stats?.speeds?.download || 0), 0)
    const totalUpload = statsData.reduce((sum, { stats }) => 
      sum + (stats?.speeds?.upload || 0), 0)
    const totalErrors = statsData.reduce((sum, { torrentCounts }) => 
      sum + (torrentCounts?.status?.errored || 0), 0)
    
    // Calculate server stats
    const alltimeDl = statsData.reduce((sum, { serverState }) => 
      sum + (serverState?.alltime_dl || 0), 0)
    const alltimeUl = statsData.reduce((sum, { serverState }) => 
      sum + (serverState?.alltime_ul || 0), 0)
    const totalPeers = statsData.reduce((sum, { serverState }) => 
      sum + (serverState?.total_peer_connections || 0), 0)
    
    // Calculate global ratio
    let globalRatio = 0
    if (alltimeDl > 0) {
      globalRatio = alltimeUl / alltimeDl
    }

    return {
      connected,
      total: statsData.length,
      totalTorrents,
      activeTorrents,
      totalDownload,
      totalUpload,
      totalErrors,
      alltimeDl,
      alltimeUl,
      globalRatio,
      totalPeers,
    }
  }, [statsData])

  return (
    <>
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Instances</CardTitle>
          <HardDrive className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{globalStats.connected}/{globalStats.total}</div>
          <p className="text-xs text-muted-foreground">
            Connected instances
          </p>
          <Progress 
            value={(globalStats.connected / globalStats.total) * 100} 
            className="mt-2 h-1"
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Total Torrents</CardTitle>
          <Activity className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{globalStats.totalTorrents}</div>
          <p className="text-xs text-muted-foreground">
            {globalStats.activeTorrents} active
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Total Download</CardTitle>
          <Download className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{formatSpeed(globalStats.totalDownload)}</div>
          <p className="text-xs text-muted-foreground">
            All instances combined
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Total Upload</CardTitle>
          <Upload className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{formatSpeed(globalStats.totalUpload)}</div>
          <p className="text-xs text-muted-foreground">
            All instances combined
          </p>
        </CardContent>
      </Card>
    </>
  )
}

function GlobalAllTimeStats({ statsData }: { statsData: Array<{ instance: InstanceResponse, stats: InstanceStats | undefined, serverState: ServerState | null }> }) {
  const [isExpanded, setIsExpanded] = useState(false)
  const globalStats = useMemo(() => {
    // Calculate server stats
    const alltimeDl = statsData.reduce((sum, { serverState }) => 
      sum + (serverState?.alltime_dl || 0), 0)
    const alltimeUl = statsData.reduce((sum, { serverState }) => 
      sum + (serverState?.alltime_ul || 0), 0)
    const totalPeers = statsData.reduce((sum, { serverState }) => 
      sum + (serverState?.total_peer_connections || 0), 0)
    
    // Calculate global ratio
    let globalRatio = 0
    if (alltimeDl > 0) {
      globalRatio = alltimeUl / alltimeDl
    }

    return {
      alltimeDl,
      alltimeUl,
      globalRatio,
      totalPeers,
    }
  }, [statsData])

  // Apply color grading to ratio
  const ratioColor = getRatioColor(globalStats.globalRatio)

  // Don't show if no data
  if (globalStats.alltimeDl === 0 && globalStats.alltimeUl === 0) {
    return null
  }

  return (
    <div className="rounded-lg border bg-card">
      {/* Combined Stats Header - Clickable */}
      <div 
        className="p-4 cursor-pointer hover:bg-muted/50 transition-colors"
        onClick={() => setIsExpanded(!isExpanded)}
      >
        {/* Mobile layout */}
        <div className="sm:hidden">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              {isExpanded ? (
                <Minus className="h-3.5 w-3.5 text-muted-foreground" />
              ) : (
                <Plus className="h-3.5 w-3.5 text-muted-foreground" />
              )}
              <h3 className="text-sm font-medium text-muted-foreground">Server Statistics</h3>
            </div>
          </div>
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-4">
              <div className="flex items-center gap-1.5">
                <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
                <span className="text-sm font-semibold">{formatBytes(globalStats.alltimeDl)}</span>
              </div>
              <div className="flex items-center gap-1.5">
                <ChevronUp className="h-3.5 w-3.5 text-muted-foreground" />
                <span className="text-sm font-semibold">{formatBytes(globalStats.alltimeUl)}</span>
              </div>
            </div>
            <div className="flex items-center gap-4 text-sm">
              <div>
                <span className="text-xs text-muted-foreground">Ratio: </span>
                <span className="font-semibold" style={{ color: ratioColor }}>
                  {globalStats.globalRatio.toFixed(2)}
                </span>
              </div>
              {globalStats.totalPeers > 0 && (
                <div>
                  <span className="text-xs text-muted-foreground">Peers: </span>
                  <span className="font-semibold">{globalStats.totalPeers}</span>
                </div>
              )}
            </div>
          </div>
        </div>
        
        {/* Desktop layout */}
        <div className="hidden sm:flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div className="flex items-center gap-2">
            {isExpanded ? (
              <Minus className="h-4 w-4 text-muted-foreground" />
            ) : (
              <Plus className="h-4 w-4 text-muted-foreground" />
            )}
            <h3 className="text-base font-medium">Server Statistics</h3>
          </div>
          <div className="flex flex-wrap items-center gap-6 text-sm">
            <div className="flex items-center gap-2">
              <ChevronDown className="h-4 w-4 text-muted-foreground" />
              <span className="text-lg font-semibold">{formatBytes(globalStats.alltimeDl)}</span>
            </div>
            
            <div className="flex items-center gap-2">
              <ChevronUp className="h-4 w-4 text-muted-foreground" />
              <span className="text-lg font-semibold">{formatBytes(globalStats.alltimeUl)}</span>
            </div>
            
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Ratio:</span>
              <span className="text-lg font-semibold" style={{ color: ratioColor }}>
                {globalStats.globalRatio.toFixed(2)}
              </span>
            </div>
            
            {globalStats.totalPeers > 0 && (
              <div className="flex items-center gap-2">
                <span className="text-muted-foreground">Peers:</span>
                <span className="text-lg font-semibold">{globalStats.totalPeers}</span>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Individual Instance Stats - Expandable Table */}
      {isExpanded && (
        <div className="border-t">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent bg-muted/50">
                <TableHead className="text-center">Instance</TableHead>
                <TableHead className="text-center">
                  <div className="flex items-center justify-center gap-1">
                    <span>Downloaded</span>
                  </div>
                </TableHead>
                <TableHead className="text-center">
                  <div className="flex items-center justify-center gap-1">
                    <span>Uploaded</span>
                  </div>
                </TableHead>
                <TableHead className="text-center">Ratio</TableHead>
                <TableHead className="text-center hidden sm:table-cell">Peers</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {statsData
                .filter(({ serverState }) => serverState?.alltime_dl || serverState?.alltime_ul)
                .map(({ instance, serverState }) => {
                  const instanceRatio = serverState?.alltime_dl ? (serverState.alltime_ul || 0) / serverState.alltime_dl : 0
                  const instanceRatioColor = getRatioColor(instanceRatio)
                  
                  return (
                    <TableRow key={instance.id}>
                      <TableCell className="text-center font-medium">{instance.name}</TableCell>
                      <TableCell className="text-center font-semibold">
                        {formatBytes(serverState?.alltime_dl || 0)}
                      </TableCell>
                      <TableCell className="text-center font-semibold">
                        {formatBytes(serverState?.alltime_ul || 0)}
                      </TableCell>
                      <TableCell className="text-center font-semibold" style={{ color: instanceRatioColor }}>
                        {instanceRatio.toFixed(2)}
                      </TableCell>
                      <TableCell className="text-center font-semibold hidden sm:table-cell">
                        {serverState?.total_peer_connections || "-"}
                      </TableCell>
                    </TableRow>
                  )
                })}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}

function QuickActionsDropdown({ statsData }: { statsData: Array<{ instance: InstanceResponse, stats: InstanceStats | undefined, serverState: ServerState | null }> }) {
  const connectedInstances = statsData
    .filter(({ stats }) => stats?.connected)
    .map(({ instance }) => instance)

  if (connectedInstances.length === 0) {
    return null
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" className="w-full sm:w-auto">
          <Zap className="h-4 w-4 mr-2" />
          Quick Actions
          <ChevronDown className="h-3 w-3 ml-1" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel>Add Torrent</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {connectedInstances.map(instance => (
          <Link 
            key={instance.id} 
            to="/instances/$instanceId" 
            params={{ instanceId: instance.id.toString() }}
            search={{ modal: "add-torrent" }}
          >
            <DropdownMenuItem className="cursor-pointer active:bg-accent focus:bg-accent">
              <Plus className="h-4 w-4 mr-2" />
              <span>Add to {instance.name}</span>
            </DropdownMenuItem>
          </Link>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function Dashboard() {
  const { instances, isLoading } = useInstances()
  const allInstances = instances || []
  
  // Use safe hook that always calls the same number of hooks
  const statsData = useAllInstanceStats(allInstances)
  
  if (isLoading) {
    return (
      <div className="container mx-auto p-4 sm:p-6">
        <div className="animate-pulse space-y-4">
          <div className="h-8 bg-muted rounded w-48"></div>
          <div className="h-4 bg-muted rounded w-64"></div>
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
            {[...Array(4)].map((_, i) => (
              <div key={i} className="h-24 bg-muted rounded"></div>
            ))}
          </div>
        </div>
      </div>
    )
  }
  
  return (
    <div className="container mx-auto p-4 sm:p-6">
      {/* Header with Actions */}
      <div className="mb-6">
        <h1 className="text-2xl sm:text-3xl font-bold">Dashboard</h1>
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 mt-2">
          <p className="text-muted-foreground">
            Overview of all your qBittorrent instances
          </p>
          {instances && instances.length > 0 && (
            <div className="flex flex-col sm:flex-row gap-2 w-full sm:w-auto">
              <QuickActionsDropdown statsData={statsData} />
              <Link to="/instances" search={{ modal: "add-instance" }} className="w-full sm:w-auto">
                <Button variant="outline" size="sm" className="w-full sm:w-auto">
                  <Plus className="h-4 w-4 mr-2" />
                  Add Instance
                </Button>
              </Link>
            </div>
          )}
        </div>
      </div>
      
      {/* Show banner if any instances have decryption errors */}
      <PasswordIssuesBanner instances={instances || []} />
      
      {instances && instances.length > 0 ? (
        <div className="space-y-6">
          {/* Stats Bar */}
          <GlobalAllTimeStats statsData={statsData} />
          
          {/* Global Stats */}
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
            <GlobalStatsCards statsData={statsData} />
          </div>
          
          {/* Instance Cards */}
          {allInstances.length > 0 && (
            <div>
              <h2 className="text-xl font-semibold mb-4">Instances</h2>
              <div className="grid gap-4 sm:grid-cols-1 md:grid-cols-2 lg:grid-cols-3">
                {allInstances.map(instance => (
                  <InstanceCard key={instance.id} instance={instance} />
                ))}
              </div>
            </div>
          )}
        </div>
      ) : (
        <Card className="p-8 sm:p-12 text-center">
          <div className="space-y-4">
            <HardDrive className="h-12 w-12 mx-auto text-muted-foreground" />
            <div>
              <h3 className="text-lg font-semibold">No instances configured</h3>
              <p className="text-muted-foreground">Get started by adding your first qBittorrent instance</p>
            </div>
            <Link to="/instances" search={{ modal: "add-instance" }}>
              <Button>
                <Plus className="h-4 w-4 mr-2" />
                Add Instance
              </Button>
            </Link>
          </div>
        </Card>
      )}
    </div>
  )
}