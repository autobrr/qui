/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api } from "@/lib/api"
import { copyTextToClipboard } from "@/lib/utils"
import type { TorrentFile, TorrentFileMediaInfoResponse } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { Copy, Loader2, RotateCw } from "lucide-react"
import { useMemo, useState } from "react"
import { toast } from "sonner"

interface TorrentFileMediaInfoDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  torrentHash: string
  file: TorrentFile | null
}

function buildStreamLabels(streams: TorrentFileMediaInfoResponse["streams"]): string[] {
  const totals = new Map<string, number>()
  for (const stream of streams) {
    totals.set(stream.kind, (totals.get(stream.kind) ?? 0) + 1)
  }

  const seen = new Map<string, number>()
  return streams.map((stream) => {
    const next = (seen.get(stream.kind) ?? 0) + 1
    seen.set(stream.kind, next)
    const total = totals.get(stream.kind) ?? 0
    if (stream.kind !== "General" && total > 1) {
      return `${stream.kind} #${next}`
    }
    return stream.kind
  })
}

function formatSummary(data: TorrentFileMediaInfoResponse, streamLabels: string[]): string {
  const lines: string[] = []

  data.streams.forEach((stream, idx) => {
    const label = streamLabels[idx] ?? stream.kind
    const fields = stream.fields.filter((field) => field.value.trim() !== "")
    lines.push(label)
    for (const field of fields) {
      lines.push(`${field.name}: ${field.value}`)
    }
    lines.push("")
  })

  return lines.join("\n").trimEnd()
}

function ErrorRetryBlock({
  error,
  onRetry,
}: {
  error: unknown
  onRetry: () => void
}) {
  return (
    <div className="flex flex-col items-start gap-3 py-8">
      <p className="text-sm text-muted-foreground">
        {error instanceof Error ? error.message : "Failed to fetch MediaInfo"}
      </p>
      <Button variant="outline" size="sm" onClick={onRetry}>
        <RotateCw className="h-4 w-4 mr-2" />
        Retry
      </Button>
    </div>
  )
}

function QueryStateWrapper({
  isLoading,
  isError,
  error,
  onRetry,
  children,
}: {
  isLoading: boolean
  isError: boolean
  error: unknown
  onRetry: () => void
  children: React.ReactNode
}) {
  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Loader2 className="h-6 w-6 animate-spin" />
      </div>
    )
  }

  if (isError) {
    return <ErrorRetryBlock error={error} onRetry={onRetry} />
  }

  return children
}

export function TorrentFileMediaInfoDialog({
  open,
  onOpenChange,
  instanceId,
  torrentHash,
  file,
}: TorrentFileMediaInfoDialogProps) {
  const [tab, setTab] = useState<"summary" | "raw">("summary")

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) {
      setTab("summary")
    }
    onOpenChange(nextOpen)
  }

  const query = useQuery({
    queryKey: ["torrent-file-mediainfo", instanceId, torrentHash, file?.index],
    queryFn: () => api.getTorrentFileMediaInfo(instanceId, torrentHash, file!.index),
    enabled: open && !!file && !!torrentHash,
    staleTime: 30000,
    gcTime: 5 * 60 * 1000,
  })

  const streamLabels = useMemo(() => {
    const streams = query.data?.streams ?? []
    return buildStreamLabels(streams)
  }, [query.data?.streams])

  const summaryText = useMemo(() => {
    if (!query.data) return ""
    return formatSummary(query.data, streamLabels)
  }, [query.data, streamLabels])

  const prettyRawJSON = useMemo(() => {
    const raw = query.data?.rawJSON
    if (!raw) return ""
    try {
      return JSON.stringify(JSON.parse(raw), null, 2)
    } catch {
      return raw
    }
  }, [query.data?.rawJSON])

  const copyLabel = tab === "summary" ? "Copy Summary" : "Copy JSON"
  const copyText = tab === "summary" ? summaryText : prettyRawJSON
  const canCopy = !!copyText && !query.isLoading && !query.isError && !query.isFetching

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-lg md:max-w-5xl max-h-[85vh] overflow-hidden">
        <DialogHeader>
          <DialogTitle>MediaInfo</DialogTitle>
        </DialogHeader>

        <Tabs value={tab} onValueChange={(value) => setTab(value as typeof tab)} className="w-full">
          <div className="flex items-center justify-between gap-2 min-w-0 mb-4">
            <TabsList className="min-w-0">
              <TabsTrigger value="summary">Summary</TabsTrigger>
              <TabsTrigger value="raw">Raw JSON</TabsTrigger>
            </TabsList>

            <Button
              className="shrink-0"
              variant="outline"
              size="sm"
              onClick={async () => {
                if (!canCopy) return
                try {
                  await copyTextToClipboard(copyText)
                  toast.success(`${copyLabel} copied to clipboard`)
                } catch {
                  toast.error("Failed to copy to clipboard")
                }
              }}
              disabled={!canCopy}
            >
              <Copy className="h-4 w-4 mr-2" />
              {copyLabel}
            </Button>
          </div>

          <TabsContent value="summary" className="m-0">
            <ScrollArea className="h-[65vh] pr-4">
              <QueryStateWrapper
                isLoading={query.isLoading}
                isError={query.isError}
                error={query.error}
                onRetry={() => void query.refetch()}
              >
                {query.data ? (
                  <div className="space-y-6">
                    {query.data.streams.map((stream, idx) => {
                      const label = streamLabels[idx] ?? stream.kind
                      const fields = stream.fields.filter((field) => field.value.trim() !== "")

                      return (
                        <section key={`${stream.kind}-${idx}`} className="space-y-3">
                          <div className="flex items-center justify-between">
                            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                              {label}
                            </h3>
                          </div>

                          {fields.length === 0 ? (
                            <p className="text-sm text-muted-foreground">No fields</p>
                          ) : (
                            <div className="grid grid-cols-[minmax(10rem,1fr)_minmax(0,2fr)] gap-x-4 gap-y-1">
                              {fields.map((field, fieldIdx) => (
                                <div
                                  key={`${field.name}-${fieldIdx}`}
                                  className="contents"
                                >
                                  <div className="text-xs text-muted-foreground">
                                    {field.name}
                                  </div>
                                  <div className="text-xs break-words">
                                    {field.value}
                                  </div>
                                </div>
                              ))}
                            </div>
                          )}
                        </section>
                      )
                    })}
                  </div>
                ) : null}
              </QueryStateWrapper>
            </ScrollArea>
          </TabsContent>

          <TabsContent value="raw" className="m-0">
            <ScrollArea className="h-[65vh] pr-4">
              <QueryStateWrapper
                isLoading={query.isLoading}
                isError={query.isError}
                error={query.error}
                onRetry={() => void query.refetch()}
              >
                <pre className="rounded-md border bg-muted/30 p-3 text-xs font-mono whitespace-pre-wrap break-all">
                  {prettyRawJSON}
                </pre>
              </QueryStateWrapper>
            </ScrollArea>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}
