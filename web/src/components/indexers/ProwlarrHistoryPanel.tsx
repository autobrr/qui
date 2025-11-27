/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardTitle } from "@/components/ui/card"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger
} from "@/components/ui/collapsible"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import type { ProwlarrHistoryEntry, ProwlarrServerHistory, TorznabIndexer } from "@/types"
import { useQuery } from "@tanstack/react-query"
import {
  AlertCircle,
  Check,
  ChevronDown,
  ChevronRight,
  Download,
  Loader2,
  RefreshCw,
  Rss,
  Search,
  X
} from "lucide-react"
import { useMemo, useState } from "react"

function formatRelativeTime(date: Date): string {
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHour = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHour / 24)

  if (diffSec < 60) return "just now"
  if (diffMin < 60) return `${diffMin}m ago`
  if (diffHour < 24) return `${diffHour}h ago`
  if (diffDay < 7) return `${diffDay}d ago`

  return date.toLocaleDateString()
}

interface ProwlarrHistoryPanelProps {
  indexers: TorznabIndexer[]
}

function EventTypeBadge({ eventType }: { eventType: string }) {
  switch (eventType) {
    case "IndexerRss":
      return (
        <Badge variant="secondary" className="gap-1 bg-blue-500/10 text-blue-600 dark:text-blue-400">
          <Rss className="h-3 w-3" />
          RSS
        </Badge>
      )
    case "IndexerQuery":
      return (
        <Badge variant="secondary" className="gap-1 bg-purple-500/10 text-purple-600 dark:text-purple-400">
          <Search className="h-3 w-3" />
          Search
        </Badge>
      )
    case "ReleaseGrabbed":
      return (
        <Badge variant="secondary" className="gap-1 bg-green-500/10 text-green-600 dark:text-green-400">
          <Download className="h-3 w-3" />
          Grab
        </Badge>
      )
    default:
      return (
        <Badge variant="outline">
          {eventType}
        </Badge>
      )
  }
}

function HistoryRow({ entry }: { entry: ProwlarrHistoryEntry }) {
  const [expanded, setExpanded] = useState(false)

  const query = entry.data.query || entry.data.grabTitle || "—"
  const source = entry.data.source || "—"
  const results = entry.data.queryResults
  const elapsed = entry.data.elapsedTime

  const displayQuery = query.length > 40 ? `${query.substring(0, 40)}...` : query

  return (
    <>
      <TableRow
        className="cursor-pointer hover:bg-muted/50"
        onClick={() => setExpanded(!expanded)}
      >
        <TableCell className="w-8">
          {expanded ? (
            <ChevronDown className="h-4 w-4 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-4 w-4 text-muted-foreground" />
          )}
        </TableCell>
        <TableCell className="whitespace-nowrap text-muted-foreground text-sm">
          {formatRelativeTime(new Date(entry.date))}
        </TableCell>
        <TableCell className="font-medium">{entry.indexerName}</TableCell>
        <TableCell><EventTypeBadge eventType={entry.eventType} /></TableCell>
        <TableCell>
          {entry.successful ? (
            <Check className="h-4 w-4 text-green-500" />
          ) : (
            <X className="h-4 w-4 text-destructive" />
          )}
        </TableCell>
        <TableCell className="max-w-[200px] truncate" title={query}>
          {displayQuery}
        </TableCell>
        <TableCell className="text-muted-foreground text-sm">{source}</TableCell>
        <TableCell className="text-right text-muted-foreground text-sm">
          {results && elapsed ? `${results} (${elapsed}ms)` : results || elapsed ? `${results || ""}${elapsed ? `${elapsed}ms` : ""}` : "—"}
        </TableCell>
      </TableRow>
      {expanded && (
        <TableRow className="bg-muted/20 hover:bg-muted/20">
          <TableCell colSpan={8} className="p-4">
            <div className="grid grid-cols-2 gap-2 text-sm md:grid-cols-3 lg:grid-cols-4">
              {Object.entries(entry.data)
                .sort(([a], [b]) => {
                  // Put url last since it's always long
                  if (a === "url") return 1
                  if (b === "url") return -1
                  return 0
                })
                .map(([key, value]) => (
                <div
                  key={key}
                  className={`rounded-md bg-muted/30 px-3 py-2 border border-border/30 overflow-hidden hover:bg-muted/50 transition-colors ${
                    key === "url" ? "col-span-2 md:col-span-3 lg:col-span-4" : ""
                  }`}
                >
                  <p className="text-[10px] font-semibold text-muted-foreground/70 uppercase tracking-wider">{key}</p>
                  <TooltipProvider>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <p className={`text-foreground mt-1 font-mono text-[13px] cursor-default ${key === "url" ? "truncate" : "truncate"}`}>
                          {value || "—"}
                        </p>
                      </TooltipTrigger>
                      {value && value.length > 30 && (
                        <TooltipContent side="bottom" className="max-w-md break-all font-mono text-xs">
                          {value}
                        </TooltipContent>
                      )}
                    </Tooltip>
                  </TooltipProvider>
                </div>
              ))}
            </div>
          </TableCell>
        </TableRow>
      )}
    </>
  )
}

