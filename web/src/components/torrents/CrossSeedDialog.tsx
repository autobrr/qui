/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger
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
import { api } from "@/lib/api"
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import { formatBytes } from "@/lib/utils"
import type {
  CrossSeedApplyResponse,
  CrossSeedTorrentSearchResponse,
  Torrent
} from "@/types"
import { AlertTriangle, ChevronDown, ChevronRight, ExternalLink, Loader2, RefreshCw, SlidersHorizontal } from "lucide-react"
import { memo, useCallback, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

type CrossSeedSearchResult = CrossSeedTorrentSearchResponse["results"][number]
type CrossSeedIndexerOption = {
  id: number
  name: string
}

function getBlocklistPendingKey(instanceId: number, infoHash: string): string {
  return `${instanceId}:${infoHash}`
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
  startPaused: boolean
  onStartPausedChange: (value: boolean) => void
  hasSearched: boolean
  cacheMetadata?: CrossSeedTorrentSearchResponse["cache"] | null
  partial?: boolean
  queryDegraded?: string
  canForceRefresh?: boolean
  refreshCooldownLabel?: string
  onForceRefresh?: () => void
}

// Backend TorrentSearchResponse.query_degraded values; unknown values render nothing.
const queryDegradedMessageKeys: Record<string, string> = {
  arr_lookup_failed: "crossSeedDialog.arrLookupFailed",
  arr_no_ids: "crossSeedDialog.arrNoIds",
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
  startPaused,
  onStartPausedChange,
  hasSearched,
  cacheMetadata,
  partial,
  queryDegraded,
  canForceRefresh,
  refreshCooldownLabel,
  onForceRefresh,
}: CrossSeedDialogProps) => {
  const { t } = useTranslation(["torrents", "settings", "crossseed"])
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
  const [blocklistPendingKeys, setBlocklistPendingKeys] = useState<Set<string>>(() => new Set())

  // Auto-expand results when there are failures
  const hasFailures = applyResult?.results.some(r => !r.success || r.instanceResults?.some(ir => !ir.success))
  useEffect(() => {
    if (hasFailures) {
      setApplyResultOpen(true)
    }
  }, [hasFailures])

  const handleBlockInfoHash = useCallback(async (instanceId: number, infoHash: string) => {
    if (instanceId <= 0) {
      toast.error(t("crossSeedDialog.missingInstance"))
      return
    }
    const pendingKey = getBlocklistPendingKey(instanceId, infoHash)
    setBlocklistPendingKeys(prev => new Set(prev).add(pendingKey))
    try {
      await api.addCrossSeedBlocklist({ instanceId, infoHash })
      toast.success(t("crossSeedDialog.addedToBlocklist"))
    } catch (error) {
      const message = error instanceof Error ? error.message : t("crossSeedDialog.addedToBlocklist")
      toast.error(message)
    } finally {
      setBlocklistPendingKeys((prev) => {
        const next = new Set(prev)
        next.delete(pendingKey)
        return next
      })
    }
  }, [t])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] max-w-[90vw] sm:max-w-3xl flex flex-col">
        <DialogHeader className="min-w-0 shrink-0 pb-3">
          <DialogTitle className="text-base">{t("crossSeedDialog.title")}</DialogTitle>
          <DialogDescription className="min-w-0 space-y-1">
            <p className="truncate font-mono text-xs font-medium" title={sourceTorrent?.name ?? torrent?.name}>
              {sourceTorrent?.name ?? torrent?.name ?? t("crossSeedDialog.torrentLabel")}
            </p>
            {(sourceTorrent?.category || sourceTorrent?.size !== undefined || sourceTorrent?.contentType) && (
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
                {sourceTorrent?.contentType && (
                  <Badge variant="secondary" className="h-5 text-xs font-normal capitalize">
                    {t(`crossseed:dirScan.contentTypeLabels.${sourceTorrent.contentType}`, sourceTorrent.contentType)}
                  </Badge>
                )}
                {sourceTorrent?.category && <span>{t("crossSeedDialog.category", { category: sourceTorrent.category })}</span>}
                {sourceTorrent?.size !== undefined && <span>{t("crossSeedDialog.size", { size: formatBytes(sourceTorrent.size) })}</span>}
              </div>
            )}
          </DialogDescription>
          {cacheMetadata && (
            <div className="mt-2 space-y-2 rounded-lg border border-dashed border-border/70 p-2 text-xs text-muted-foreground">
              <div className="flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between">
                <span>
                  {cacheMetadata.hit ? t("crossSeedDialog.servedFromCache") : t("crossSeedDialog.freshSearch")} · {cacheMetadata.scope?.replace("_", " ") ?? "torznab"}
                </span>
                <span>
                  {t("crossSeedDialog.cached", { time: formatRelativeTime(cacheMetadata.cachedAt) })} · {t("crossSeedDialog.expires", { time: formatRelativeTime(cacheMetadata.expiresAt) })}
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
                    {t("crossSeedDialog.refreshFromIndexers")}
                  </Button>
                  {!canForceRefresh && refreshCooldownLabel && (
                    <span className="text-[11px] text-muted-foreground">{refreshCooldownLabel}</span>
                  )}
                </div>
              )}
            </div>
          )}
          {partial && (
            <div className="mt-2 flex items-center gap-1.5 text-xs text-yellow-500">
              <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
              <span>{t("crossSeedDialog.partialResults")}</span>
            </div>
          )}
          {queryDegraded && queryDegradedMessageKeys[queryDegraded] && (
            <div className="mt-2 flex items-center gap-1.5 text-xs text-yellow-500">
              <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
              <span>{t(queryDegradedMessageKeys[queryDegraded])}</span>
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
                <p className="font-medium">{t("crossSeedDialog.noIndexers")}</p>
                <p className="text-xs">
                  {t("crossSeedDialog.noIndexersHelp")}
                </p>
              </div>
            )}
            {(capabilityFilteredIndexerEntries.length > 0 && indexerOptions.length > 0) && (
              <div className="mt-2 rounded-md border border-dashed border-border/60 bg-muted/30 p-2 text-xs text-muted-foreground">
                <p className="font-medium text-[11px] text-foreground">{t("crossSeedDialog.capabilityNote")}</p>
                <p>
                  {t("crossSeedDialog.capabilityDescription")}
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
                        <span className="font-medium">{t("crossSeedDialog.contentFilteringInProgress")}</span>
                        <Badge variant="secondary" className="text-xs">
                          {t("crossSeedDialog.analyzingContent")}
                        </Badge>
                      </>
                    ) : (
                      <>
                        <span className="font-medium">{t("crossSeedDialog.smartFilteringActive")}</span>
                        <Badge variant="secondary" className="text-xs">
                          {t("crossSeedDialog.indexersFiltered", { count: excludedIndexerEntries.length, plural: excludedIndexerEntries.length === 1 ? "" : "s" })}
                        </Badge>
                      </>
                    )}
                  </div>
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <div className="px-2.5 pb-2.5">
                    {!sourceTorrent?.contentFilteringCompleted ? (
                      <p className="text-xs text-muted-foreground">
                        {t("crossSeedDialog.contentFilteringDescription")}
                      </p>
                    ) : excludedIndexerEntries.length > 0 ? (
                      <>
                        <p className="text-xs text-muted-foreground">
                          {t("crossSeedDialog.excludedDescription")}
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
                        {t("crossSeedDialog.noContentDuplicates")}
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
              <span>{t("crossSeedDialog.searchingIndexers")}</span>
            </div>
          ) : error ? (
            <div className="space-y-2 rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
              <p className="break-words text-xs">{error}</p>
              {(error.includes("rate limit") || error.includes("429") || error.includes("too many requests") ||
                error.includes("cooldown") || error.includes("rate-limited")) ? (
                  <div className="text-xs text-muted-foreground space-y-1">
                    <p><strong>{t("crossSeedDialog.rateLimitHelp.why")}</strong> {t("crossSeedDialog.rateLimitHelp.whyDescription")}</p>
                    <p><strong>{t("crossSeedDialog.rateLimitHelp.whatYouCanDo")}</strong></p>
                    <ul className="list-disc list-inside space-y-0.5 ml-2">
                      <li>{t("crossSeedDialog.rateLimitHelp.wait")}</li>
                      <li>{t("crossSeedDialog.rateLimitHelp.fewerIndexers")}</li>
                      <li>{t("crossSeedDialog.rateLimitHelp.useRss")}</li>
                      <li>{t("crossSeedDialog.rateLimitHelp.checkIndexers")}</li>
                    </ul>
                  </div>
                ) : null}
              <div className="flex gap-2">
                <Button size="sm" onClick={onRetry} className="h-7">
                  {t("crossSeedDialog.retry")}
                </Button>
                <Button size="sm" variant="outline" onClick={onClose} className="h-7">
                  {t("crossSeedDialog.close")}
                </Button>
              </div>
            </div>
          ) : (
            <>
              {results.length === 0 ? (
                <div className="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">
                  {t("crossSeedDialog.noMatchesFound")}
                </div>
              ) : (
                <>
                  <div className="flex items-center justify-between gap-2 text-xs">
                    <span className="truncate text-muted-foreground">{t("crossSeedDialog.selectReleasesToAdd")}</span>
                    <div className="flex shrink-0 items-center gap-2">
                      <Badge variant="outline" className="shrink-0 text-xs">
                        {selectionCount} / {results.length}
                      </Badge>
                      <Button variant="outline" size="sm" onClick={onSelectAll} className="h-7">
                        {t("crossSeedDialog.selectAll")}
                      </Button>
                      <Button variant="outline" size="sm" onClick={onClearSelection} className="h-7">
                        {t("crossSeedDialog.clear")}
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
                            aria-label={`${t("crossSeedDialog.select")} ${result.title}`}
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
                              <span className="shrink-0">{t("crossSeedDialog.seeders", { count: result.seeders })}</span>
                              {result.matchReason && <span className="min-w-0 truncate">{t("crossSeedDialog.match", { reason: result.matchReason })}</span>}
                              <span className="shrink-0">{formatCrossSeedPublishDate(result.publishDate)}</span>
                            </div>
                          </div>
                        </div>
                      )
                    })}
                  </div>
                  <div className="flex flex-col gap-2 rounded-md border p-2.5">
                    <div className="flex items-center justify-between gap-3">
                      <div className="flex items-center gap-2 shrink-0">
                        <Switch
                          id="cross-seed-tag-toggle"
                          checked={useTag}
                          onCheckedChange={(value) => onUseTagChange(Boolean(value))}
                        />
                        <label htmlFor="cross-seed-tag-toggle" className="text-sm whitespace-nowrap">
                          {t("crossSeedDialog.tagAddedTorrents")}
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
                    <div className="flex items-center gap-2">
                      <Switch
                        id="cross-seed-start-paused"
                        checked={startPaused}
                        onCheckedChange={(value) => onStartPausedChange(Boolean(value))}
                      />
                      <label htmlFor="cross-seed-start-paused" className="text-sm">
                        {t("crossSeedDialog.startPaused")}
                      </label>
                    </div>
                  </div>
                </>
              )}
              {applyResult && (
                <Collapsible open={applyResultOpen} onOpenChange={setApplyResultOpen}>
                  <div className="min-w-0 space-y-2 rounded-md border">
                    <CollapsibleTrigger className="w-full px-3 pt-2.5 pb-2 text-left hover:bg-muted/50 transition-colors">
                      <div className="flex items-center gap-2">
                        <ChevronRight className={`h-3.5 w-3.5 transition-transform ${applyResultOpen ? "rotate-90" : ""}`} />
                        <p className="text-sm font-medium">{t("crossSeedDialog.latestAddAttempt")}</p>
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
                                {result.success ? t("crossSeedDialog.status.queued") : t("crossSeedDialog.status.check")}
                              </Badge>
                            </div>
                            <p className="truncate text-xs text-muted-foreground" title={result.torrentName ?? result.title}>{result.torrentName ?? result.title}</p>
                            {result.error && <p className="break-words text-xs text-destructive">{result.error}</p>}
                            {result.instanceResults && result.instanceResults.length > 0 && (
                              <ul className="mt-1.5 space-y-1 text-xs">
                                {result.instanceResults.map(instance => {
                                  const infoHash = result.infoHash
                                  const pendingKey = infoHash ? getBlocklistPendingKey(instance.instanceId, infoHash) : null
                                  const isBlocking = pendingKey ? blocklistPendingKeys.has(pendingKey) : false
                                  const statusDisplay = getInstanceStatusDisplay(t, instance.status, instance.success)
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
                                        {infoHash && (
                                          <Button
                                            variant="ghost"
                                            size="sm"
                                            onClick={() => handleBlockInfoHash(instance.instanceId, infoHash!)}
                                            disabled={isBlocking}
                                            aria-label={`${t("crossSeedDialog.blockFor")} ${instance.instanceName}`}
                                            title={`${t("crossSeedDialog.block")} ${infoHash}`}
                                            className="h-5 px-2 text-[10px]"
                                          >
                                            {isBlocking ? t("crossSeedDialog.blocking") : t("crossSeedDialog.block")}
                                          </Button>
                                        )}
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
            {t("crossSeedDialog.close")}
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
                {t("crossSeedDialog.adding")}
              </>
            ) : (
              t("crossSeedDialog.addSelected")
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
function getInstanceStatusDisplay(
  t: ReturnType<typeof useTranslation<"torrents">>["t"],
  status: string,
  success: boolean
): { text: string; variant: "default" | "success" | "warning" | "destructive" } {
  switch (status) {
    case "added":
      return { text: t("crossSeedDialog.status.added"), variant: "success" }
    case "added_hardlink":
      return { text: t("crossSeedDialog.status.addedHardlink"), variant: "success" }
    case "added_reflink":
      return { text: t("crossSeedDialog.status.addedReflink"), variant: "success" }
    case "exists":
      return { text: t("crossSeedDialog.status.exists"), variant: "warning" }
    case "blocked":
      return { text: t("crossSeedDialog.status.blocked"), variant: "warning" }
    case "no_match":
      return { text: t("crossSeedDialog.status.noMatch"), variant: "destructive" }
    case "rejected":
      return { text: t("crossSeedDialog.status.rejected"), variant: "destructive" }
    case "no_save_path":
      return { text: t("crossSeedDialog.status.noSavePath"), variant: "destructive" }
    case "invalid_content_path":
      return { text: t("crossSeedDialog.status.invalidContentPath"), variant: "destructive" }
    case "skipped_recheck":
      return { text: t("crossSeedDialog.status.skippedRecheck"), variant: "destructive" }
    case "below_threshold":
      return { text: t("crossSeedDialog.status.belowThreshold"), variant: "destructive" }
    case "skipped_unsafe_pieces":
      return { text: t("crossSeedDialog.status.skippedUnsafePieces"), variant: "destructive" }
    case "requires_hardlink_reflink":
      return { text: t("crossSeedDialog.status.requiresHardlinkReflink"), variant: "destructive" }
    case "error":
      return { text: t("crossSeedDialog.status.error"), variant: "destructive" }
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

const CrossSeedScopeSelector = memo(function CrossSeedScopeSelector({
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
}: CrossSeedScopeSelectorProps) {
  const { t } = useTranslation("torrents")
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
      return t("crossSeedDialog.scope.enabledIndexers", { count: total, suffix })
    }
    if (selectedCount === 0) {
      return t("crossSeedDialog.scope.noneSelected")
    }
    return t("crossSeedDialog.scope.selectedOfTotal", { selected: selectedCount, total })
  }, [t, indexerMode, total, selectedCount])

  const searchText = useMemo(() => {
    const suffix = searchIndexerCount === 1 ? "indexer" : "indexers"
    return t("crossSeedDialog.scope.indexersForSearch", { count: searchIndexerCount, suffix })
  }, [t, searchIndexerCount])

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
          <h3 className="text-xs font-medium">{t("crossSeedDialog.searchScope")}</h3>
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
            {t("crossSeedDialog.scope.allIndexers")}
          </Button>
          <Button
            size="sm"
            variant={indexerMode === "custom" ? "secondary" : "ghost"}
            onClick={handleCustomIndexersClick}
            disabled={disableCustomSelection || isSearching}
            className="h-7 flex-1 sm:flex-initial text-xs"
          >
            {t("crossSeedDialog.scope.selectCustom")}
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
                  {selectedCount > 0 ? t("crossSeedDialog.scope.selectedCount", { count: selectedCount }) : t("crossSeedDialog.scope.selectIndexers")}
                  <ChevronDown className="ml-1.5 h-3 w-3" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent className="w-64" align="end">
                <DropdownMenuLabel className="text-xs">{t("crossSeedDialog.scope.availableIndexers")}</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {indexerItems}
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  onSelect={(event) => event.preventDefault()}
                  onClick={onSelectAllIndexers}
                  className="text-xs"
                >
                  {t("crossSeedDialog.scope.selectAllIndexers")}
                </DropdownMenuItem>
                <DropdownMenuItem
                  onSelect={(event) => event.preventDefault()}
                  onClick={onClearIndexerSelection}
                  className="text-xs"
                >
                  {t("crossSeedDialog.scope.clearSelection")}
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
                {t("crossSeedDialog.scope.filtering")}
              </>
            ) : isSearching ? (
              <>
                <Loader2 className="mr-1.5 h-3 w-3 animate-spin" />
                {t("crossSeedDialog.scope.searching")}
              </>
            ) : (
              t("crossSeedDialog.scope.search")
            )}
          </Button>
        </div>
      </div>
    </div>
  )
})
CrossSeedScopeSelector.displayName = "CrossSeedScopeSelector"
