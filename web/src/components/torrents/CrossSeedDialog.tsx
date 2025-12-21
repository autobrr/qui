/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { formatBytes, formatRelativeTime } from "@/lib/utils"
import type {
  CrossSeedApplyResponse,
  CrossSeedTorrentSearchResponse,
  Torrent
} from "@/types"
import { ChevronDown, ChevronRight, ExternalLink, Loader2, RefreshCw, SlidersHorizontal } from "lucide-react"
import { memo, useCallback, useEffect, useMemo, useState } from "react"

type CrossSeedSearchResult = CrossSeedTorrentSearchResponse["results"][number]
type CrossSeedIndexerOption = {
  id: number
  name: string
}

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
  indexerOptions: CrossSeedIndexerOption[]
  indexerMode: "all" | "custom"
  selectedIndexerIds: number[]
  indexerNameMap: Record<number, string>
  onIndexerModeChange: (mode: "all" | "custom") => void
  onToggleIndexer: (indexerId: number) => void
  onSelectAllIndexers: () => void
  onClearIndexerSelection: () => void
  onScopeSearch: () => void
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
  hasSearched: boolean
  cacheMetadata?: CrossSeedTorrentSearchResponse["cache"] | null
  canForceRefresh?: boolean
  refreshCooldownLabel?: string
  onForceRefresh?: () => void
}

