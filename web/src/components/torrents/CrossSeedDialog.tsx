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
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Drawer, DrawerContent, DrawerDescription, DrawerHeader, DrawerTitle } from "@/components/ui/drawer"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { api } from "@/lib/api"
import { crossSeedRouteStatusKeys, crossSeedRouteText } from "@/lib/cross-seed-utils"
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import { cn, copyTextToClipboard, formatBytes } from "@/lib/utils"
import { SearchScopeEntries, type ScopeEntry } from "@/components/torrents/SearchScopeEntries"
import { useGazelleLookupText } from "@/hooks/useGazelleLookupText"
import { gazelleReportLines, gazelleStatusClass } from "@/lib/gazelle-lookup"
import type {
  CrossSeedApplyResponse,
  CrossSeedGazelleLookup,
  CrossSeedSearchDecisionTrace,
  CrossSeedSearchRejectedCandidate,
  CrossSeedTorrentSearchResponse,
  Torrent
} from "@/types"
import { AlertTriangle, ChevronDown, ChevronRight, Copy, ExternalLink, ListTree, Loader2, RefreshCw } from "lucide-react"
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useIsPhone } from "@/hooks/useMediaQuery"
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

export type CrossSeedApplySummary = {
  level: "success" | "warning" | "error" | "info"
  message: string
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
  applyOutcomes: Map<string, CrossSeedApplyResponse["results"][number]>
  applySummary?: CrossSeedApplySummary | null
  indexerOptions: CrossSeedIndexerOption[]
  alreadySeededIndexers: CrossSeedIndexerOption[]
  unsupportedIndexers: CrossSeedIndexerOption[]
  /** The RED/OPS sites for the scope list: the outcome of the last search while the pick is the same, else the plan. */
  gazelleEntries: CrossSeedGazelleLookup[]
  gazelleKeysMissing?: boolean
  /** The outcome of each RED/OPS site in the last search, for the report. */
  gazelleLookups?: CrossSeedGazelleLookup[]
  /** The pseudo indexer IDs of the RED/OPS entries that the user can pick. */
  selectableGazelleIds: number[]
  indexerMode: "all" | "custom"
  selectedIndexerIds: number[]
  indexerNameMap: Record<number, string>
  onToggleIndexer: (indexerId: number) => void
  onSetAllIndexers: (picked: boolean) => void
  outcomesStale: boolean
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
  torznabFailed?: boolean
  decisionTrace?: CrossSeedSearchDecisionTrace
  canForceRefresh?: boolean
  refreshCooldownUntil?: number
  onForceRefresh?: () => void
  /** The Cross-seed instance that receives the adds, when the instance of the torrent routes its cross-seeds. */
  crossSeedInstanceName?: string
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
  applyOutcomes,
  applySummary,
  indexerOptions,
  alreadySeededIndexers,
  unsupportedIndexers,
  gazelleEntries,
  gazelleKeysMissing = false,
  gazelleLookups: lastLookups,
  selectableGazelleIds,
  indexerMode,
  selectedIndexerIds,
  indexerNameMap,
  onToggleIndexer,
  onSetAllIndexers,
  outcomesStale,
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
  torznabFailed,
  decisionTrace,
  canForceRefresh,
  refreshCooldownUntil = 0,
  onForceRefresh,
  crossSeedInstanceName,
}: CrossSeedDialogProps) => {
  const { t } = useTranslation(["torrents", "settings", "crossseed"])
  const { formatISOTimestamp } = useDateTimeFormatters()
  const lookupText = useGazelleLookupText()
  const isMobile = useIsPhone()
  const toastPosition = isMobile ? "top-center" as const : undefined

  const [traceOpen, setTraceOpen] = useState(false)
  const [unsupportedOpen, setUnsupportedOpen] = useState(false)
  const [scopeOpen, setScopeOpen] = useState(false)
  const [openedAt, setOpenedAt] = useState(open)
  const traceRef = useRef<HTMLDivElement>(null)
  if (scopeOpen && (isLoading || isSubmitting)) {
    setScopeOpen(false)
  }
  if (open !== openedAt) {
    setOpenedAt(open)
    if (open) {
      setScopeOpen(false)
      setTraceOpen(false)
      setUnsupportedOpen(false)
    }
  }
  const [blocklistPendingKeys, setBlocklistPendingKeys] = useState<Set<string>>(() => new Set())

  const searchDone = hasSearched && !isLoading && !error
  const isPicked = useCallback(
    (id: number) => indexerMode === "all" || selectedIndexerIds.includes(id),
    [indexerMode, selectedIndexerIds]
  )

  const scopeEntries = useMemo((): ScopeEntry[] => {
    const outcomes = new Map((decisionTrace?.indexers ?? []).map(outcome => [outcome.indexerId, outcome]))
    const matches = new Map<number, number>()
    for (const result of results) {
      matches.set(result.indexerId, (matches.get(result.indexerId) ?? 0) + 1)
    }

    const torznab = indexerOptions.map((option): ScopeEntry => {
      const picked = isPicked(option.id)
      const entry: ScopeEntry = {
        key: `torznab-${option.id}`,
        name: option.name,
        status: picked ? "will_ask" : "not_asked",
        picked,
        onToggle: () => onToggleIndexer(option.id),
      }
      const outcome = searchDone && picked && !outcomesStale ? outcomes.get(option.id) : undefined
      if (!outcome) {
        return entry
      }
      const matchCount = matches.get(option.id) ?? 0
      if (matchCount > 0) {
        return { ...entry, status: "match", note: t("crossSeedDialog.scope.matchCount", { count: matchCount }), noteClass: "text-primary" }
      }
      if (outcome.status === "error" || outcome.status === "not_covered") {
        return { ...entry, status: outcome.status }
      }
      return { ...entry, status: "no_match" }
    })

    const gazelle = gazelleEntries.filter(entry => entry.reason !== "not_music").map((entry): ScopeEntry => {
      const base = { key: entry.site, name: entry.name, status: entry.status }
      if (!selectableGazelleIds.includes(entry.indexerId)) {
        return { ...base, picked: null, note: lookupText(entry) }
      }
      const picked = isPicked(entry.indexerId)
      const quiet = entry.reason === "not_selected" || entry.status === "no_match" || entry.status === "failed" || (entry.status === "will_ask" && entry.reason !== "key_rejected")
      const matchCount = matches.get(entry.indexerId) ?? 0
      return {
        ...base,
        status: picked ? entry.status : "not_asked",
        picked,
        note: quiet || !picked ? undefined : entry.status === "match" && matchCount > 0 ? t("crossSeedDialog.scope.matchCount", { count: matchCount }) : lookupText(entry),
        noteClass: gazelleStatusClass(entry.status),
        attention: picked && entry.reason === "key_rejected",
        onToggle: () => onToggleIndexer(entry.indexerId),
      }
    })

    const alreadySeeded = alreadySeededIndexers.map((option): ScopeEntry => ({
      key: `seeded-${option.id}`,
      name: option.name,
      status: "not_asked",
      picked: null,
      note: t("crossSeedDialog.scope.alreadySeeded"),
    }))

    const noGazelleKeys: ScopeEntry[] = gazelleKeysMissing ? [{ key: "gazelle-no-keys", name: t("crossSeedDialog.gazelle.noApiKeys"), status: "not_asked", picked: null, settingsLink: true, attention: true }] : []

    return [...torznab, ...gazelle.filter(entry => entry.picked !== null), ...gazelle.filter(entry => entry.picked === null), ...noGazelleKeys, ...alreadySeeded]
  }, [
    alreadySeededIndexers,
    decisionTrace?.indexers,
    gazelleEntries,
    gazelleKeysMissing,
    indexerOptions,
    isPicked,
    lookupText,
    onToggleIndexer,
    outcomesStale,
    results,
    searchDone,
    selectableGazelleIds,
    t,
  ])

  const pickedCount = scopeEntries.filter(entry => entry.picked).length
  const selectableCount = scopeEntries.filter(entry => entry.picked !== null).length
  const canSearch = selectableCount > 0
  const analyzing = !sourceTorrent && !hasSearched && !error
  const retry = Boolean(error) && (!hasSearched || !outcomesStale)
  const freshResults = searchDone && results.length > 0 && !outcomesStale
  const showSearch = canSearch && (!hasSearched || outcomesStale || Boolean(error))
  const searchAgain = searchDone && results.length === 0 && !outcomesStale && canSearch
  const pickedNames = scopeEntries.filter(entry => entry.picked).map(entry => entry.name)
  const attentionEntries = scopeEntries.filter(entry => entry.attention).map(entry => ({ ...entry, picked: null }))
  const rateLimited = Boolean(error && /rate limit|429|too many requests|cooldown|rate-limited/.test(error))
  const allAdded = applyOutcomes.size > 0 && selectionCount === 0
  const unsupportedNames = useMemo(
    () => [
      ...unsupportedIndexers.map(option => option.name),
      ...gazelleEntries.filter(entry => entry.reason === "not_music").map(entry => entry.name),
    ],
    [gazelleEntries, unsupportedIndexers]
  )

  const problems = useMemo(() => {
    const torznab = (decisionTrace?.indexers ?? [])
      .filter(outcome => outcome.status === "error" || outcome.status === "not_covered")
      .map(outcome => {
        const name = indexerNameMap[outcome.indexerId] ?? `Indexer ${outcome.indexerId}`
        const status = t(`crossSeedDialog.trace.status.${outcome.status === "not_covered" ? "notCovered" : "error"}`)
        return {
          key: `torznab-${outcome.indexerId}`,
          name,
          text: outcome.detail ? t("crossSeedDialog.problemDetail", { name, status, detail: outcome.detail }) : t("crossSeedDialog.problem", { name, status }),
        }
      })
    const gazelle = (lastLookups ?? [])
      .filter(entry => entry.status === "failed")
      .map(entry => ({ key: entry.site, name: entry.name, text: t("crossSeedDialog.problem", { name: entry.name, status: lookupText(entry) }) }))
    return [...torznab, ...gazelle].sort((a, b) => a.name.localeCompare(b.name))
  }, [decisionTrace?.indexers, indexerNameMap, lastLookups, lookupText, t])

  const traceReasonGroups = useMemo(
    () => (decisionTrace ? groupTraceReasons(decisionTrace) : []),
    [decisionTrace]
  )

  const gazelleLookups = useMemo(
    () => (searchDone ? lastLookups ?? [] : []),
    [searchDone, lastLookups]
  )
  const rejectedCount = decisionTrace ? Math.max(0, decisionTrace.totalResults - decisionTrace.finalMatches) : 0
  const traceStats = decisionTrace
    ? ([
      ["returned", decisionTrace.totalResults],
      ["release", decisionTrace.releaseFiltered],
      ["size", decisionTrace.sizeFiltered],
      ["late", decisionTrace.lateContentFiltered],
      ["duplicates", decisionTrace.duplicateFiltered],
      ["matches", decisionTrace.finalMatches],
    ] as const).filter(([key, value]) => value > 0 || key === "returned" || key === "matches")
    : []

  const handleCopyTraceReport = useCallback(async () => {
    const report = buildCrossSeedTraceReport(decisionTrace, sourceTorrent?.name ?? torrent?.name ?? "", indexerNameMap, gazelleLookups)
    try {
      await copyTextToClipboard(report)
      toast.success(t("crossSeedDialog.trace.copied"), { position: toastPosition })
    } catch {
      toast.error(t("crossSeedDialog.trace.copyFailed"), { position: toastPosition })
    }
  }, [decisionTrace, sourceTorrent?.name, torrent?.name, indexerNameMap, gazelleLookups, t, toastPosition])

  const handleBlockInfoHash = useCallback(async (instanceId: number, infoHash: string) => {
    if (instanceId <= 0) {
      toast.error(t("crossSeedDialog.missingInstance"), { position: toastPosition })
      return
    }
    const pendingKey = getBlocklistPendingKey(instanceId, infoHash)
    setBlocklistPendingKeys(prev => new Set(prev).add(pendingKey))
    try {
      await api.addCrossSeedBlocklist({ instanceId, infoHash })
      toast.success(t("crossSeedDialog.addedToBlocklist"), { position: toastPosition })
    } catch (error) {
      const message = error instanceof Error ? error.message : t("crossSeedDialog.addedToBlocklist")
      toast.error(message, { position: toastPosition })
    } finally {
      setBlocklistPendingKeys((prev) => {
        const next = new Set(prev)
        next.delete(pendingKey)
        return next
      })
    }
  }, [t, toastPosition])

  const renderApplyOutcome = (outcome: CrossSeedApplyResponse["results"][number]) => {
    const instances = outcome.instanceResults ?? []
    return (
      <div className="space-y-1 pt-0.5 text-xs">
        {outcome.error && <p className="break-words text-destructive">{outcome.error}</p>}
        {instances.length === 0 ? (
          <Badge variant={outcome.success ? "outline" : "destructive"}>
            {outcome.success ? t("crossSeedDialog.status.queued") : t("crossSeedDialog.status.check")}
          </Badge>
        ) : instances.map(instance => {
          const infoHash = outcome.infoHash
          const blockInstanceId = instance.crossSeedInstanceId ?? instance.instanceId
          const blockInstanceName = instance.crossSeedInstanceName ?? instance.instanceName
          const pendingKey = infoHash ? getBlocklistPendingKey(blockInstanceId, infoHash) : null
          const isBlocking = pendingKey ? blocklistPendingKeys.has(pendingKey) : false
          const statusDisplay = getInstanceStatusDisplay(t, instance.status, instance.success)
          return (
            <div key={`${instance.instanceId}-${instance.status}`} className="flex flex-wrap items-center gap-x-2 gap-y-1">
              <Badge variant={statusDisplay.variant === "success" ? "outline" : statusDisplay.variant === "warning" ? "secondary" : "destructive"}>
                {statusDisplay.text}
              </Badge>
              {(instances.length > 1 || instance.crossSeedInstanceName) && (
                <span className="font-medium">
                  {instance.crossSeedInstanceName
                    ? crossSeedRouteText(t, instance.instanceName, instance.crossSeedInstanceName, instance.success)
                    : instance.instanceName}
                </span>
              )}
              {instance.message && instance.message !== instance.status && (
                <span className={cn("min-w-0 break-words", instance.success ? "text-muted-foreground" : "text-destructive/80")}>
                  {instance.message}
                </span>
              )}
              {infoHash && (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => handleBlockInfoHash(blockInstanceId, infoHash)}
                  disabled={isBlocking}
                  aria-label={`${t("crossSeedDialog.blockFor")} ${blockInstanceName}`}
                  className="relative ml-auto h-8 px-3 text-xs after:absolute after:inset-x-0 after:-inset-y-1.5 sm:h-7 sm:px-2 sm:after:hidden sideways:h-8 sideways:px-3 sideways:after:block"
                >
                  {isBlocking ? t("crossSeedDialog.blocking") : t("crossSeedDialog.block")}
                </Button>
              )}
            </div>
          )
        })}
      </div>
    )
  }

  const sourceName = sourceTorrent?.name ?? torrent?.name
  const metaParts = [
    sourceTorrent?.contentType ? t([`common:contentTypeLabels.${sourceTorrent.contentType}`, "common:contentTypeLabels.unknown"]) : null,
    sourceTorrent?.category || null,
    sourceTorrent?.size !== undefined ? formatBytes(sourceTorrent.size) : null,
  ].filter(Boolean)
  const allSelected = results.length > 0 && selectionCount === results.length

  const Header = isMobile ? DrawerHeader : DialogHeader
  const Title = isMobile ? DrawerTitle : DialogTitle
  const Description = isMobile ? DrawerDescription : DialogDescription
  const focusContent = (event: Event) => {
    event.preventDefault()
    const content = event.currentTarget as HTMLElement
    content.focus()
  }

  const content = (
    <>
      <Header className={cn("min-w-0 shrink-0 gap-2 border-b px-5 pb-3 text-left sm:px-6 sideways:gap-1 sideways:pb-2 sideways:pl-[max(1.25rem,env(safe-area-inset-left))] sideways:pr-[max(1.25rem,env(safe-area-inset-right))]", isMobile ? "pt-3 sideways:pt-1" : "pt-5")}>
        <Title>{t("crossSeedDialog.title")}</Title>
        <Description asChild>
          <div className="min-w-0 space-y-0.5 text-xs">
            <p className="font-mono break-words text-foreground/80 sideways:truncate">
              {sourceName ?? t("crossSeedDialog.torrentLabel")}
            </p>
            {metaParts.length > 0 && <p className="sideways:hidden">{metaParts.join(" · ")}</p>}
            {crossSeedInstanceName && <p>{t("crossSeedDialog.routedTo", { instance: crossSeedInstanceName })}</p>}
          </div>
        </Description>
      </Header>

      <div className="min-w-0 flex-1 overflow-y-auto overflow-x-hidden px-5 sm:px-6 sideways:pl-[max(1.25rem,env(safe-area-inset-left))] sideways:pr-[max(1.25rem,env(safe-area-inset-right))]">
        <div className="space-y-4 pt-4 pb-5">
          {(sourceTorrent || analyzing) && (
            <section className="space-y-2">
              <div className="flex min-h-11 items-center justify-between gap-3 sm:min-h-7 sideways:min-h-11">
                <div className="flex min-w-0 items-baseline gap-2">
                  <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{t("crossSeedDialog.scope.heading")}</h3>
                  {!analyzing && canSearch && (
                    <span className="truncate text-xs text-muted-foreground">{t("crossSeedDialog.selectedOfTotal", { selected: pickedCount, total: selectableCount })}</span>
                  )}
                </div>
                {!analyzing && canSearch && !isLoading && !isSubmitting && (
                  <div className="-mr-2 flex shrink-0 items-center">
                    {scopeOpen && selectableCount > 1 && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => onSetAllIndexers(pickedCount < selectableCount)}
                        className="h-11 px-3 text-xs sm:h-7 sm:px-2 sideways:h-11 sideways:px-3"
                      >
                        {pickedCount < selectableCount ? t("crossSeedDialog.scope.selectAll") : t("crossSeedDialog.scope.selectNone")}
                      </Button>
                    )}
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      aria-expanded={scopeOpen}
                      onClick={() => setScopeOpen(!scopeOpen)}
                      className="h-11 px-3 text-xs sm:h-7 sm:px-2 sideways:h-11 sideways:px-3"
                    >
                      {scopeOpen ? t("crossSeedDialog.scope.done") : t("crossSeedDialog.scope.change")}
                      <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", scopeOpen && "rotate-180")} />
                    </Button>
                  </div>
                )}
              </div>
              {analyzing ? (
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  <span>{t("crossSeedDialog.scope.checking")}</span>
                </div>
              ) : canSearch && !scopeOpen ? (
                <div className="space-y-1.5">
                  <p className={cn("line-clamp-2 text-xs break-words text-muted-foreground", searchDone && "sideways:hidden")}>
                    {pickedNames.length > 0 ? pickedNames.join(", ") : t("crossSeedDialog.scope.nonePicked")}
                  </p>
                  {attentionEntries.length > 0 && <SearchScopeEntries entries={attentionEntries} disabled />}
                </div>
              ) : (
                <div className="space-y-2">
                  {!canSearch && (
                    <div className="space-y-1 rounded-md border border-dashed px-3 py-2.5 text-sm text-muted-foreground">
                      {scopeEntries.length > 0 || unsupportedNames.length > 0 ? (
                        <p className="font-medium text-foreground">{t("crossSeedDialog.scope.nothingLeft")}</p>
                      ) : (
                        <>
                          <p className="font-medium text-foreground">{t("crossSeedDialog.noIndexers")}</p>
                          <p className="text-xs">{t("crossSeedDialog.noIndexersHelp")}</p>
                        </>
                      )}
                    </div>
                  )}
                  <SearchScopeEntries entries={scopeEntries} disabled={isLoading} />
                  {unsupportedNames.length > 0 && (
                    <Collapsible open={unsupportedOpen} onOpenChange={setUnsupportedOpen}>
                      <CollapsibleTrigger className="flex min-h-11 items-center gap-1 text-xs text-muted-foreground hover:text-foreground sm:min-h-7 sideways:min-h-11">
                        <ChevronRight className={cn("h-3 w-3 transition-transform", unsupportedOpen && "rotate-90")} />
                        {t("crossSeedDialog.scope.unsupported", { count: unsupportedNames.length })}
                      </CollapsibleTrigger>
                      <CollapsibleContent>
                        <p className="pl-4 text-xs break-words text-muted-foreground">
                          {unsupportedNames.join(", ")}
                        </p>
                      </CollapsibleContent>
                    </Collapsible>
                  )}
                </div>
              )}
            </section>
          )}

          {isLoading ? (
            <div className="flex items-center justify-center gap-2.5 border-t py-8 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              <span>{t("crossSeedDialog.searchingIndexers")}</span>
            </div>
          ) : error ? (
            <div className="space-y-1 rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2.5 text-xs">
              <p className="break-words text-destructive">{error}</p>
              {rateLimited && <p className="text-muted-foreground">{t("crossSeedDialog.rateLimitHelp.wait")}</p>}
            </div>
          ) : searchDone ? (
            <section inert={outcomesStale} className={cn("space-y-3 border-t pt-4 transition-opacity", outcomesStale && "opacity-50")}>
              {((partial && problems.length === 0) || (queryDegraded && queryDegradedMessageKeys[queryDegraded]) || torznabFailed || problems.length > 0) && (
                <div className="space-y-1">
                  {partial && problems.length === 0 && <Warning>{t("crossSeedDialog.partialResults")}</Warning>}
                  {queryDegraded && queryDegradedMessageKeys[queryDegraded] && <Warning>{t(queryDegradedMessageKeys[queryDegraded])}</Warning>}
                  {torznabFailed && <Warning>{t("crossSeedDialog.torznabFailed")}</Warning>}
                  {problems.map(problem => <Warning key={problem.key}>{problem.text}</Warning>)}
                </div>
              )}
              <div className="space-y-0.5">
                <div className="flex min-h-11 items-center justify-between gap-3 sm:min-h-8 sideways:min-h-11">
                  {results.length > 0 ? (
                    <label className="flex min-h-11 cursor-pointer items-center gap-2.5 text-sm sm:min-h-0 sideways:min-h-11">
                      <Checkbox
                        checked={allSelected ? true : selectionCount > 0 ? "indeterminate" : false}
                        onCheckedChange={() => (allSelected ? onClearSelection() : onSelectAll())}
                        aria-label={t("crossSeedDialog.selectAll")}
                      />
                      <span className="whitespace-nowrap font-medium">{t("crossSeedDialog.resultsSelectedOfTotal", { selected: selectionCount, total: results.length })}</span>
                    </label>
                  ) : (
                    <p className="text-sm text-muted-foreground">{t("crossSeedDialog.noMatchesFound")}</p>
                  )}
                  {!outcomesStale && results.length > 0 && onForceRefresh && !isSubmitting && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      disabled={!canForceRefresh}
                      onClick={onForceRefresh}
                      className="-mr-2 h-11 shrink-0 px-3 text-xs text-muted-foreground sm:h-7 sm:px-2 sideways:h-11 sideways:px-3"
                    >
                      <RefreshCw className="h-3.5 w-3.5" />
                      {t("crossSeedDialog.refresh")}
                    </Button>
                  )}
                </div>
                {!outcomesStale && (cacheMetadata?.hit || refreshCooldownUntil > 0) && (
                  <p className="text-xs text-muted-foreground">
                    {cacheMetadata?.hit && t("crossSeedDialog.cachedAgo", { time: formatRelativeTime(cacheMetadata.cachedAt) })}
                    {cacheMetadata?.hit && refreshCooldownUntil > 0 && " · "}
                    {refreshCooldownUntil > 0 && <RefreshCountdown until={refreshCooldownUntil} />}
                  </p>
                )}
                {applySummary && !outcomesStale && (
                  <p
                    role="status"
                    className={cn(
                      "text-sm font-medium",
                      applySummary.level === "success" && "text-green-600 dark:text-green-500",
                      applySummary.level === "warning" && "text-yellow-600 dark:text-yellow-500",
                      applySummary.level === "error" && "text-destructive",
                      applySummary.level === "info" && "text-muted-foreground"
                    )}
                  >
                    {applySummary.message}
                  </p>
                )}
              </div>

              {results.length > 0 && (
                <>
                  <ul className="divide-y rounded-md border">
                    {results.map((result, index) => {
                      const key = getResultKey(result, index)
                      const checked = selectedKeys.has(key)
                      const outcome = applyOutcomes.get(key)
                      return (
                        <li
                          key={key}
                          className="flex cursor-pointer items-start gap-2.5 px-3 py-2.5 transition-colors hover:bg-accent/30"
                          onClick={(event) => {
                            if (!(event.target as HTMLElement).closest("a, button")) onToggleSelection(result, index)
                          }}
                        >
                          <Checkbox
                            checked={checked}
                            onCheckedChange={() => onToggleSelection(result, index)}
                            aria-label={`${t("crossSeedDialog.select")} ${result.title}`}
                            className="mt-0.5 shrink-0"
                          />
                          <div className="min-w-0 flex-1 space-y-1">
                            <p className="text-sm font-medium leading-snug break-words">{result.title}</p>
                            <div className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-0.5 text-xs text-muted-foreground">
                              <span className="shrink-0 font-medium text-foreground/80">{result.indexer}</span>
                              <span className="shrink-0">{formatBytes(result.size)}</span>
                              <span className="shrink-0">{t("crossSeedDialog.seeders", { count: result.seeders })}</span>
                              <span className="shrink-0">{formatISOTimestamp(result.publishDate)}</span>
                              {result.matchReason && <span className="min-w-0 break-words">{t("crossSeedDialog.match", { reason: result.matchReason })}</span>}
                            </div>
                            {outcome && renderApplyOutcome(outcome)}
                          </div>
                          {result.infoUrl && (
                            <a
                              href={result.infoUrl}
                              target="_blank"
                              rel="noopener noreferrer"
                              aria-label={t("crossSeedDialog.openOnIndexer", { indexer: result.indexer })}
                              className="-my-2 -mr-2 shrink-0 rounded-md p-3.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground sm:-my-1 sm:-mr-1.5 sm:p-2 sideways:-my-2 sideways:-mr-2 sideways:p-3.5"
                            >
                              <ExternalLink className="h-4 w-4" />
                            </a>
                          )}
                        </li>
                      )
                    })}
                  </ul>
                  <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                    <div className="flex min-h-11 items-center gap-3 rounded-md border px-3 py-1.5">
                      <Label htmlFor="cross-seed-tag-name" className="flex min-h-11 shrink-0 cursor-pointer items-center sm:min-h-0 sideways:min-h-11">
                        {t("crossSeedDialog.tag")}
                      </Label>
                      <Input
                        id="cross-seed-tag-name"
                        value={tagName}
                        onChange={(event) => onTagNameChange(event.target.value)}
                        placeholder="cross-seed"
                        disabled={!useTag}
                        autoCapitalize="off"
                        autoCorrect="off"
                        spellCheck={false}
                        enterKeyHint="done"
                        className="h-9 min-w-0 flex-1 sm:h-8 sideways:h-9"
                      />
                      <label className="-my-1.5 -mr-3 flex min-h-11 shrink-0 cursor-pointer items-center pr-3 pl-1">
                        <Switch
                          id="cross-seed-use-tag"
                          checked={useTag}
                          onCheckedChange={(value) => onUseTagChange(Boolean(value))}
                          aria-label={t("crossSeedDialog.tagAddedTorrents")}
                        />
                      </label>
                    </div>
                    <label htmlFor="cross-seed-start-paused" className="flex min-h-11 cursor-pointer items-center justify-between gap-3 rounded-md border px-3 py-1.5 text-sm font-medium">
                      {t("crossSeedDialog.startPaused")}
                      <Switch
                        id="cross-seed-start-paused"
                        checked={startPaused}
                        onCheckedChange={(value) => onStartPausedChange(Boolean(value))}
                      />
                    </label>
                  </div>
                </>
              )}

              {decisionTrace && decisionTrace.totalResults > 0 ? (
                <Collapsible
                  ref={traceRef}
                  open={traceOpen}
                  onOpenChange={(next) => {
                    setTraceOpen(next)
                    if (next) {
                      requestAnimationFrame(() => traceRef.current?.scrollIntoView({ behavior: "smooth", block: "start" }))
                    }
                  }}
                >
                  <CollapsibleTrigger asChild>
                    <button
                      type="button"
                      className="flex w-full items-center gap-2 rounded-md border px-3 py-2.5 text-left text-sm transition-colors hover:bg-accent/40"
                    >
                      <ListTree className="h-4 w-4 shrink-0 text-muted-foreground" />
                      <span className="flex min-w-0 flex-1 flex-col">
                        <span className="font-medium">{t("crossSeedDialog.trace.title")}</span>
                        {rejectedCount > 0 && (
                          <span className="truncate text-xs text-muted-foreground">
                            {t("crossSeedDialog.trace.rejectedCount", { count: rejectedCount })}
                          </span>
                        )}
                      </span>
                      <ChevronDown className={cn("h-4 w-4 shrink-0 text-muted-foreground transition-transform", traceOpen && "rotate-180")} />
                    </button>
                  </CollapsibleTrigger>
                  <CollapsibleContent>
                    <div className="space-y-3 px-1 pt-3 text-xs">
                      <dl className="grid grid-cols-[auto_auto] justify-start gap-x-6 gap-y-0.5">
                        {traceStats.map(([key, value]) => (
                          <div key={key} className="contents">
                            <dt className="text-muted-foreground">{t(`crossSeedDialog.trace.stats.${key}`)}</dt>
                            <dd className="tabular-nums">{value}</dd>
                          </div>
                        ))}
                      </dl>
                      {traceReasonGroups.length > 0 && (
                        <div className="space-y-1.5">
                          <p className="font-medium text-foreground">{t("crossSeedDialog.trace.rejectionsHeading")}</p>
                          {traceReasonGroups.map(group => (
                            <div key={group.reason} className="space-y-0.5">
                              <p className="text-muted-foreground">{t("crossSeedDialog.trace.reasonCount", { reason: group.reason, count: group.count })}</p>
                              <ul className="ml-3 space-y-0.5">
                                {group.candidates.map((candidate, index) => (
                                  <li key={`${group.reason}-${index}`} className="min-w-0 break-words">
                                    {candidate.title}
                                    <span className="text-muted-foreground">{` · ${candidate.indexer} · ${formatBytes(candidate.size)}`}</span>
                                  </li>
                                ))}
                                {group.count > group.candidates.length && (
                                  <li className="text-muted-foreground">
                                    {t("crossSeedDialog.trace.moreCandidates", { count: group.count - group.candidates.length })}
                                  </li>
                                )}
                              </ul>
                            </div>
                          ))}
                        </div>
                      )}
                      <Button type="button" variant="outline" size="sm" onClick={handleCopyTraceReport} className="h-11 sm:h-8 sideways:h-11">
                        <Copy className="h-3.5 w-3.5" />
                        {t("crossSeedDialog.trace.copyReport")}
                      </Button>
                    </div>
                  </CollapsibleContent>
                </Collapsible>
              ) : (decisionTrace || gazelleLookups.length > 0) && (
                <Button type="button" variant="ghost" size="sm" onClick={handleCopyTraceReport} className="-ml-2 h-11 px-2 text-xs text-muted-foreground sm:h-8 sideways:h-11">
                  <Copy className="h-3.5 w-3.5" />
                  {t("crossSeedDialog.trace.copyReport")}
                </Button>
              )}
            </section>
          ) : null}
        </div>
      </div>

      <div className="shrink-0 border-t bg-background px-5 pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] sm:px-6 sideways:pt-2 sideways:pb-[max(0.5rem,env(safe-area-inset-bottom))] sideways:pl-[max(1.25rem,env(safe-area-inset-left))] sideways:pr-[max(1.25rem,env(safe-area-inset-right))]">
        <div className="flex gap-2 sm:justify-end">
          <Button
            variant="outline"
            onClick={onClose}
            disabled={isSubmitting}
            className={cn("h-11 px-5 sm:h-10 sideways:h-11", !isLoading && !retry && !(freshResults && !allAdded) && !showSearch && !searchAgain && "flex-1 sm:flex-none")}
          >
            {t("crossSeedDialog.close")}
          </Button>
          {isLoading ? (
            <Button disabled className="h-11 flex-1 sm:h-10 sm:min-w-36 sm:flex-none sideways:h-11">
              <Loader2 className="h-4 w-4 animate-spin" />
              {t("crossSeedDialog.scope.searching")}
            </Button>
          ) : retry ? (
            <Button
              variant={rateLimited ? "outline" : "default"}
              onClick={onRetry}
              disabled={sourceTorrent !== undefined && selectableCount > 0 && pickedCount === 0}
              className="h-11 flex-1 sm:h-10 sm:min-w-36 sm:flex-none sideways:h-11"
            >
              {t("crossSeedDialog.retry")}
            </Button>
          ) : freshResults && !allAdded ? (
            <Button
              onClick={onApply}
              disabled={isSubmitting || selectionCount === 0}
              className="h-11 flex-1 sm:h-10 sm:min-w-36 sm:flex-none sideways:h-11"
            >
              {isSubmitting && <Loader2 className="h-4 w-4 animate-spin" />}
              {isSubmitting ? t("crossSeedDialog.adding") : selectionCount > 0 ? t("crossSeedDialog.addCount", { count: selectionCount }) : t("crossSeedDialog.add")}
            </Button>
          ) : searchAgain ? (
            <Button
              onClick={onForceRefresh}
              disabled={!onForceRefresh}
              className="h-11 flex-1 sm:h-10 sm:min-w-36 sm:flex-none sideways:h-11"
            >
              <RefreshCw className="h-4 w-4" />
              {t("crossSeedDialog.scope.searchAgain")}
            </Button>
          ) : showSearch ? (
            <Button
              onClick={onScopeSearch}
              disabled={analyzing || isSubmitting || pickedCount === 0}
              className="h-11 flex-1 sm:h-10 sm:min-w-36 sm:flex-none sideways:h-11"
            >
              {analyzing || pickedCount === 0 ? t("crossSeedDialog.scope.search") : t("crossSeedDialog.scope.searchCount", { count: pickedCount })}
            </Button>
          ) : null}
        </div>
      </div>
    </>
  )

  if (isMobile) {
    return (
      <Drawer open={open} onOpenChange={onOpenChange} dismissible={!isSubmitting}>
        <DrawerContent className="max-h-[94dvh] outline-none" onOpenAutoFocus={focusContent}>
          {content}
        </DrawerContent>
      </Drawer>
    )
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !isSubmitting && onOpenChange(next)}>
      <DialogContent
        className="flex max-h-[85dvh] w-full max-w-[95vw] flex-col gap-0 p-0 !translate-y-0 !top-[7.5dvh] sm:max-w-2xl"
        onOpenAutoFocus={focusContent}
      >
        {content}
      </DialogContent>
    </Dialog>
  )
}

