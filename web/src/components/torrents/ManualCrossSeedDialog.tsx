/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useDebounce } from "@/hooks/useDebounce"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { api } from "@/lib/api"
import { fileToBase64, overlapPercent } from "@/lib/manual-cross-seed"
import { formatBytes } from "@/lib/utils"
import type { ManualCrossSeedProposal } from "@/types"
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, FileUp } from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

interface ManualCrossSeedDialogProps {
  instanceId: number
  open: boolean
  onOpenChange: (open: boolean) => void
  initialFile?: File | null
  preselectedTarget?: { hash: string; name: string } | null
  onApplied?: () => void
}

export function ManualCrossSeedDialog({
  instanceId,
  open,
  onOpenChange,
  initialFile = null,
  preselectedTarget = null,
  onApplied,
}: ManualCrossSeedDialogProps) {
  const { t } = useTranslation("torrents")
  const queryClient = useQueryClient()
  const { data: metadata } = useInstanceMetadata(instanceId)

  const [file, setFile] = useState<File | null>(null)
  const [torrentData, setTorrentData] = useState<string | null>(null)
  const [selectedHashes, setSelectedHashes] = useState<string[] | null>(null)
  const [pickedTargets, setPickedTargets] = useState<{ hash: string; name: string }[]>([])
  const [pinnedHash, setPinnedHash] = useState<string | null>(null)
  // null = not edited for the current selection; prefill wins until then.
  const [categoryEdit, setCategoryEdit] = useState<string | null>(null)
  const [tagsEdit, setTagsEdit] = useState<string[] | null>(null)
  const [newTag, setNewTag] = useState("")
  const [pickerSearch, setPickerSearch] = useState("")
  const [showPicker, setShowPicker] = useState(false)

  const activeFile = file ?? initialFile

  // FileReader is an external system: derive the base64 payload when the file changes.
  useEffect(() => {
    if (!open || !activeFile) {
      setTorrentData(null)
      return
    }
    let stale = false
    fileToBase64(activeFile).then(data => {
      if (!stale) setTorrentData(data)
    }).catch(() => {
      if (!stale) setTorrentData(null)
    })
    return () => {
      stale = true
    }
  }, [open, activeFile])

  const requestedHash = selectedHashes?.length === 1 ? selectedHashes[0] : pinnedHash ?? preselectedTarget?.hash ?? undefined
  const fileKey = activeFile ? `${activeFile.name}:${activeFile.size}:${activeFile.lastModified}` : ""
  const proposalsQuery = useQuery({
    queryKey: ["cross-seed-manual-proposals", instanceId, fileKey, requestedHash],
    queryFn: () => api.getManualCrossSeedProposals({
      instanceId,
      torrentData: torrentData ?? "",
      targetHash: requestedHash,
    }),
    enabled: open && Boolean(torrentData),
    staleTime: 60_000,
    retry: false,
    // Keep proposals visible while a picker pin refetches, but blank on a
    // file swap so the list never shows another torrent's proposals.
    placeholderData: (previousData, previousQuery) =>
      previousQuery?.queryKey[2] === fileKey ? previousData : undefined,
  })
  const proposals = useMemo(
    () => proposalsQuery.data?.proposals ?? [],
    [proposalsQuery.data]
  )

  const source = proposalsQuery.data
  const packMode = source?.packMode ?? false
  const assemblyUnavailable = source?.assemblyUnavailableReason ?? ""
  const suggestQuery = useQuery({
    queryKey: ["cross-seed-manual-suggest", instanceId, fileKey],
    queryFn: ({ signal }) => api.checkManualAssemble({ instanceId, torrentData: torrentData ?? "", targetHashes: [] }, signal),
    enabled: open && Boolean(torrentData) && packMode,
    retry: false,
  })
  const effectiveSelectedHashes = selectedHashes ?? (preselectedTarget
    ? [preselectedTarget.hash]
    : packMode && !assemblyUnavailable
      ? (suggestQuery.data?.targets ?? []).filter(target => !target.reason).map(target => target.hash)
      : proposals[0] ? [proposals[0].hash] : [])
  const effectiveSelectedHash = effectiveSelectedHashes[0]
  const selectedProposal: ManualCrossSeedProposal | undefined = proposals.find(
    proposal => proposal.hash.toLowerCase() === effectiveSelectedHash?.toLowerCase()
  )
  const assembling = packMode && effectiveSelectedHashes.length > 1
  const checkQuery = useQuery({
    queryKey: ["cross-seed-manual-check", instanceId, fileKey, [...effectiveSelectedHashes].sort()],
    queryFn: ({ signal }) => api.checkManualAssemble({
      instanceId, torrentData: torrentData ?? "", targetHashes: effectiveSelectedHashes,
    }, signal),
    enabled: open && Boolean(torrentData) && assembling && !assemblyUnavailable,
    retry: false,
  })
  const preview = checkQuery.data
  const targetRows = [...proposals]
  for (const target of [...(suggestQuery.data?.targets ?? []), ...pickedTargets]) {
    if (!targetRows.some(row => row.hash.toLowerCase() === target.hash.toLowerCase())) {
      targetRows.push({ ...target, size: 0, category: "", effectiveSavePath: "", overlapBytes: 0, overlapFraction: 0 })
    }
  }

  const pinnedCategory = assembling ? "" : (source?.pinnedCategory ?? "")
  const categoryValue = pinnedCategory || (categoryEdit ?? (assembling
    ? preview?.defaultCategory ?? suggestQuery.data?.defaultCategory ?? ""
    : selectedProposal?.category ?? ""))
  const selectedTags = tagsEdit ?? proposalsQuery.data?.defaultTags ?? []
  const toggleTag = (tag: string) => {
    setTagsEdit(selectedTags.includes(tag) ? selectedTags.filter(item => item !== tag) : [...selectedTags, tag])
  }

  const debouncedPickerSearch = useDebounce(pickerSearch, 300)
  const pickerQuery = useQuery({
    queryKey: ["cross-seed-manual-picker", instanceId, debouncedPickerSearch],
    queryFn: ({ signal }) => api.getTorrents(instanceId, { search: debouncedPickerSearch, limit: 20, preferCached: true }, signal),
    enabled: open && showPicker && debouncedPickerSearch.trim().length > 0,
    staleTime: 30_000,
    // Keep the old list while the next term loads so the picker never
    // flickers empty between searches.
    placeholderData: keepPreviousData,
  })

  const applyMutation = useMutation({
    mutationFn: async () => {
      if (assembling) {
        const assembly = await api.applyManualAssemble({
          instanceId, torrentData: torrentData ?? "", targetHashes: effectiveSelectedHashes,
          category: categoryValue, tags: selectedTags,
        })
        return { success: assembly.applied, results: [], assembly }
      }
      const result = await api.applyManualCrossSeed({
        instanceId,
        torrentData: torrentData ?? "",
        targetHash: effectiveSelectedHash ?? "",
        category: categoryValue || undefined,
        tags: selectedTags,
      })
      return { ...result, assembly: undefined }
    },
    onSuccess: response => {
      const firstResult = response.results[0]
      if (response.success) {
        const dropped = response.assembly?.targets.filter(target => target.reason) ?? []
        if (dropped.length > 0) {
          toast.warning(t("manualCrossSeed.pack.dropped", { names: dropped.map(target => target.name || target.hash).join(", ") }), { duration: 15_000 })
        } else {
          toast.success(t("manualCrossSeed.applySuccess"))
        }
        // Same post-add delay as AddTorrentDialog: give qBittorrent a beat to
        // register the torrent before the list refetch.
        setTimeout(() => {
          void queryClient.invalidateQueries({ queryKey: ["torrents-list", instanceId] })
        }, 500)
        handleOpenChange(false)
        onApplied?.()
      } else {
        toast.error(t("manualCrossSeed.applyFailed", { message: response.assembly ? assemblyReason(response.assembly.reason) : firstResult?.message ?? firstResult?.status ?? "" }))
      }
    },
    onError: error => {
      toast.error(t("manualCrossSeed.applyFailed", { message: error instanceof Error ? error.message : "" }))
    },
  })

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) {
      setFile(null)
      setTorrentData(null)
      setSelectedHashes(null)
      setPickedTargets([])
      setPinnedHash(null)
      setCategoryEdit(null)
      setTagsEdit(null)
      setNewTag("")
      setPickerSearch("")
      setShowPicker(false)
    }
    onOpenChange(nextOpen)
  }

  const handleSelectTarget = (hash: string) => {
    if (packMode && !assemblyUnavailable) {
      setSelectedHashes(effectiveSelectedHashes.some(selected => selected.toLowerCase() === hash.toLowerCase())
        ? effectiveSelectedHashes.filter(selected => selected.toLowerCase() !== hash.toLowerCase())
        : [...effectiveSelectedHashes, hash])
    } else {
      setSelectedHashes([hash])
      setCategoryEdit(null)
    }
    setShowPicker(false)
  }

  const handlePickFromSearch = (hash: string, name: string) => {
    setPickedTargets(current => current.some(target => target.hash === hash) ? current : [...current, { hash, name }])
    setPinnedHash(hash)
    handleSelectTarget(hash)
  }

  // Only complete torrents can serve as Manual match targets; the apply rejects
  // incomplete ones, so the picker does not offer them.
  const completePickerTorrents = (pickerQuery.data?.torrents ?? []).filter(torrent => torrent.progress >= 1)

  const zeroOverlap = selectedProposal !== undefined && selectedProposal.overlapBytes === 0
  const categoryNames = Object.keys(metadata?.categories ?? {}).sort()
  const availableTags = Array.from(new Set([
    ...(metadata?.tags ?? []),
    ...(proposalsQuery.data?.defaultTags ?? []),
    ...selectedTags,
  ])).sort()
  const assemblyReason = (reason: string) => t(`manualCrossSeed.pack.reasons.${reason}`, { defaultValue: t("manualCrossSeed.pack.checkFailed") })

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      {/* Top-pinned with dvh so the footer stays reachable when the iOS
          keyboard opens (same mitigation as AddTorrentDialog). */}
      <DialogContent className="sm:max-w-2xl max-h-[90dvh] sm:max-h-[85dvh] flex flex-col !translate-y-0 !top-[5vh] sm:!top-[7.5vh]">
        <DialogHeader>
          <DialogTitle>{t("manualCrossSeed.title")}</DialogTitle>
          <DialogDescription>{t(packMode ? "manualCrossSeed.pack.description" : "manualCrossSeed.description")}</DialogDescription>
        </DialogHeader>

        <div className="flex-1 min-h-0 overflow-y-auto space-y-4 pr-1">
          {!activeFile && (
            <div className="space-y-2">
              <Label htmlFor="manual-cross-seed-file">{t("manualCrossSeed.fileLabel")}</Label>
              <Input
                id="manual-cross-seed-file"
                type="file"
                accept=".torrent,application/x-bittorrent"
                onChange={event => {
                  const selected = event.target.files?.[0] ?? null
                  setFile(selected)
                  setSelectedHashes(null)
                  setPickedTargets([])
                  setPinnedHash(null)
                  setCategoryEdit(null)
                  setTagsEdit(null)
                  setNewTag("")
                }}
              />
            </div>
          )}

          {activeFile && source && (
            <div className="flex items-start gap-2 rounded-md border p-3 text-sm">
              <FileUp className="h-4 w-4 mt-0.5 shrink-0 text-muted-foreground" />
              <div className="min-w-0">
                <p className="truncate font-medium">{source.sourceName}</p>
                <p className="text-muted-foreground">
                  {t("manualCrossSeed.sourceSummary", {
                    size: formatBytes(source.sourceSize),
                    count: source.sourceFileCount,
                  })}
                </p>
              </div>
            </div>
          )}

          {activeFile && proposalsQuery.isLoading && (
            <p className="text-sm text-muted-foreground">{t("manualCrossSeed.loadingProposals")}</p>
          )}
          {activeFile && proposalsQuery.isError && (
            <p className="text-sm text-destructive">
              {proposalsQuery.error instanceof Error ? proposalsQuery.error.message : t("manualCrossSeed.proposalsFailed")}
            </p>
          )}

          {activeFile && source && (
            <div className="space-y-2">
              <Label>{t(packMode ? "manualCrossSeed.pack.targetLabel" : "manualCrossSeed.targetLabel")}</Label>
              {packMode && assemblyUnavailable && (
                <p className="text-sm text-muted-foreground">{assemblyReason(assemblyUnavailable)}</p>
              )}
              {packMode && source.proposalsTruncated && (
                <p className="text-sm text-muted-foreground">{t("manualCrossSeed.pack.proposalLimit", { count: source.packEpisodeCount, limit: source.proposalLimit })}</p>
              )}
              {packMode && suggestQuery.isFetching && (
                <p className="text-sm text-muted-foreground">{t("manualCrossSeed.pack.checking")}</p>
              )}
              {packMode && suggestQuery.isError && (
                <p className="text-sm text-destructive">{t("manualCrossSeed.pack.checkFailed")}</p>
              )}
              {proposals.length === 0 && (
                <p className="text-sm text-muted-foreground">{t("manualCrossSeed.noProposals")}</p>
              )}
              {targetRows.length > 0 && (
                <div className="space-y-1 rounded-md border p-1 sm:max-h-48 sm:overflow-y-auto">
                  {targetRows.map(proposal => {
                    const isSelected = effectiveSelectedHashes.some(hash => proposal.hash.toLowerCase() === hash.toLowerCase())
                    return (
                      <div key={proposal.hash} className="flex items-center gap-2 px-2">
                        {packMode && (
                          <input
                            type="checkbox"
                            aria-label={proposal.name}
                            checked={isSelected}
                            disabled={Boolean(assemblyUnavailable) || applyMutation.isPending}
                            onChange={() => handleSelectTarget(proposal.hash)}
                            className="h-4 w-4 shrink-0 accent-primary"
                          />
                        )}
                        <button
                          type="button"
                          aria-pressed={isSelected}
                          disabled={applyMutation.isPending}
                          onClick={() => handleSelectTarget(proposal.hash)}
                          className={`w-full rounded-sm px-2 py-1.5 text-left text-sm transition-colors ${
                            isSelected ? "bg-accent text-accent-foreground" : "hover:bg-muted"
                          }`}
                        >
                          <span className="block truncate">{proposal.name}</span>
                          {proposal.size > 0 && <span className="block text-xs text-muted-foreground">
                            {formatBytes(proposal.size)} · {t("manualCrossSeed.overlap", { percent: overlapPercent(proposal.overlapFraction) })}
                          </span>}
                        </button>
                      </div>
                    )
                  })}
                </div>
              )}

              {!showPicker ? (
                <Button type="button" variant="outline" size="sm" onClick={() => setShowPicker(true)}>
                  {t("manualCrossSeed.pickManually")}
                </Button>
              ) : (
                <div className="space-y-1">
                  <Input
                    type="search"
                    enterKeyHint="search"
                    value={pickerSearch}
                    onChange={event => setPickerSearch(event.target.value)}
                    placeholder={t("manualCrossSeed.pickerPlaceholder")}
                  />
                  {pickerSearch.trim() !== "" && pickerQuery.data && (
                    <div className="rounded-md border p-1 sm:max-h-40 sm:overflow-y-auto">
                      {completePickerTorrents.length === 0 && (
                        <p className="px-2 py-1.5 text-sm text-muted-foreground">{t("manualCrossSeed.pickerNoResults")}</p>
                      )}
                      {completePickerTorrents.map(torrent => (
                        <button
                          key={torrent.hash}
                          type="button"
                          onClick={() => handlePickFromSearch(torrent.hash, torrent.name)}
                          className="w-full rounded-sm px-2 py-1.5 text-left text-sm hover:bg-muted"
                        >
                          <span className="block truncate">{torrent.name}</span>
                          <span className="block text-xs text-muted-foreground">{formatBytes(torrent.size)}</span>
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </div>
          )}

          {zeroOverlap && !assembling && (
            <div className="flex items-start gap-2 rounded-md border border-yellow-500/50 bg-yellow-500/10 p-3 text-sm">
              <AlertTriangle className="h-4 w-4 mt-0.5 shrink-0 text-yellow-600 dark:text-yellow-500" />
              <p>{t("manualCrossSeed.zeroOverlapWarning")}</p>
            </div>
          )}

          {assembling && (
            <div className="space-y-2 rounded-md border p-3 text-sm" aria-live="polite">
              {checkQuery.isFetching && <p>{t("manualCrossSeed.pack.checking")}</p>}
              {checkQuery.isError && <p className="text-destructive">{t("manualCrossSeed.pack.checkFailed")}</p>}
              {preview && !checkQuery.isFetching && (
                <>
                  <p>{t("manualCrossSeed.pack.coverage", { matched: preview.matchedEpisodes, total: preview.totalEpisodes, percent: overlapPercent(preview.coverage) })}</p>
                  <p>{t("manualCrossSeed.pack.missing", { size: formatBytes(preview.missingBytes) })}</p>
                  <p className="text-muted-foreground">{t("manualCrossSeed.pack.recheck")}</p>
                  {preview.reason && <p className="text-destructive">{assemblyReason(preview.reason)}</p>}
                  {preview.targets.filter(target => target.reason).map(target => (
                    <p key={target.hash} className="text-destructive">{t("manualCrossSeed.pack.rejected", { name: target.name || target.hash, reason: assemblyReason(target.reason) })}</p>
                  ))}
                </>
              )}
            </div>
          )}

          {(selectedProposal || assembling) && (
            <div className="space-y-3">
              <div className="space-y-2">
                <Label>{t("manualCrossSeed.category")}</Label>
                <Select
                  value={categoryValue === "" ? "__none__" : categoryValue}
                  onValueChange={value => setCategoryEdit(value === "__none__" ? "" : value)}
                  disabled={pinnedCategory !== ""}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {(assembling || (selectedProposal?.category ?? "") === "") && (
                      <SelectItem value="__none__">{t("manualCrossSeed.noCategory")}</SelectItem>
                    )}
                    {categoryNames.map(name => (
                      <SelectItem key={name} value={name}>{name}</SelectItem>
                    ))}
                    {categoryValue !== "" && !categoryNames.includes(categoryValue) && (
                      <SelectItem value={categoryValue}>{categoryValue}</SelectItem>
                    )}
                  </SelectContent>
                </Select>
                {pinnedCategory !== "" && (
                  <p className="text-xs text-muted-foreground">
                    {t("manualCrossSeed.pinnedCategory", { category: pinnedCategory })}
                  </p>
                )}
              </div>

              <div className="space-y-2">
                <Label htmlFor="manual-cross-seed-new-tag">{t("manualCrossSeed.tags")}</Label>
                <div className="flex flex-wrap gap-1.5">
                  {availableTags.map(tag => {
                    const isActive = selectedTags.includes(tag)
                    return (
                      <Badge
                        key={tag}
                        asChild
                        variant={isActive ? "default" : "outline"}
                        className="cursor-pointer select-none min-h-[44px] px-3 text-sm sm:min-h-0 sm:px-2 sm:text-xs"
                      >
                        <button type="button" onClick={() => toggleTag(tag)}>{tag}</button>
                      </Badge>
                    )
                  })}
                </div>
                <Input
                  id="manual-cross-seed-new-tag"
                  enterKeyHint="done"
                  value={newTag}
                  onChange={event => setNewTag(event.target.value)}
                  onKeyDown={event => {
                    if (event.key === "Enter") {
                      event.preventDefault()
                      const tag = newTag.trim()
                      if (tag !== "" && !selectedTags.includes(tag)) {
                        setTagsEdit([...selectedTags, tag])
                      }
                      setNewTag("")
                    }
                  }}
                  placeholder={t("manualCrossSeed.newTagPlaceholder")}
                />
              </div>

              <div className="space-y-1">
                <Label>{t("manualCrossSeed.savePath")}</Label>
                <p className="rounded-md border bg-muted/40 px-3 py-2 font-mono text-xs break-all">
                  {assembling ? preview?.destination : selectedProposal?.effectiveSavePath}
                </p>
              </div>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
            {t("manualCrossSeed.cancel")}
          </Button>
          <Button
            type="button"
            disabled={!torrentData || effectiveSelectedHashes.length === 0 || applyMutation.isPending || (assembling ? !preview?.ready || checkQuery.isFetching || checkQuery.isError || Boolean(assemblyUnavailable) : !selectedProposal)}
            onClick={() => applyMutation.mutate()}
          >
            {applyMutation.isPending ? t("manualCrossSeed.applying") : t("manualCrossSeed.apply")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