const CrossSeedDialogComponent = ({
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
  indexerOptions,
  indexerMode,
  selectedIndexerIds,
  indexerNameMap,
  onIndexerModeChange,
  onToggleIndexer,
  onSelectAllIndexers,
  onClearIndexerSelection,
  onScopeSearch,
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
  hasSearched,
  cacheMetadata,
  canForceRefresh,
  refreshCooldownLabel,
  onForceRefresh,
}: CrossSeedDialogProps) => {
  const excludedIndexerEntries = useMemo(() => {
    if (!sourceTorrent?.excludedIndexers) {
      return []
    }

    return Object.keys(sourceTorrent.excludedIndexers)
      .map(id => Number(id))
      .filter(id => !Number.isNaN(id))
      .map(id => ({
        id,
        name: indexerNameMap[id] ?? `Indexer ${id}`,
      }))
      .sort((a, b) => a.name.localeCompare(b.name))
  }, [indexerNameMap, sourceTorrent?.excludedIndexers])

  const capabilityFilteredIndexerEntries = useMemo(() => {
    const available = sourceTorrent?.availableIndexers
    if (!available || available.length === 0) {
      return []
    }
    const supported = new Set(available)
    return Object.keys(indexerNameMap)
      .map(id => Number(id))
      .filter(id => !Number.isNaN(id) && !supported.has(id))
      .map(id => ({
        id,
        name: indexerNameMap[id] ?? `Indexer ${id}`,
      }))
      .sort((a, b) => a.name.localeCompare(b.name))
  }, [indexerNameMap, sourceTorrent?.availableIndexers])

  const [excludedOpen, setExcludedOpen] = useState(false)
  const [applyResultOpen, setApplyResultOpen] = useState(true)

  // Auto-expand results when there are failures
  const hasFailures = applyResult?.results.some(r => !r.success || r.instanceResults?.some(ir => !ir.success))
  useEffect(() => {
    if (hasFailures) {
      setApplyResultOpen(true)
    }
  }, [hasFailures])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] max-w-[90vw] sm:max-w-3xl flex flex-col">
        <DialogHeader className="min-w-0 shrink-0 pb-3">
          <DialogTitle className="text-base">Search Cross-Seeds</DialogTitle>
          <DialogDescription className="min-w-0 space-y-1">
            <p className="truncate font-mono text-xs font-medium" title={sourceTorrent?.name ?? torrent?.name}>
              {sourceTorrent?.name ?? torrent?.name ?? "Torrent"}
            </p>
            {(sourceTorrent?.category || sourceTorrent?.size !== undefined || sourceTorrent?.contentType) && (
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
                {sourceTorrent?.contentType && (
                  <Badge variant="secondary" className="h-5 text-xs font-normal capitalize">
                    {sourceTorrent.contentType}
                  </Badge>
                )}
                {sourceTorrent?.category && <span>Category: {sourceTorrent.category}</span>}
                {sourceTorrent?.size !== undefined && <span>Size: {formatBytes(sourceTorrent.size)}</span>}
              </div>
            )}
          </DialogDescription>
          {cacheMetadata && (
            <div className="mt-2 space-y-2 rounded-lg border border-dashed border-border/70 p-2 text-xs text-muted-foreground">
              <div className="flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between">
                <span>
                  {cacheMetadata.hit ? "Served from cache" : "Fresh search"} · {cacheMetadata.scope?.replace("_", " ") ?? "torznab"}
                </span>
                <span>
                  Cached {formatRelativeTime(cacheMetadata.cachedAt)} · Expires {formatRelativeTime(cacheMetadata.expiresAt)}
                </span>
              </div>
              {onForceRefresh && (
                <div className="flex flex-wrap items-center gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={!canForceRefresh}
                    onClick={onForceRefresh}
                  >
                    <RefreshCw className="mr-2 h-4 w-4" />
                    Refresh from indexers
                  </Button>
                  {!canForceRefresh && refreshCooldownLabel && (
                    <span className="text-[11px] text-muted-foreground">{refreshCooldownLabel}</span>
                  )}
                </div>
              )}
            </div>
          )}
        </DialogHeader>
        <div className="min-w-0 space-y-2 overflow-y-auto overflow-x-hidden flex-1">
          {/* Search Scope Section */}
          <div className="rounded-lg border border-border/60 bg-muted/30 p-2.5">
            {indexerOptions.length > 0 ? (
              <CrossSeedScopeSelector
                indexerOptions={indexerOptions}
                indexerMode={indexerMode}
                selectedIndexerIds={selectedIndexerIds}
                excludedIndexerIds={excludedIndexerEntries.map(entry => entry.id)}
                contentFilteringCompleted={sourceTorrent?.contentFilteringCompleted ?? false}
                onIndexerModeChange={onIndexerModeChange}
                onToggleIndexer={onToggleIndexer}
                onSelectAllIndexers={onSelectAllIndexers}
                onClearIndexerSelection={onClearIndexerSelection}
                onScopeSearch={onScopeSearch}
                isSearching={isLoading}
              />
            ) : (
              <div className="space-y-1.5 text-sm text-muted-foreground">
                <p className="font-medium">No Torznab indexers available</p>
                <p className="text-xs">
                  Add or enable Torznab indexers in Settings to search for cross-seeds.
                </p>
              </div>
            )}
            {(capabilityFilteredIndexerEntries.length > 0 && indexerOptions.length > 0) && (
              <div className="mt-2 rounded-md border border-dashed border-border/60 bg-muted/30 p-2 text-xs text-muted-foreground">
                <p className="font-medium text-[11px] text-foreground">Capability note</p>
                <p>
                  These indexers lack the required capabilities/categories for this torrent and will be skipped by the server:
                </p>
                <ul className="mt-1.5 ml-4 space-y-0.5">
                  {capabilityFilteredIndexerEntries.map(entry => (
                    <li key={entry.id} className="break-words">• {entry.name}</li>
                  ))}
                </ul>
              </div>
            )}
          </div>

          {/* Content-based filtering info */}
          {(sourceTorrent && !sourceTorrent.contentFilteringCompleted) || excludedIndexerEntries.length > 0 ? (
            <Collapsible open={excludedOpen} onOpenChange={setExcludedOpen}>
              <div className="rounded-lg border bg-accent/10">
                <CollapsibleTrigger className="w-full p-2.5 text-left hover:bg-accent/20 transition-colors">
                  <div className="flex items-center gap-2 text-sm text-accent-foreground">
                    <ChevronRight className={`h-3.5 w-3.5 transition-transform ${excludedOpen ? "rotate-90" : ""}`} />
                    {!sourceTorrent?.contentFilteringCompleted ? (
                      <>
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        <span className="font-medium">Content Filtering In Progress</span>
                        <Badge variant="secondary" className="text-xs">
                          Analyzing existing content...
                        </Badge>
                      </>
                    ) : (
                      <>
                        <span className="font-medium">Smart Filtering Active</span>
                        <Badge variant="secondary" className="text-xs">
                          {excludedIndexerEntries.length} {excludedIndexerEntries.length === 1 ? "indexer" : "indexers"} filtered
                        </Badge>
                      </>
                    )}
                  </div>
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <div className="px-2.5 pb-2.5">
                    {!sourceTorrent?.contentFilteringCompleted ? (
                      <p className="text-xs text-muted-foreground">
                        Checking your existing torrents to find duplicates and exclude redundant trackers. This helps avoid downloading the same content multiple times.
                      </p>
                    ) : excludedIndexerEntries.length > 0 ? (
                      <>
                        <p className="text-xs text-muted-foreground">
                          You already seed this release from these trackers, so they're excluded from the search.
                        </p>
                        <ul className="mt-2 ml-4 text-xs text-muted-foreground space-y-0.5">
                          {excludedIndexerEntries.map(entry => (
                            <li key={entry.id} className="break-words">
                              • {entry.name}
                            </li>
                          ))}
                        </ul>
                      </>
                    ) : (
                      <p className="text-xs text-muted-foreground">
                        Content filtering completed. No duplicate content found on your enabled trackers.
                      </p>
                    )}
                  </div>
                </CollapsibleContent>
              </div>
            </Collapsible>
          ) : null}
          {!hasSearched ? null : isLoading ? (
            <div className="flex items-center justify-center gap-2.5 py-8 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              <span>Searching indexers…</span>
            </div>
          ) : error ? (
            <div className="space-y-2 rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
              <p className="break-words text-xs">{error}</p>
              {(error.includes('rate limit') || error.includes('429') || error.includes('too many requests') || 
                error.includes('cooldown') || error.includes('rate-limited')) ? (
                <div className="text-xs text-muted-foreground space-y-1">
                  <p><strong>Why this happens:</strong> Trackers limit request frequency to prevent abuse and bans.</p>
                  <p><strong>What you can do:</strong></p>
                  <ul className="list-disc list-inside space-y-0.5 ml-2">
                    <li>Wait 30-60 minutes before trying again</li>
                    <li>Try searching with fewer indexers selected</li>
                    <li>Use the RSS automation feature for ongoing cross-seeding</li>
                    <li>Check the indexers page to see which ones are rate-limited</li>
                  </ul>
                </div>
              ) : null}
              <div className="flex gap-2">
                <Button size="sm" onClick={onRetry} className="h-7">
                  Retry
                </Button>
                <Button size="sm" variant="outline" onClick={onClose} className="h-7">
                  Close
                </Button>
              </div>
            </div>
          ) : (
            <>
              {results.length === 0 ? (
                <div className="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">
                  No matches found in your search.
                </div>
              ) : (
                <>
                  <div className="flex items-center justify-between gap-2 text-xs">
                    <span className="truncate text-muted-foreground">Select the releases you want to add</span>
                    <div className="flex shrink-0 items-center gap-2">
                      <Badge variant="outline" className="shrink-0 text-xs">
                        {selectionCount} / {results.length}
                      </Badge>
                      <Button variant="outline" size="sm" onClick={onSelectAll} className="h-7">
                        Select All
                      </Button>
                      <Button variant="outline" size="sm" onClick={onClearSelection} className="h-7">
                        Clear
                      </Button>
                    </div>
                  </div>
                  <div className="space-y-2">
                    {results.map((result, index) => {
                      const key = getResultKey(result, index)
                      const checked = selectedKeys.has(key)
                      return (
                        <div key={key} className="flex items-start gap-2.5 rounded-md border p-2.5">
                          <Checkbox
                            checked={checked}
                            onCheckedChange={() => onToggleSelection(result, index)}
                            aria-label={`Select ${result.title}`}
                            className="shrink-0 mt-0.5"
                          />
                          <div className="min-w-0 flex-1 space-y-1">
                            <div className="flex items-start justify-between gap-2">
                              {result.infoUrl ? (
                                <a
                                  href={result.infoUrl}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="min-w-0 flex-1 font-medium text-sm leading-tight text-primary hover:underline inline-flex items-center gap-1"
                                  title={result.title}
                                >
                                  <span className="truncate">{result.title}</span>
                                  <ExternalLink className="h-3 w-3 shrink-0 opacity-50" />
                                </a>
                              ) : (
                                <span className="min-w-0 flex-1 truncate font-medium text-sm leading-tight" title={result.title}>{result.title}</span>
                              )}
                              <Badge variant="outline" className="shrink-0 text-xs">{result.indexer}</Badge>
                            </div>
                            <div className="flex min-w-0 flex-wrap gap-x-2.5 text-xs text-muted-foreground">
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
                  <div className="flex items-center justify-between gap-3 rounded-md border p-2.5">
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
                      className="w-32 min-w-0 h-8"
                    />
                  </div>
                </>
              )}
              {applyResult && (
                <Collapsible open={applyResultOpen} onOpenChange={setApplyResultOpen}>
                  <div className="min-w-0 space-y-2 rounded-md border">
                    <CollapsibleTrigger className="w-full px-3 pt-2.5 pb-2 text-left hover:bg-muted/50 transition-colors">
                      <div className="flex items-center gap-2">
                        <ChevronRight className={`h-3.5 w-3.5 transition-transform ${applyResultOpen ? "rotate-90" : ""}`} />
                        <p className="text-sm font-medium">Latest add attempt</p>
                        <Badge variant="outline" className="text-xs">
                          {applyResult.results.length}
                        </Badge>
                      </div>
                    </CollapsibleTrigger>
                    <CollapsibleContent>
                      <div className="px-3 pb-2.5 space-y-2">
                        {applyResult.results.map(result => (
                          <div
                            key={`${result.indexer}-${result.title}`}
                            className="min-w-0 space-y-1 rounded border border-border/60 bg-muted/30 p-2.5"
                          >
                            <div className="flex items-center justify-between gap-2 text-sm">
                              <span className="min-w-0 truncate">{result.indexer}</span>
                              <Badge variant={result.success ? "outline" : "destructive"} className="shrink-0 text-xs">
                                {result.success ? "Queued" : "Check"}
                              </Badge>
                            </div>
                            <p className="truncate text-xs text-muted-foreground" title={result.torrentName ?? result.title}>{result.torrentName ?? result.title}</p>
                            {result.error && <p className="break-words text-xs text-destructive">{result.error}</p>}
                            {result.instanceResults && result.instanceResults.length > 0 && (
                              <ul className="mt-1.5 space-y-1 text-xs">
                                {result.instanceResults.map(instance => {
                                  const statusDisplay = getInstanceStatusDisplay(instance.status, instance.success)
                                  return (
                                    <li key={`${result.indexer}-${instance.instanceId}-${instance.status}`} className="flex flex-col gap-0.5">
                                      <div className="flex items-center gap-1.5">
                                        <span className="font-medium">{instance.instanceName}</span>
                                        <Badge
                                          variant={statusDisplay.variant === "success" ? "outline" : statusDisplay.variant === "warning" ? "secondary" : "destructive"}
                                          className="text-[10px] h-4 px-1"
                                        >
                                          {statusDisplay.text}
                                        </Badge>
                                      </div>
                                      {instance.message && instance.message !== instance.status && (
                                        <span className={`break-words pl-0.5 ${instance.success ? "text-muted-foreground" : "text-destructive/80"}`}>
                                          {instance.message}
                                        </span>
                                      )}
                                    </li>
                                  )
                                })}
                              </ul>
                            )}
                          </div>
                        ))}
                      </div>
                    </CollapsibleContent>
                  </div>
                </Collapsible>
              )}
            </>
          )}
        </div>
        <DialogFooter className="shrink-0">
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

export const CrossSeedDialog = memo(CrossSeedDialogComponent)
CrossSeedDialog.displayName = "CrossSeedDialog"

function formatCrossSeedPublishDate(value: string): string {
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return value
  }
  return parsed.toLocaleString()
}

// Maps instance status codes to user-friendly display information
function getInstanceStatusDisplay(status: string, success: boolean): { text: string; variant: "default" | "success" | "warning" | "destructive" } {
  switch (status) {
    case "added":
      return { text: "Added", variant: "success" }
    case "exists":
      return { text: "Already exists", variant: "warning" }
    case "no_match":
      return { text: "No match", variant: "destructive" }
    case "no_save_path":
      return { text: "No save path", variant: "destructive" }
    case "invalid_content_path":
      return { text: "Invalid path", variant: "destructive" }
    case "skipped_recheck":
      return { text: "Skipped - recheck required", variant: "destructive" }
    case "error":
      return { text: "Error", variant: "destructive" }
    default:
      // For unknown status, use success flag to determine variant
      return { text: status, variant: success ? "success" : "destructive" }
  }
}

interface CrossSeedScopeSelectorProps {
  indexerOptions: CrossSeedIndexerOption[]
  indexerMode: "all" | "custom"
  selectedIndexerIds: number[]
  excludedIndexerIds: number[]
  contentFilteringCompleted: boolean
  onIndexerModeChange: (mode: "all" | "custom") => void
  onToggleIndexer: (indexerId: number) => void
  onSelectAllIndexers: () => void
  onClearIndexerSelection: () => void
  onScopeSearch: () => void
  isSearching: boolean
}

// Memoized indexer option component to prevent re-rendering
const IndexerCheckboxItem = memo(({
  option,
  isChecked,
  onToggle,
}: {
  option: CrossSeedIndexerOption
  isChecked: boolean
  onToggle: (id: number) => void
}) => {
  const handleChange = useCallback(() => {
    onToggle(option.id)
  }, [onToggle, option.id])

  return (
    <DropdownMenuCheckboxItem
      key={option.id}
      checked={isChecked}
      onCheckedChange={handleChange}
      onSelect={(event) => event.preventDefault()} // keep menu open for multi-select
    >
      {option.name}
    </DropdownMenuCheckboxItem>
  )
})
IndexerCheckboxItem.displayName = "IndexerCheckboxItem"

const CrossSeedScopeSelector = memo(({
  indexerOptions,
  indexerMode,
  selectedIndexerIds,
  excludedIndexerIds,
  contentFilteringCompleted,
  onIndexerModeChange,
  onToggleIndexer,
  onSelectAllIndexers,
  onClearIndexerSelection,
  onScopeSearch,
  isSearching,
}: CrossSeedScopeSelectorProps) => {
  const total = indexerOptions.length
  const selectedCount = selectedIndexerIds.length
  const excludedCount = excludedIndexerIds.length
  const disableCustomSelection = total === 0
  const filteringInProgress = !contentFilteringCompleted
  const scopeSearchDisabled = filteringInProgress || isSearching || (indexerMode === "custom" && selectedCount === 0)

  // Calculate the actual count of indexers that will be used for search
  const searchIndexerCount = useMemo(() => {
    if (indexerMode === "all") {
      return total
    }
    return selectedCount
  }, [indexerMode, total, selectedCount])

  const statusText = useMemo(() => {
    const suffix = total === 1 ? "indexer" : "indexers"
    if (indexerMode === "all") {
      return `${total} enabled ${suffix}`
    }
    if (selectedCount === 0) {
      return "None selected"
    }
    return `${selectedCount} of ${total} selected`
  }, [indexerMode, total, selectedCount])

  const searchText = useMemo(() => {
    const suffix = searchIndexerCount === 1 ? "indexer" : "indexers"
    return `${searchIndexerCount} ${suffix} for search`
  }, [searchIndexerCount])

  // Memoize the dropdown items to prevent recreation on each render
  const indexerItems = useMemo(
    () =>
      indexerOptions.map(option => (
        <IndexerCheckboxItem
          key={option.id}
          option={option}
          isChecked={selectedIndexerIds.includes(option.id)}
          onToggle={onToggleIndexer}
        />
      )),
    [indexerOptions, selectedIndexerIds, onToggleIndexer]
  )

  // Memoize button callbacks
  const handleAllIndexersClick = useCallback(() => {
    onIndexerModeChange("all")
  }, [onIndexerModeChange])

  const handleCustomIndexersClick = useCallback(() => {
    onIndexerModeChange("custom")
  }, [onIndexerModeChange])

  return (
    <div className="space-y-2">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5">
          <SlidersHorizontal className="h-3.5 w-3.5 text-muted-foreground" />
          <h3 className="text-xs font-medium">Search Scope</h3>
        </div>
        <div className="flex flex-col items-end gap-0.5">
          <div className="text-xs text-muted-foreground">
            {statusText}
          </div>
          {contentFilteringCompleted && excludedCount > 0 && (
            <div className="text-xs font-medium text-primary">
              {searchText}
            </div>
          )}
        </div>
      </div>

      {/* Controls */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        {/* Mode Selection */}
        <div className="flex items-center gap-1.5 rounded-md border border-border/60 bg-muted/30 p-0.5">
          <Button
            size="sm"
            variant={indexerMode === "all" ? "secondary" : "ghost"}
            onClick={handleAllIndexersClick}
            disabled={isSearching}
            className="h-7 flex-1 sm:flex-initial text-xs"
          >
            All indexers
          </Button>
          <Button
            size="sm"
            variant={indexerMode === "custom" ? "secondary" : "ghost"}
            onClick={handleCustomIndexersClick}
            disabled={disableCustomSelection || isSearching}
            className="h-7 flex-1 sm:flex-initial text-xs"
          >
            Select Custom
          </Button>
        </div>

        {/* Custom Selection Dropdown + Search */}
        <div className="flex items-center gap-2">
          {indexerMode === "custom" && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={isSearching}
                  className="h-7 text-xs"
                >
                  {selectedCount > 0 ? `${selectedCount} selected` : "Select indexers"}
                  <ChevronDown className="ml-1.5 h-3 w-3" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent className="w-64" align="end">
                <DropdownMenuLabel className="text-xs">Available Indexers</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {indexerItems}
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  onSelect={(event) => event.preventDefault()}
                  onClick={onSelectAllIndexers}
                  className="text-xs"
                >
                  Select all
                </DropdownMenuItem>
                <DropdownMenuItem
                  onSelect={(event) => event.preventDefault()}
                  onClick={onClearIndexerSelection}
                  className="text-xs"
                >
                  Clear selection
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          <Button
            size="sm"
            onClick={onScopeSearch}
            disabled={scopeSearchDisabled}
            className="h-7 text-xs"
          >
            {filteringInProgress ? (
              <>
                <Loader2 className="mr-1.5 h-3 w-3 animate-spin" />
                Filtering…
              </>
            ) : isSearching ? (
              <>
                <Loader2 className="mr-1.5 h-3 w-3 animate-spin" />
                Searching
              </>
            ) : (
              "Search"
            )}
          </Button>
        </div>
      </div>
    </div>
  )
})
CrossSeedScopeSelector.displayName = "CrossSeedScopeSelector"