export const CrossSeedDialog = memo(CrossSeedDialogComponent)
CrossSeedDialog.displayName = "CrossSeedDialog"

// Plain-text report for sharing in bug reports; deliberately not localized.
export function buildCrossSeedTraceReport(
  trace: CrossSeedSearchDecisionTrace | undefined,
  sourceName: string,
  indexerNameMap: Record<number, string>,
  gazelleLookups: CrossSeedGazelleLookup[] = []
): string {
  const lines: string[] = []
  lines.push("qui cross-seed search report")
  if (!trace) {
    lines.push(`source: ${sourceName}`)
    lines.push(...gazelleReportLines(gazelleLookups))
    return lines.join("\n")
  }
  lines.push(`source: ${sourceName} (${formatBytes(trace.sourceSize)})`)
  lines.push(`size tolerance: ${trace.tolerancePercent}%`)
  lines.push(
    `results: ${trace.totalResults} candidates, ${trace.releaseFiltered} release-filtered, ${trace.sizeFiltered} size-filtered, ${trace.lateContentFiltered} late-content-filtered, ${trace.duplicateFiltered} duplicates, ${trace.finalMatches} matches`
  )
  if (trace.indexers?.length) {
    lines.push("indexers:")
    for (const outcome of trace.indexers) {
      const name = indexerNameMap[outcome.indexerId] ?? `indexer ${outcome.indexerId}`
      const candidates = outcome.candidates > 0 ? `, ${outcome.candidates} candidates` : ""
      const detail = outcome.detail ? ` - ${outcome.detail}` : ""
      lines.push(`  ${name}: ${outcome.status}${candidates}${detail}`)
    }
  }
  lines.push(...gazelleReportLines(gazelleLookups))
  const groups = groupTraceReasons(trace)
  if (groups.length > 0) {
    lines.push("rejections:")
    for (const group of groups) {
      lines.push(`  ${group.reason}: ${group.count}`)
      for (const candidate of group.candidates) {
        lines.push(`    - ${candidate.title} [${candidate.indexer}] ${formatBytes(candidate.size)}`)
      }
      if (group.count > group.candidates.length) {
        lines.push(`    (+${group.count - group.candidates.length} more)`)
      }
    }
  }
  return lines.join("\n")
}

