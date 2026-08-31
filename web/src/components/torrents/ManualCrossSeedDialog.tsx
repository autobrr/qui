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
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { api } from "@/lib/api"
import { fileToBase64, overlapPercent } from "@/lib/manual-cross-seed"
import { formatBytes } from "@/lib/utils"
import type { ManualCrossSeedProposal } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
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
  const [selectedHash, setSelectedHash] = useState<string | null>(null)
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

  const requestedHash = pinnedHash ?? preselectedTarget?.hash ?? undefined
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
  })
  const proposals = useMemo(
    () => proposalsQuery.data?.proposals ?? [],
    [proposalsQuery.data]
  )

  const effectiveSelectedHash = selectedHash ?? preselectedTarget?.hash ?? proposals[0]?.hash ?? null
  const selectedProposal: ManualCrossSeedProposal | undefined = proposals.find(
    proposal => proposal.hash.toLowerCase() === effectiveSelectedHash?.toLowerCase()
  )

  const categoryValue = categoryEdit ?? selectedProposal?.category ?? ""
  const selectedTags = tagsEdit ?? proposalsQuery.data?.defaultTags ?? []
  const toggleTag = (tag: string) => {
    setTagsEdit(selectedTags.includes(tag) ? selectedTags.filter(item => item !== tag) : [...selectedTags, tag])
  }

  const pickerQuery = useQuery({
    queryKey: ["cross-seed-manual-picker", instanceId, pickerSearch],
    queryFn: ({ signal }) => api.getTorrents(instanceId, { search: pickerSearch, limit: 20, preferCached: true }, signal),
    enabled: open && showPicker && pickerSearch.trim().length > 0,
    staleTime: 30_000,
  })

  const applyMutation = useMutation({
    mutationFn: () => api.applyManualCrossSeed({
      instanceId,
      torrentData: torrentData ?? "",
      targetHash: effectiveSelectedHash ?? "",
      category: categoryValue || undefined,
      tags: selectedTags,
    }),
    onSuccess: response => {
      const firstResult = response.results[0]
      if (response.success) {
        toast.success(t("manualCrossSeed.applySuccess"))
        setTimeout(() => {
          void queryClient.invalidateQueries({ queryKey: ["torrents-list", instanceId] })
        }, 500)
        handleOpenChange(false)
        onApplied?.()
      } else {
        toast.error(t("manualCrossSeed.applyFailed", { message: firstResult?.message ?? firstResult?.status ?? "" }))
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
      setSelectedHash(null)
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
    setSelectedHash(hash)
    setCategoryEdit(null)
    setShowPicker(false)
  }

  const handlePickFromSearch = (hash: string) => {
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
  const source = proposalsQuery.data

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      {/* Top-pinned with dvh so the footer stays reachable when the iOS
          keyboard opens (same mitigation as AddTorrentDialog). */}
      <DialogContent className="sm:max-w-2xl max-h-[90dvh] sm:max-h-[85dvh] flex flex-col !translate-y-0 !top-[5vh] sm:!top-[7.5vh]">
        <DialogHeader>
          <DialogTitle>{t("manualCrossSeed.title")}</DialogTitle>
          <DialogDescription>{t("manualCrossSeed.description")}</DialogDescription>
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
                  setSelectedHash(null)
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
              <Label>{t("manualCrossSeed.targetLabel")}</Label>
              {proposals.length === 0 && (
                <p className="text-sm text-muted-foreground">{t("manualCrossSeed.noProposals")}</p>
              )}
              {proposals.length > 0 && (
                <div className="space-y-1 rounded-md border p-1 sm:max-h-48 sm:overflow-y-auto">
                  {proposals.map(proposal => {
                    const isSelected = proposal.hash.toLowerCase() === effectiveSelectedHash?.toLowerCase()
                    return (
                      <button
                        key={proposal.hash}
                        type="button"
                        onClick={() => handleSelectTarget(proposal.hash)}
                        className={`w-full rounded-sm px-2 py-1.5 text-left text-sm transition-colors ${
                          isSelected ? "bg-accent text-accent-foreground" : "hover:bg-muted"
                        }`}
                      >
                        <span className="block truncate">{proposal.name}</span>
                        <span className="block text-xs text-muted-foreground">
                          {formatBytes(proposal.size)} · {t("manualCrossSeed.overlap", { percent: overlapPercent(proposal.overlapFraction) })}
                        </span>
                      </button>
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
                  {pickerQuery.data && (
                    <div className="rounded-md border p-1 sm:max-h-40 sm:overflow-y-auto">
                      {completePickerTorrents.length === 0 && (
                        <p className="px-2 py-1.5 text-sm text-muted-foreground">{t("manualCrossSeed.pickerNoResults")}</p>
                      )}
                      {completePickerTorrents.map(torrent => (
                        <button
                          key={torrent.hash}
                          type="button"
                          onClick={() => handlePickFromSearch(torrent.hash)}
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

          {zeroOverlap && (
            <div className="flex items-start gap-2 rounded-md border border-yellow-500/50 bg-yellow-500/10 p-3 text-sm">
              <AlertTriangle className="h-4 w-4 mt-0.5 shrink-0 text-yellow-600 dark:text-yellow-500" />
              <p>{t("manualCrossSeed.zeroOverlapWarning")}</p>
            </div>
          )}

          {selectedProposal && (
            <div className="space-y-3">
              <div className="space-y-2">
                <Label>{t("manualCrossSeed.category")}</Label>
                <Select
                  value={categoryValue === "" ? "__none__" : categoryValue}
                  onValueChange={value => setCategoryEdit(value === "__none__" ? "" : value)}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {(selectedProposal?.category ?? "") === "" && (
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
                  {selectedProposal.effectiveSavePath}
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
            disabled={!torrentData || !selectedProposal || applyMutation.isPending}
            onClick={() => applyMutation.mutate()}
          >
            {applyMutation.isPending ? t("manualCrossSeed.applying") : t("manualCrossSeed.apply")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
