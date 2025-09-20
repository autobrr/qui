/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useDebounce } from "@/hooks/useDebounce"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { LogEntry, PeerLogEntry } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { format } from "date-fns"
import { ChevronDown, Filter, RefreshCw, Search } from "lucide-react"
import { useEffect, useState } from "react"

interface LogViewerProps {
  instanceId: number
  instanceName?: string
  compact?: boolean
  maxHeight?: string
}

const LOG_LEVELS = [
  { value: 1, label: "Normal", color: "text-foreground" },
  { value: 2, label: "Info", color: "text-blue-600 dark:text-blue-400" },
  { value: 4, label: "Warning", color: "text-orange-600 dark:text-orange-400" },
  { value: 8, label: "Critical", color: "text-destructive" },
]

export function LogViewer({ instanceId, instanceName, compact = false, maxHeight = "600px" }: LogViewerProps) {
  const [activeTab, setActiveTab] = useState<"main" | "peers">("main")
  const [page, setPage] = useState(0)
  const [search, setSearch] = useState("")
  const [selectedLevels, setSelectedLevels] = useState<number[]>([1, 2, 4, 8])
  const [autoRefresh, setAutoRefresh] = useState(true)

  const debouncedSearch = useDebounce(search, 500)

  // Main logs query
  const mainLogsQuery = useQuery({
    queryKey: ["logs", "main", instanceId, page, debouncedSearch, selectedLevels],
    queryFn: () =>
      api.getMainLogs(instanceId, {
        page,
        limit: 100,
        search: debouncedSearch,
        levels: selectedLevels,
      }),
    enabled: activeTab === "main",
    refetchInterval: autoRefresh ? 5000 : false,
  })

  // Peer logs query
  const peerLogsQuery = useQuery({
    queryKey: ["logs", "peers", instanceId, page, debouncedSearch],
    queryFn: () =>
      api.getPeerLogs(instanceId, {
        page,
        limit: 100,
        search: debouncedSearch,
      }),
    enabled: activeTab === "peers",
    refetchInterval: autoRefresh ? 5000 : false,
  })

  const currentQuery = activeTab === "main" ? mainLogsQuery : peerLogsQuery

  // Reset page when search or filters change
  useEffect(() => {
    setPage(0)
  }, [debouncedSearch, selectedLevels])

  const toggleLevel = (level: number) => {
    setSelectedLevels(prev =>
      prev.includes(level)? prev.filter(l => l !== level): [...prev, level]
    )
  }

  const getLogLevelColor = (type: number) => {
    const level = LOG_LEVELS.find(l => l.value === type)
    return level?.color || "text-foreground"
  }

  const formatTimestamp = (timestamp: number) => {
    // Timestamp is in seconds, convert to milliseconds for Date
    return format(new Date(timestamp * 1000), "yyyy-MM-dd HH:mm:ss")
  }

  const renderMainLog = (log: LogEntry) => (
    <div
      key={log.id}
      className={cn(
        "px-3 py-2 border-b border-border last:border-b-0 hover:bg-accent hover:text-accent-foreground transition-colors",
        "flex items-start gap-3 text-sm"
      )}
    >
      <span className="text-xs text-muted-foreground min-w-[140px]">
        {formatTimestamp(log.timestamp)}
      </span>
      <Badge
        variant="outline"
        className={cn(
          "min-w-[70px] text-center",
          getLogLevelColor(log.type)
        )}
      >
        {LOG_LEVELS.find(l => l.value === log.type)?.label || "Unknown"}
      </Badge>
      <span className={cn("flex-1 break-words", getLogLevelColor(log.type))}>
        {log.message}
      </span>
    </div>
  )

  const renderPeerLog = (log: PeerLogEntry) => (
    <div
      key={log.id}
      className={cn(
        "px-3 py-2 border-b border-border last:border-b-0 hover:bg-accent hover:text-accent-foreground transition-colors",
        "flex items-start gap-3 text-sm",
        log.blocked && "text-destructive"
      )}
    >
      <span className="text-xs text-muted-foreground min-w-[140px]">
        {formatTimestamp(log.timestamp)}
      </span>
      <Badge
        variant={log.blocked ? "destructive" : "outline"}
        className="min-w-[70px] text-center"
      >
        {log.blocked ? "Blocked" : "Normal"}
      </Badge>
      <span className="font-mono min-w-[120px]">{log.ip}</span>
      <span className="flex-1 break-words">{log.reason || "-"}</span>
    </div>
  )

  if (compact) {
    // Compact view for Dashboard
    return (
      <Card>
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <CardTitle className="text-lg">
              Recent Logs {instanceName && `- ${instanceName}`}
            </CardTitle>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => currentQuery.refetch()}
              disabled={currentQuery.isFetching}
            >
              <RefreshCw className={cn("h-4 w-4", currentQuery.isFetching && "animate-spin")} />
            </Button>
          </div>
        </CardHeader>
        <CardContent className="pt-0">
          <ScrollArea className="h-[200px]">
            {currentQuery.isLoading ? (
              <div className="flex items-center justify-center py-8 text-muted-foreground">
                Loading logs...
              </div>
            ) : currentQuery.error ? (
              <div className="flex items-center justify-center py-8 text-destructive">
                Failed to load logs
              </div>
            ) : (
              <div>
                {currentQuery.data?.logs.slice(0, 10).map(log =>
                  activeTab === "main"? renderMainLog(log as LogEntry): renderPeerLog(log as PeerLogEntry)
                )}
              </div>
            )}
          </ScrollArea>
        </CardContent>
      </Card>
    )
  }

  // Full view
  return (
    <div className="space-y-4">
      <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as "main" | "peers")}>
        <div className="flex items-center justify-between mb-4">
          <TabsList>
            <TabsTrigger value="main">Main Logs</TabsTrigger>
            <TabsTrigger value="peers">Peer Logs</TabsTrigger>
          </TabsList>

          <div className="flex items-center gap-2">
            <div className="relative">
              <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="Search logs..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="pl-8 w-[250px]"
              />
            </div>

            {activeTab === "main" && (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="outline" size="sm">
                    <Filter className="h-4 w-4 mr-2" />
                    Levels ({selectedLevels.length})
                    <ChevronDown className="h-4 w-4 ml-2" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  {LOG_LEVELS.map(level => (
                    <DropdownMenuCheckboxItem
                      key={level.value}
                      checked={selectedLevels.includes(level.value)}
                      onCheckedChange={() => toggleLevel(level.value)}
                    >
                      <span className={level.color}>{level.label}</span>
                    </DropdownMenuCheckboxItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            )}

            <div className="flex items-center gap-2">
              <Checkbox
                id="auto-refresh"
                checked={autoRefresh}
                onCheckedChange={(checked) => setAutoRefresh(checked as boolean)}
              />
              <Label htmlFor="auto-refresh" className="text-sm">
                Auto-refresh
              </Label>
            </div>

            <Button
              variant="outline"
              size="sm"
              onClick={() => currentQuery.refetch()}
              disabled={currentQuery.isFetching}
            >
              <RefreshCw className={cn("h-4 w-4", currentQuery.isFetching && "animate-spin")} />
            </Button>
          </div>
        </div>

        <TabsContent value="main" className="mt-0">
          <Card>
            <ScrollArea style={{ height: maxHeight }}>
              {mainLogsQuery.isLoading ? (
                <div className="flex items-center justify-center py-12 text-muted-foreground">
                  Loading logs...
                </div>
              ) : mainLogsQuery.error ? (
                <div className="flex items-center justify-center py-12 text-destructive">
                  Failed to load logs
                </div>
              ) : mainLogsQuery.data?.logs.length === 0 ? (
                <div className="flex items-center justify-center py-12 text-muted-foreground">
                  No logs found
                </div>
              ) : (
                <div>
                  {(mainLogsQuery.data?.logs as LogEntry[])?.map(renderMainLog)}
                </div>
              )}
            </ScrollArea>
            {mainLogsQuery.data && mainLogsQuery.data.total > 0 && (
              <div className="flex items-center justify-between p-4 border-t">
                <div className="text-sm text-muted-foreground">
                  Showing {mainLogsQuery.data.logs.length} of {mainLogsQuery.data.total} logs
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage(Math.max(0, page - 1))}
                    disabled={page === 0}
                  >
                    Previous
                  </Button>
                  <span className="text-sm">Page {page + 1}</span>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage(page + 1)}
                    disabled={!mainLogsQuery.data.hasMore}
                  >
                    Next
                  </Button>
                </div>
              </div>
            )}
          </Card>
        </TabsContent>

        <TabsContent value="peers" className="mt-0">
          <Card>
            <ScrollArea style={{ height: maxHeight }}>
              {peerLogsQuery.isLoading ? (
                <div className="flex items-center justify-center py-12 text-muted-foreground">
                  Loading logs...
                </div>
              ) : peerLogsQuery.error ? (
                <div className="flex items-center justify-center py-12 text-destructive">
                  Failed to load logs
                </div>
              ) : peerLogsQuery.data?.logs.length === 0 ? (
                <div className="flex items-center justify-center py-12 text-muted-foreground">
                  No peer logs found
                </div>
              ) : (
                <div>
                  {(peerLogsQuery.data?.logs as PeerLogEntry[])?.map(renderPeerLog)}
                </div>
              )}
            </ScrollArea>
            {peerLogsQuery.data && peerLogsQuery.data.total > 0 && (
              <div className="flex items-center justify-between p-4 border-t">
                <div className="text-sm text-muted-foreground">
                  Showing {peerLogsQuery.data.logs.length} of {peerLogsQuery.data.total} logs
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage(Math.max(0, page - 1))}
                    disabled={page === 0}
                  >
                    Previous
                  </Button>
                  <span className="text-sm">Page {page + 1}</span>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage(page + 1)}
                    disabled={!peerLogsQuery.data.hasMore}
                  >
                    Next
                  </Button>
                </div>
              </div>
            )}
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}