function ServerHistorySection({ server, defaultOpen = true }: { server: ProwlarrServerHistory; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen)

  if (server.error) {
    return (
      <div className="rounded-lg border border-destructive/50 bg-destructive/10 p-4">
        <div className="flex items-center gap-2 text-destructive">
          <AlertCircle className="h-4 w-4" />
          <span className="font-medium">{server.serverName}</span>
        </div>
        <p className="mt-1 text-sm text-muted-foreground">{server.error}</p>
      </div>
    )
  }

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-center gap-2 rounded-lg border bg-muted/40 px-4 py-3 hover:bg-muted/60">
        {open ? (
          <ChevronDown className="h-4 w-4" />
        ) : (
          <ChevronRight className="h-4 w-4" />
        )}
        <span className="font-medium">{server.serverName}</span>
        <Badge variant="secondary" className="ml-auto">
          {server.records.length} / {server.totalRecords} events
        </Badge>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="mt-2 rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-8" />
                <TableHead className="w-[100px]">Time</TableHead>
                <TableHead>Indexer</TableHead>
                <TableHead className="w-[100px]">Type</TableHead>
                <TableHead className="w-[60px]">Status</TableHead>
                <TableHead>Query/Title</TableHead>
                <TableHead>Source</TableHead>
                <TableHead className="text-right w-[120px]">Results</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {server.records.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={8} className="text-center text-muted-foreground py-8">
                    No recent activity
                  </TableCell>
                </TableRow>
              ) : (
                server.records.map((entry) => (
                  <HistoryRow key={entry.id} entry={entry} />
                ))
              )}
            </TableBody>
          </Table>
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

export function ProwlarrHistoryPanel({ indexers }: ProwlarrHistoryPanelProps) {
  const [isOpen, setIsOpen] = useState(false)
  const [autoRefresh, setAutoRefresh] = useState(false)

  // Check if there are any Prowlarr indexers
  const hasProwlarrIndexers = useMemo(
    () => indexers.some((idx) => idx.backend === "prowlarr"),
    [indexers]
  )

  const {
    data: history,
    isLoading,
    isFetching,
    refetch
  } = useQuery({
    queryKey: ["prowlarr", "history"],
    queryFn: () => api.getProwlarrHistory(false),
    enabled: isOpen && hasProwlarrIndexers,
    staleTime: 5_000,
    refetchInterval: autoRefresh ? 5_000 : false
  })

  // Force refresh handler (bypasses cache)
  const handleRefresh = async () => {
    await api.getProwlarrHistory(true)
    refetch()
  }

  // Don't render if no Prowlarr indexers
  if (!hasProwlarrIndexers) {
    return null
  }

  const totalEvents = history?.servers.reduce((sum, s) => sum + s.records.length, 0) ?? 0

  return (
    <Card className="mt-4 !py-0 !gap-0">
      <Collapsible open={isOpen} onOpenChange={setIsOpen}>
        <CollapsibleTrigger className="flex w-full items-start justify-between text-left px-6 py-4 hover:bg-muted/50 transition-colors">
            <div className="py-1">
              <CardTitle className="flex items-center gap-2">
                {isOpen ? (
                  <ChevronDown className="h-5 w-5" />
                ) : (
                  <ChevronRight className="h-5 w-5" />
                )}
                Torznab Activity
              </CardTitle>
            </div>
            {isOpen && (
              <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant={autoRefresh ? "default" : "outline"}
                        size="sm"
                        onClick={() => setAutoRefresh(!autoRefresh)}
                      >
                        <RefreshCw className={`h-4 w-4 ${autoRefresh ? "animate-spin" : ""}`} />
                        {autoRefresh ? "Live" : "Off"}
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      {autoRefresh ? "Live updates (5s)" : "Enable live updates"}
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleRefresh}
                  disabled={isFetching}
                >
                  {isFetching ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <RefreshCw className="h-4 w-4" />
                  )}
                </Button>
              </div>
            )}
          </CollapsibleTrigger>
        <CollapsibleContent>
          <CardContent className="pt-0">
            {isLoading ? (
              <div className="flex items-center justify-center py-8">
                <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                <span className="ml-2 text-muted-foreground">Loading history...</span>
              </div>
            ) : !history || history.servers.length === 0 ? (
              <div className="text-center text-muted-foreground py-8">
                No Prowlarr servers configured
              </div>
            ) : (
              <div className="space-y-4">
                {totalEvents > 0 && (
                  <p className="text-sm text-muted-foreground">
                    Showing {totalEvents} events from {history.servers.length} server{history.servers.length !== 1 ? "s" : ""}
                  </p>
                )}
                {history.servers.length === 1 ? (
                  // Single server - show table directly without collapsible
                  <div className="rounded-lg border">
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead className="w-8" />
                          <TableHead className="w-[100px]">Time</TableHead>
                          <TableHead>Indexer</TableHead>
                          <TableHead className="w-[100px]">Type</TableHead>
                          <TableHead className="w-[60px]">Status</TableHead>
                          <TableHead>Query/Title</TableHead>
                          <TableHead>Source</TableHead>
                          <TableHead className="text-right w-[120px]">Results</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {history.servers[0].error ? (
                          <TableRow>
                            <TableCell colSpan={8} className="text-center text-destructive py-8">
                              <div className="flex items-center justify-center gap-2">
                                <AlertCircle className="h-4 w-4" />
                                {history.servers[0].error}
                              </div>
                            </TableCell>
                          </TableRow>
                        ) : history.servers[0].records.length === 0 ? (
                          <TableRow>
                            <TableCell colSpan={8} className="text-center text-muted-foreground py-8">
                              No recent activity
                            </TableCell>
                          </TableRow>
                        ) : (
                          history.servers[0].records.map((entry) => (
                            <HistoryRow key={entry.id} entry={entry} />
                          ))
                        )}
                      </TableBody>
                    </Table>
                  </div>
                ) : (
                  // Multiple servers - show grouped by server
                  history.servers.map((server, index) => (
                    <ServerHistorySection
                      key={server.serverUrl}
                      server={server}
                      defaultOpen={index === 0}
                    />
                  ))
                )}
              </div>
            )}
          </CardContent>
        </CollapsibleContent>
      </Collapsible>
    </Card>
  )
}
