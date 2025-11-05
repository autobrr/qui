/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { formatBytes } from "@/lib/utils"
import type {
  CrossSeedApplyResponse,
  CrossSeedTorrentSearchResponse,
  Torrent
} from "@/types"
import { Loader2 } from "lucide-react"

type CrossSeedSearchResult = CrossSeedTorrentSearchResponse["results"][number]

export interface CrossSeedDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  torrent: Torrent | null
  sourceTorrent?: CrossSeedTorrentSearchResponse["sourceTorrent"]
  results: CrossSeedSearchResult[]
  selectedKeys: Set<string>
  selectionCount: number
  isLoading: boolean
  isSubmitting: boolean
  error: string | null
  applyResult: CrossSeedApplyResponse | null
  getResultKey: (result: CrossSeedSearchResult, index: number) => string
  onToggleSelection: (result: CrossSeedSearchResult, index: number) => void
  onSelectAll: () => void
  onClearSelection: () => void
  onRetry: () => void
  onClose: () => void
  onApply: () => void
  useTag: boolean
  onUseTagChange: (value: boolean) => void
  tagName: string
  onTagNameChange: (value: string) => void
}

export function CrossSeedDialog({
  open,
  onOpenChange,
  torrent,
  sourceTorrent,
  results,
  selectedKeys,
  selectionCount,
  isLoading,
  isSubmitting,
  error,
  applyResult,
  getResultKey,
  onToggleSelection,
  onSelectAll,
  onClearSelection,
  onRetry,
  onClose,
  onApply,
  useTag,
  onUseTagChange,
  tagName,
  onTagNameChange,
}: CrossSeedDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[90vw] sm:max-w-3xl">
        <DialogHeader className="min-w-0">
          <DialogTitle>Search Cross-Seeds</DialogTitle>
          <DialogDescription className="min-w-0 truncate" title={torrent?.name}>
            {torrent ? `Indexers scanned for "${torrent.name}"` : "Indexers scanned"}
          </DialogDescription>
        </DialogHeader>
        <div className="min-w-0 space-y-4 overflow-hidden">
          {isLoading ? (
            <div className="flex items-center justify-center gap-3 py-12 text-sm text-muted-foreground">
              <Loader2 className="h-5 w-5 animate-spin" />
              <span>Searching indexers…</span>
            </div>
          ) : error ? (
            <div className="space-y-3 rounded-md border border-destructive/20 bg-destructive/10 p-4 text-sm text-destructive">
              <p className="break-words">{error}</p>
              <div className="flex gap-2">
                <Button size="sm" onClick={onRetry}>
                  Retry
                </Button>
                <Button size="sm" variant="outline" onClick={onClose}>
                  Close
                </Button>
              </div>
            </div>
          ) : (
            <>
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium" title={sourceTorrent?.name ?? torrent?.name}>
                    {sourceTorrent?.name ?? torrent?.name ?? "Torrent"}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {sourceTorrent?.category && (
                      <span className="mr-2">Category: {sourceTorrent.category}</span>
                    )}
                    {sourceTorrent?.size !== undefined && (
                      <span>Size: {formatBytes(sourceTorrent.size)}</span>
                    )}
                  </p>
                </div>
                <Badge variant="outline" className="shrink-0">
                  {selectionCount} / {results.length} selected
                </Badge>
              </div>
              {results.length === 0 ? (
                <div className="rounded-md border border-dashed p-8 text-center text-sm text-muted-foreground">
                  No matches found across the enabled indexers.
                </div>
              ) : (
                <>
                  <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
                    <span className="truncate">Select the releases you want to add</span>
                    <div className="flex shrink-0 gap-2">
                      <Button variant="outline" size="sm" onClick={onSelectAll}>
                        Select All
                      </Button>
                      <Button variant="outline" size="sm" onClick={onClearSelection}>
                        Clear
                      </Button>
                    </div>
                  </div>
                  <div className="max-h-72 space-y-2 overflow-x-hidden overflow-y-auto pr-1">
                    {results.map((result, index) => {
                      const key = getResultKey(result, index)
                      const checked = selectedKeys.has(key)
                      return (
                        <div key={key} className="flex items-start gap-3 rounded-md border p-3">
                          <Checkbox
                            checked={checked}
                            onCheckedChange={() => onToggleSelection(result, index)}
                            aria-label={`Select ${result.title}`}
                            className="shrink-0"
                          />
                          <div className="min-w-0 flex-1 space-y-1">
                            <div className="flex items-start justify-between gap-3">
                              <span className="min-w-0 flex-1 truncate font-medium leading-tight" title={result.title}>{result.title}</span>
                              <Badge variant="outline" className="shrink-0">{result.indexer}</Badge>
                            </div>
                            <div className="flex min-w-0 flex-wrap gap-x-3 text-xs text-muted-foreground">
                              <span className="shrink-0">{formatBytes(result.size)}</span>
                              <span className="shrink-0">{result.seeders} seeders</span>
                              {result.matchReason && <span className="min-w-0 truncate">Match: {result.matchReason}</span>}
                              <span className="shrink-0">{formatCrossSeedPublishDate(result.publishDate)}</span>
                            </div>
                          </div>
                        </div>
                      )
                    })}
                  </div>
                  <div className="flex items-center justify-between gap-3 rounded-md border p-3">
                    <div className="flex items-center gap-2 shrink-0">
                      <Switch
                        id="cross-seed-tag-toggle"
                        checked={useTag}
                        onCheckedChange={(value) => onUseTagChange(Boolean(value))}
                      />
                      <label htmlFor="cross-seed-tag-toggle" className="text-sm whitespace-nowrap">
                        Tag added torrents
                      </label>
                    </div>
                    <Input
                      value={tagName}
                      onChange={(event) => onTagNameChange(event.target.value)}
                      placeholder="cross-seed"
                      disabled={!useTag}
                      className="w-32 min-w-0"
                    />
                  </div>
                </>
              )}
              {applyResult && (
                <div className="min-w-0 space-y-2 rounded-md border p-3">
                  <p className="text-sm font-medium">Latest add attempt</p>
                  <div className="max-h-72 space-y-2 overflow-x-hidden overflow-y-auto pr-1">
                    {applyResult.results.map(result => (
                      <div
                        key={`${result.indexer}-${result.title}`}
                        className="min-w-0 space-y-1 rounded border border-border/60 bg-muted/30 p-3"
                      >
                        <div className="flex items-center justify-between gap-2 text-sm">
                          <span className="min-w-0 truncate">{result.indexer}</span>
                          <Badge variant={result.success ? "outline" : "destructive"} className="shrink-0">
                            {result.success ? "Queued" : "Check"}
                          </Badge>
                        </div>
                        <p className="truncate text-xs text-muted-foreground" title={result.torrentName ?? result.title}>{result.torrentName ?? result.title}</p>
                        {result.error && <p className="break-words text-xs text-destructive">{result.error}</p>}
                        {result.instanceResults && result.instanceResults.length > 0 && (
                          <ul className="mt-2 space-y-1 text-xs text-muted-foreground">
                            {result.instanceResults.map(instance => (
                              <li key={`${result.indexer}-${instance.instanceId}-${instance.status}`} className="break-words">
                                <span className="font-medium">{instance.instanceName}</span>:{" "}
                                {instance.message ?? instance.status}
                              </li>
                            ))}
                          </ul>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Close
          </Button>
          <Button
            onClick={onApply}
            disabled={
              isLoading ||
              isSubmitting ||
              results.length === 0 ||
              selectionCount === 0
            }
          >
            {isSubmitting ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                Adding…
              </>
            ) : (
              "Add Selected"
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function formatCrossSeedPublishDate(value: string): string {
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return value
  }
  return parsed.toLocaleString()
}
