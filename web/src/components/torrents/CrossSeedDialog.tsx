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
import { formatBytes } from "@/lib/utils"
import type {
  CrossSeedApplyResponse,
  CrossSeedTorrentSearchResponse,
  Torrent
} from "@/types"
import { ChevronDown, Loader2, SlidersHorizontal } from "lucide-react"
import { memo, useCallback, useMemo } from "react"

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

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[90vw] sm:max-w-3xl">
        <DialogHeader className="min-w-0">
          <DialogTitle>Search Cross-Seeds</DialogTitle>
          <DialogDescription className="min-w-0 truncate font-medium" title={torrent?.name}>
            <p className="truncate font-bold font-mono text-sm" title={sourceTorrent?.name ?? torrent?.name}>
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
          </DialogDescription>
        </DialogHeader>
        <div className="min-w-0 space-y-3 overflow-hidden">
          {/* Metadata - Always visible when available */}
          {sourceTorrent?.contentType && (
            <div className="flex flex-wrap items-center gap-2 px-1">
              <Badge variant="secondary" className="h-6 text-xs font-normal capitalize">
                {sourceTorrent.contentType}
              </Badge>
              {sourceTorrent.searchType && sourceTorrent.searchType !== sourceTorrent.contentType && (
                <Badge variant="outline" className="h-6 text-xs font-normal capitalize">
                  {sourceTorrent.searchType}
                </Badge>
              )}
            </div>
          )}

          {/* Search Scope Section */}
          <div className="rounded-lg border border-border/60 bg-muted/30 p-4">
            {indexerOptions.length > 0 ? (
              <CrossSeedScopeSelector
                indexerOptions={indexerOptions}
                indexerMode={indexerMode}
                selectedIndexerIds={selectedIndexerIds}
                onIndexerModeChange={onIndexerModeChange}
                onToggleIndexer={onToggleIndexer}
                onSelectAllIndexers={onSelectAllIndexers}
                onClearIndexerSelection={onClearIndexerSelection}
                onScopeSearch={onScopeSearch}
                isSearching={isLoading}
              />
            ) : sourceTorrent && (
              <div className="space-y-2 text-sm text-yellow-600 dark:text-yellow-400">
                <p className="font-medium">No compatible indexers found</p>
                <p className="text-xs">
                  None of your enabled indexers support the required capabilities ({sourceTorrent.requiredCaps?.join(", ")})
                  or categories ({sourceTorrent.searchCategories?.join(", ")}) for this {sourceTorrent.contentType} content.
                </p>
              </div>
            )}
          </div>

          {/* Content-based filtering info */}
          {excludedIndexerEntries.length > 0 && (
            <div className="rounded-lg border border-blue-200 bg-blue-50 p-3 text-sm dark:border-blue-800 dark:bg-blue-950">
              <div className="flex items-center gap-2 text-blue-700 dark:text-blue-300">
                <span className="font-medium">Smart Filtering Active</span>
                <Badge variant="secondary" className="text-xs">
                  {excludedIndexerEntries.length} {excludedIndexerEntries.length === 1 ? "indexer" : "indexers"} filtered
                </Badge>
              </div>
              <p className="mt-1 text-xs text-blue-600 dark:text-blue-400">
                You already seed this release from these trackers, so they’re excluded from the search.
              </p>
              <ul className="mt-2 ml-4 text-xs text-blue-600 dark:text-blue-400 space-y-1">
                {excludedIndexerEntries.map(entry => (
                  <li key={entry.id} className="break-words">
                    • {entry.name}
                  </li>
                ))}
              </ul>
            </div>
          )}
          {!hasSearched ? null : isLoading ? (
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
              <div className="flex items-start justify-end gap-4">
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

export const CrossSeedDialog = memo(CrossSeedDialogComponent)
CrossSeedDialog.displayName = "CrossSeedDialog"

function formatCrossSeedPublishDate(value: string): string {
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return value
  }
  return parsed.toLocaleString()
}

interface CrossSeedScopeSelectorProps {
  indexerOptions: CrossSeedIndexerOption[]
  indexerMode: "all" | "custom"
  selectedIndexerIds: number[]
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
  onIndexerModeChange,
  onToggleIndexer,
  onSelectAllIndexers,
  onClearIndexerSelection,
  onScopeSearch,
  isSearching,
}: CrossSeedScopeSelectorProps) => {
  const total = indexerOptions.length
  const selectedCount = selectedIndexerIds.length
  const disableCustomSelection = total === 0
  const scopeSearchDisabled = isSearching || (indexerMode === "custom" && selectedCount === 0)

  const statusText = useMemo(() => {
    const suffix = total === 1 ? "indexer" : "indexers"
    if (indexerMode === "all") {
      return `${total} compatible ${suffix}`
    }
    if (selectedCount === 0) {
      return "None selected"
    }
    return `${selectedCount} of ${total} selected`
  }, [indexerMode, total, selectedCount])

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
    <div className="space-y-3">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <SlidersHorizontal className="h-4 w-4 text-muted-foreground" />
          <h3 className="text-sm font-medium">Search Scope</h3>
        </div>
        <div className="text-xs text-muted-foreground">
          {statusText}
        </div>
      </div>

      {/* Controls */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        {/* Mode Selection */}
        <div className="flex items-center gap-2 rounded-md border border-border/60 bg-muted/30 p-1">
          <Button
            size="sm"
            variant={indexerMode === "all" ? "secondary" : "ghost"}
            onClick={handleAllIndexersClick}
            disabled={isSearching}
            className="h-8 flex-1 sm:flex-initial"
          >
            All Compatible
          </Button>
          <Button
            size="sm"
            variant={indexerMode === "custom" ? "secondary" : "ghost"}
            onClick={handleCustomIndexersClick}
            disabled={disableCustomSelection || isSearching}
            className="h-8 flex-1 sm:flex-initial"
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
                  className="h-8"
                >
                  {selectedCount > 0 ? `${selectedCount} selected` : "Select indexers"}
                  <ChevronDown className="ml-2 h-3 w-3" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent className="w-64" align="end">
                <DropdownMenuLabel>Available Indexers</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {indexerItems}
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={onSelectAllIndexers}>Select all</DropdownMenuItem>
                <DropdownMenuItem onClick={onClearIndexerSelection}>Clear selection</DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          <Button
            size="sm"
            onClick={onScopeSearch}
            disabled={scopeSearchDisabled}
            className="h-8"
          >
            {isSearching ? (
              <>
                <Loader2 className="mr-2 h-3 w-3 animate-spin" />
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