function groupTraceReasons(trace: CrossSeedSearchDecisionTrace): Array<{
  reason: string
  count: number
  candidates: CrossSeedSearchRejectedCandidate[]
}> {
  return Object.entries(trace.rejectionCounts ?? {})
    .sort((a, b) => b[1] - a[1])
    .map(([reason, count]) => ({
      reason,
      count,
      candidates: trace.rejectedCandidates?.filter(candidate => candidate.reason === reason) ?? [],
    }))
}

// Maps instance status codes to user-friendly display information
function getInstanceStatusDisplay(
  t: ReturnType<typeof useTranslation<"torrents">>["t"],
  status: string,
  success: boolean
): { text: string; variant: "default" | "success" | "warning" | "destructive" } {
  const routeStatusKey = crossSeedRouteStatusKeys[status]
  if (routeStatusKey) {
    return { text: t(routeStatusKey), variant: "destructive" }
  }
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
    case "size_mismatch":
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

function RefreshCountdown({ until }: { until: number }) {
  const { t } = useTranslation("crossseed")
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [])
  return t("crossseed:hooks.search.refreshReadyIn", { seconds: Math.max(1, Math.ceil((until - now) / 1000)) })
}

function Warning({ children }: { children: string }) {
  return (
    <div className="flex items-center gap-1.5 text-xs text-yellow-600 dark:text-yellow-500">
      <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
      <span>{children}</span>
    </div>
  )
}
