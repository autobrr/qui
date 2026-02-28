/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api } from "@/lib/api"
import { copyTextToClipboard } from "@/lib/utils"
import type { TorrentFile, TorrentFileMediaInfoResponse } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { Copy, Loader2, RotateCw } from "lucide-react"
import { useMemo } from "react"
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
  lines.push(data.relativePath)
  lines.push("")

  data.streams.forEach((stream, idx) => {
    const label = streamLabels[idx] ?? stream.kind
    const fields = stream.fields.filter((field) => field.value.trim() !== "")
    lines.push(`[${label}]`)
    for (const field of fields) {
      lines.push(`${field.name}: ${field.value}`)
    }
    lines.push("")
  })

  return lines.join("\n").trimEnd()
}

export function TorrentFileMediaInfoDialog({
  open,
  onOpenChange,
  instanceId,
  torrentHash,
  file,
}: TorrentFileMediaInfoDialogProps) {
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

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg md:max-w-3xl max-h-[85vh] overflow-hidden">
        <DialogHeader>
          <DialogTitle>MediaInfo</DialogTitle>
          <DialogDescription className="font-mono text-xs break-all">
            {query.data?.relativePath ?? file?.name ?? ""}
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="summary" className="w-full">
          <TabsList>
            <TabsTrigger value="summary">Summary</TabsTrigger>
            <TabsTrigger value="raw">Raw JSON</TabsTrigger>
          </TabsList>

          <TabsContent value="summary" className="m-0">
            <div className="flex items-center justify-end gap-2 mb-3">
              <Button
                variant="outline"
                size="sm"
                onClick={async () => {
                  if (!summaryText) return
                  try {
                    await copyTextToClipboard(summaryText)
                    toast.success("Summary copied to clipboard")
                  } catch {
                    toast.error("Failed to copy to clipboard")
                  }
                }}
                disabled={!summaryText}
              >
                <Copy className="h-4 w-4 mr-2" />
                Copy Summary
              </Button>
            </div>
            <ScrollArea className="h-[65vh] pr-4">
              {query.isLoading ? (
                <div className="flex items-center justify-center py-16">
                  <Loader2 className="h-6 w-6 animate-spin" />
                </div>
              ) : query.isError ? (
                <div className="flex flex-col items-start gap-3 py-8">
                  <p className="text-sm text-muted-foreground">
                    {query.error instanceof Error ? query.error.message : "Failed to fetch MediaInfo"}
                  </p>
                  <Button variant="outline" size="sm" onClick={() => void query.refetch()}>
                    <RotateCw className="h-4 w-4 mr-2" />
                    Retry
                  </Button>
                </div>
              ) : query.data ? (
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
                          <span className="text-[10px] text-muted-foreground">
                            {fields.length} field{fields.length !== 1 ? "s" : ""}
                          </span>
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
            </ScrollArea>
          </TabsContent>

          <TabsContent value="raw" className="m-0">
            <div className="flex items-center justify-end gap-2 mb-3">
              <Button
                variant="outline"
                size="sm"
                onClick={async () => {
                  const raw = query.data?.rawJSON
                  if (!raw) return
                  try {
                    await copyTextToClipboard(raw)
                    toast.success("Raw JSON copied to clipboard")
                  } catch {
                    toast.error("Failed to copy to clipboard")
                  }
                }}
                disabled={!query.data?.rawJSON}
              >
                <Copy className="h-4 w-4 mr-2" />
                Copy JSON
              </Button>
            </div>

            <ScrollArea className="h-[65vh] pr-4">
              {query.isLoading ? (
                <div className="flex items-center justify-center py-16">
                  <Loader2 className="h-6 w-6 animate-spin" />
                </div>
              ) : query.isError ? (
                <div className="flex flex-col items-start gap-3 py-8">
                  <p className="text-sm text-muted-foreground">
                    {query.error instanceof Error ? query.error.message : "Failed to fetch MediaInfo"}
                  </p>
                  <Button variant="outline" size="sm" onClick={() => void query.refetch()}>
                    <RotateCw className="h-4 w-4 mr-2" />
                    Retry
                  </Button>
                </div>
              ) : (
                <pre className="rounded-md border bg-muted/30 p-3 text-xs font-mono whitespace-pre-wrap break-words">
                  {query.data?.rawJSON ?? ""}
                </pre>
              )}
            </ScrollArea>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}
