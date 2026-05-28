/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
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
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { api } from "@/lib/api"
import {
  buildTagEditorItems,
  buildTagUpdatePlan,
  cycleTagSelectionState,
  hasTagUpdatePlan,
  sortTags,
  type TagEditorItem,
  type TagUpdatePlan
} from "@/lib/tag-editor"
import { cn } from "@/lib/utils"
import { usePathAutocomplete } from "@/hooks/usePathAutocomplete"
import type { Category, InstanceCapabilities, Torrent, TorrentFilters } from "@/types"
import { useVirtualizer } from "@tanstack/react-virtual"
import { AlertTriangle, Loader2, Plus } from "lucide-react"
import type { ChangeEvent, KeyboardEvent } from "react"
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { buildCategoryTree, type CategoryNode } from "./CategoryTree"
import {
  checkFieldConsistency,
  LIMIT_UNLIMITED,
  LIMIT_USE_GLOBAL,
  shareLimitEnumFieldFromTorrents,
  type TorrentLimitSnapshot
} from "./torrentLimitDialogHelpers"

export type { TorrentLimitSnapshot } from "./torrentLimitDialogHelpers"

interface TagEditorDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  availableTags: string[] | null
  selectedTorrents: Torrent[]
  hashCount: number
  selectionRequest?: {
    instanceId: number
    instanceIds?: number[]
    hashes?: string[]
    targets?: Array<{ instanceId: number; hash: string }>
    selectAll?: boolean
    filters?: TorrentFilters
    search?: string
    excludeHashes?: string[]
    excludeTargets?: Array<{ instanceId: number; hash: string }>
  }
  onConfirm: (plan: TagUpdatePlan) => void
  isPending?: boolean
  isLoadingTags?: boolean
}

export const TagEditorDialog = memo(function TagEditorDialog({
  open,
  onOpenChange,
  availableTags,
  selectedTorrents,
  hashCount,
  selectionRequest,
  onConfirm,
  isPending = false,
  isLoadingTags = false,
}: TagEditorDialogProps) {
  const { t } = useTranslation("torrents")
  const [items, setItems] = useState<TagEditorItem[]>([])
  const [newTag, setNewTag] = useState("")
  const [selectionTagValues, setSelectionTagValues] = useState<string[]>([])
  const [isLoadingSelectionTags, setIsLoadingSelectionTags] = useState(false)
  const [selectionBaselineError, setSelectionBaselineError] = useState<string | null>(null)
  const hasEditedRef = useRef(false)
  const previousOpenRef = useRef(false)
  const previousSelectionRequestKeyRef = useRef<string | null>(null)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const selectionRequestKey = useMemo(() => JSON.stringify({
    instanceId: selectionRequest?.instanceId,
    instanceIds: selectionRequest?.instanceIds ?? [],
    hashes: selectionRequest?.hashes ?? [],
    targets: selectionRequest?.targets ?? [],
    selectAll: selectionRequest?.selectAll ?? false,
    filters: selectionRequest?.filters ?? null,
    search: selectionRequest?.search ?? "",
    excludeHashes: selectionRequest?.excludeHashes ?? [],
    excludeTargets: selectionRequest?.excludeTargets ?? [],
  }), [selectionRequest])
  const selectionRequestSnapshot = useMemo(() => ({
    instanceId: selectionRequest?.instanceId ?? -1,
    instanceIds: selectionRequest?.instanceIds,
    hashes: selectionRequest?.hashes,
    targets: selectionRequest?.targets,
    selectAll: selectionRequest?.selectAll,
    filters: selectionRequest?.filters,
    search: selectionRequest?.search,
    excludeHashes: selectionRequest?.excludeHashes,
    excludeTargets: selectionRequest?.excludeTargets,
  }), [
    selectionRequest?.excludeHashes,
    selectionRequest?.excludeTargets,
    selectionRequest?.filters,
    selectionRequest?.hashes,
    selectionRequest?.instanceId,
    selectionRequest?.instanceIds,
    selectionRequest?.search,
    selectionRequest?.selectAll,
    selectionRequest?.targets,
  ])
  const selectedTorrentTagsKey = useMemo(
    () => JSON.stringify(selectedTorrents.map(torrent => torrent.tags)),
    [selectedTorrents]
  )
  const selectedTorrentTagValues = useMemo(
    () => JSON.parse(selectedTorrentTagsKey) as string[],
    [selectedTorrentTagsKey]
  )
  const requiresRemoteBaseline = hashCount > selectedTorrents.length
  const hasValidInstance = selectionRequestSnapshot.instanceId >= 0
  const hasExplicitSelection = (selectionRequestSnapshot.targets?.length ?? 0) > 0 || (selectionRequestSnapshot.hashes?.length ?? 0) > 0
  const canFetchRemoteBaseline = hasValidInstance && (selectionRequestSnapshot.selectAll === true || hasExplicitSelection)

  useEffect(() => {
    if (!open) {
      setItems([])
      setNewTag("")
      setSelectionTagValues([])
      setIsLoadingSelectionTags(false)
      setSelectionBaselineError(null)
      hasEditedRef.current = false
      previousOpenRef.current = false
      previousSelectionRequestKeyRef.current = null
      return
    }

    const didOpen = !previousOpenRef.current
    const selectionRequestChanged = previousSelectionRequestKeyRef.current !== selectionRequestKey
    if (didOpen || selectionRequestChanged) {
      setNewTag("")
      hasEditedRef.current = false
      setItems([])
    }
    previousOpenRef.current = true
    previousSelectionRequestKeyRef.current = selectionRequestKey
    setSelectionBaselineError(null)

    if (!requiresRemoteBaseline) {
      setSelectionTagValues(selectedTorrentTagValues)
      setIsLoadingSelectionTags(false)
      return
    }

    let cancelled = false

    if (!canFetchRemoteBaseline) {
      setIsLoadingSelectionTags(true)
      setSelectionBaselineError(t("dialogs.tagEditor.baselineError"))
      return () => {
        cancelled = true
      }
    }

    setIsLoadingSelectionTags(true)

    void api.getTorrentField(selectionRequestSnapshot.instanceId, "tags", {
      hashes: selectionRequestSnapshot.hashes,
      targets: selectionRequestSnapshot.targets,
      selectAll: selectionRequestSnapshot.selectAll,
      filters: selectionRequestSnapshot.selectAll ? selectionRequestSnapshot.filters : undefined,
      search: selectionRequestSnapshot.selectAll ? selectionRequestSnapshot.search : undefined,
      excludeHashes: selectionRequestSnapshot.selectAll ? selectionRequestSnapshot.excludeHashes : undefined,
      excludeTargets: selectionRequestSnapshot.selectAll ? selectionRequestSnapshot.excludeTargets : undefined,
      instanceIds: selectionRequestSnapshot.instanceIds,
    }).then((response) => {
      if (cancelled) {
        return
      }

      setSelectionTagValues(response.values)
      setSelectionBaselineError(null)
      setIsLoadingSelectionTags(false)
    }).catch((error: Error) => {
      if (cancelled) {
        return
      }

      setSelectionBaselineError(error.message || t("dialogs.tagEditor.baselineError"))
      setIsLoadingSelectionTags(false)
      onOpenChange(false)
      toast.error(t("dialogs.tagEditor.loadFailed"), {
        description: error.message || t("dialogs.tagEditor.baselineError"),
      })
    })

    return () => {
      cancelled = true
    }
  }, [canFetchRemoteBaseline, hashCount, onOpenChange, open, requiresRemoteBaseline, selectedTorrentTagValues, selectionRequestKey, selectionRequestSnapshot, t])

  useEffect(() => {
    if (!open || isLoadingTags || isLoadingSelectionTags || hasEditedRef.current || selectionBaselineError) {
      return
    }

    setItems(buildTagEditorItems(availableTags, selectionTagValues, hashCount))
  }, [availableTags, hashCount, isLoadingSelectionTags, isLoadingTags, open, selectionBaselineError, selectionTagValues])

  const knownTagSet = useMemo(() => new Set(availableTags ?? []), [availableTags])
  const updatePlan = useMemo(() => buildTagUpdatePlan(items), [items])
  const hasChanges = hasTagUpdatePlan(updatePlan)
  const isLoadingState = isLoadingTags || isLoadingSelectionTags
  const shouldUseVirtualization = items.length > 50

  const virtualizer = useVirtualizer({
    count: shouldUseVirtualization ? items.length : 0,
    getScrollElement: () => scrollContainerRef.current,
    estimateSize: () => 40,
    overscan: 5,
  })

  const resetItems = useCallback(() => {
    hasEditedRef.current = false
    setItems(buildTagEditorItems(availableTags, selectionTagValues, hashCount))
    setNewTag("")
  }, [availableTags, hashCount, selectionTagValues])

  const handleConfirm = useCallback((): void => {
    if (!hasChanges) {
      onOpenChange(false)
      return
    }

    onConfirm(updatePlan)
    setNewTag("")
  }, [hasChanges, onConfirm, onOpenChange, updatePlan])

  const handleCancel = useCallback((): void => {
    setNewTag("")
    onOpenChange(false)
  }, [onOpenChange])

  const toggleTag = useCallback((tag: string): void => {
    if (!isLoadingTags && !isLoadingSelectionTags) {
      hasEditedRef.current = true
    }
    setItems(prev => prev.map((item) => {
      if (item.tag !== tag) {
        return item
      }

      return {
        ...item,
        state: cycleTagSelectionState(item.state),
      }
    }))
  }, [isLoadingSelectionTags, isLoadingTags])

  const clearAll = useCallback((): void => {
    if (!isLoadingTags && !isLoadingSelectionTags) {
      hasEditedRef.current = true
    }
    setItems(prev => prev.map(item => item.state === "off" ? item : { ...item, state: "off" }))
  }, [isLoadingSelectionTags, isLoadingTags])

  const addNewTag = useCallback((tagToAdd: string): void => {
    if (isLoadingTags || isLoadingSelectionTags) {
      return
    }

    const trimmedTag = tagToAdd.trim()
    if (!trimmedTag) {
      return
    }

    hasEditedRef.current = true
    setItems((prev) => {
      const existing = prev.find(item => item.tag === trimmedTag)
      if (existing) {
        return prev.map(item => item.tag === trimmedTag ? { ...item, state: "on" } : item)
      }

      return sortTags([...prev.map(item => item.tag), trimmedTag]).map((tag) => {
        if (tag !== trimmedTag) {
          return prev.find(item => item.tag === tag) as TagEditorItem
        }

        return {
          tag,
          initialState: "off",
          state: "on",
        }
      })
    })
    setNewTag("")
  }, [isLoadingSelectionTags, isLoadingTags])

  const renderTagRow = useCallback((item: TagEditorItem) => {
    const isNew = !knownTagSet.has(item.tag)

    return (
      <button
        key={item.tag}
        type="button"
        onClick={() => toggleTag(item.tag)}
        className="flex w-full items-center justify-between gap-3 rounded-md px-2 py-1.5 text-left hover:bg-muted/60"
      >
        <div className="flex min-w-0 items-center gap-2">
          <Checkbox
            checked={item.state === "mixed" ? "indeterminate" : item.state === "on"}
            className="pointer-events-none"
          />
          <span className={cn("truncate text-sm font-medium", isNew && "text-primary italic")}>
            {item.tag}
          </span>
          {isNew && <span className="text-xs text-muted-foreground">{t("dialogs.tagEditor.new")}</span>}
        </div>
        <span className="shrink-0 text-xs text-muted-foreground">
          {item.state === "mixed" ? t("dialogs.tagEditor.mixed") : item.state === "on" ? t("dialogs.tagEditor.on") : t("dialogs.tagEditor.off")}
        </span>
      </button>
    )
  }, [knownTagSet, toggleTag, t])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t("dialogs.tagEditor.title", { count: hashCount })}</DialogTitle>
          <DialogDescription>
            {t("dialogs.tagEditor.description")}
          </DialogDescription>
        </DialogHeader>
        <div className="py-4 space-y-4">
          {selectionBaselineError ? (
            <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
              {selectionBaselineError}
            </div>
          ) : isLoadingState ? (
            <div className="space-y-2">
              <Label>{t("dialogs.tagEditor.availableTags")}</Label>
              <div className="h-48 border rounded-md p-3 flex items-center justify-center">
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  <span className="text-sm">{t("dialogs.tagEditor.loadingTags")}</span>
                </div>
              </div>
            </div>
          ) : items.length > 0 ? (
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label>{t("dialogs.tagEditor.availableTags")}</Label>
                <div className="flex items-center gap-2">
                  <Button type="button" size="sm" variant="outline" onClick={resetItems} disabled={!hasChanges}>
                    {t("dialogs.tagEditor.reset")}
                  </Button>
                  <Button type="button" size="sm" variant="outline" onClick={clearAll}>
                    {t("dialogs.tagEditor.clearAll")}
                  </Button>
                </div>
              </div>
              <div
                ref={scrollContainerRef}
                className="h-48 border rounded-md p-3 overflow-y-auto"
              >
                {shouldUseVirtualization ? (
                  <div
                    style={{
                      height: `${virtualizer.getTotalSize()}px`,
                      width: "100%",
                      position: "relative",
                    }}
                  >
                    {virtualizer.getVirtualItems().map((virtualRow) => {
                      const item = items[virtualRow.index]
                      return (
                        <div
                          key={item.tag}
                          data-index={virtualRow.index}
                          ref={virtualizer.measureElement}
                          style={{
                            position: "absolute",
                            top: 0,
                            left: 0,
                            width: "100%",
                            transform: `translateY(${virtualRow.start}px)`,
                          }}
                        >
                          {renderTagRow(item)}
                        </div>
                      )
                    })}
                  </div>
                ) : (
                  <div className="space-y-1">
                    {items.map(renderTagRow)}
                  </div>
                )}
              </div>
            </div>
          ) : (
            <div className="text-center py-8 text-muted-foreground">
              {t("dialogs.tagEditor.noTags")}
            </div>
          )}

          <div className="space-y-2">
            <Label htmlFor="newTag">{t("dialogs.tagEditor.addNewTag")}</Label>
            <div className="flex gap-2">
              <Input
                id="newTag"
                value={newTag}
                onChange={(e: ChangeEvent<HTMLInputElement>) => setNewTag(e.target.value)}
                placeholder={t("dialogs.tagEditor.enterNewTag")}
                disabled={isLoadingState}
                onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
                  if (e.key === "Enter" && newTag.trim()) {
                    e.preventDefault()
                    addNewTag(newTag)
                  }
                }}
              />
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => addNewTag(newTag)}
                disabled={isLoadingState || !newTag.trim()}
              >
                <Plus className="h-4 w-4" />
              </Button>
            </div>
          </div>

          {hasChanges ? (
            <div className="text-sm text-muted-foreground">
              {updatePlan.add.length > 0 && (
                <div>{t("dialogs.tagEditor.addEverywhere", { tags: updatePlan.add.join(", ") })}</div>
              )}
              {updatePlan.remove.length > 0 && (
                <div>{t("dialogs.tagEditor.removeEverywhere", { tags: updatePlan.remove.join(", ") })}</div>
              )}
            </div>
          ) : (
            <div className="text-sm text-muted-foreground">
              {t("dialogs.tagEditor.noTagChanges")}
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleCancel}>{t("dialogs.tagEditor.cancel")}</Button>
          <Button onClick={handleConfirm} disabled={isPending || !hasChanges || isLoadingState || Boolean(selectionBaselineError)}>{t("dialogs.tagEditor.apply")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface SetCategoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  availableCategories: Record<string, Category>
  hashCount: number
  onConfirm: (category: string) => void
  isPending?: boolean
  initialCategory?: string
  isLoadingCategories?: boolean
  useSubcategories?: boolean
}

interface SetLocationDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  hashCount: number
  onConfirm: (location: string) => void
  isPending?: boolean
  initialLocation?: string
  instanceId?: number
  capabilities?: InstanceCapabilities | null
}

export const SetLocationDialog = memo(function SetLocationDialog({
  open,
  onOpenChange,
  hashCount,
  onConfirm,
  isPending = false,
  initialLocation = "",
  instanceId = 0,
  capabilities,
}: SetLocationDialogProps) {
  const { t } = useTranslation("torrents")
  const [location, setLocation] = useState("")
  const wasOpen = useRef(false)

  const supportsPathAutocomplete = capabilities?.supportsPathAutocomplete ?? false

  const {
    suggestions,
    handleInputChange: handleAutocompleteChange,
    handleSelect,
    handleKeyDown: handleAutocompleteKeyDown,
    handleBlur: handleAutocompleteBlur,
    highlightedIndex,
    showSuggestions,
    inputRef: autocompleteInputRef,
  } = usePathAutocomplete(setLocation, instanceId)

  const inputRef = useRef<HTMLInputElement>(null)
  const effectiveInputRef = supportsPathAutocomplete ? autocompleteInputRef : inputRef

  // Initialize location only when dialog transitions from closed to open
  useEffect(() => {
    if (open && !wasOpen.current) {
      setLocation(initialLocation)
      if (supportsPathAutocomplete) {
        handleAutocompleteChange(initialLocation)
      }
      // Focus the input when dialog opens
      setTimeout(() => effectiveInputRef.current?.focus(), 0)
    }
    wasOpen.current = open
  }, [open, initialLocation, supportsPathAutocomplete, handleAutocompleteChange, effectiveInputRef])

  const handleConfirm = useCallback(() => {
    if (location.trim()) {
      onConfirm(location.trim())
      setLocation("")
    }
  }, [location, onConfirm])

  const handleCancel = useCallback(() => {
    setLocation("")
    onOpenChange(false)
  }, [onOpenChange])

  const handleKeyDown = useCallback((e: KeyboardEvent<HTMLInputElement>) => {
    if (supportsPathAutocomplete) {
      handleAutocompleteKeyDown(e)
    }
    if (e.key === "Enter" && !e.defaultPrevented && !isPending && location.trim()) {
      e.preventDefault()
      handleConfirm()
    }
  }, [isPending, location, handleConfirm, supportsPathAutocomplete, handleAutocompleteKeyDown])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("dialogs.setLocation.title", { count: hashCount })}</DialogTitle>
          <DialogDescription>
            {t("dialogs.setLocation.description")}
          </DialogDescription>
        </DialogHeader>
        <div className="py-4 space-y-4">
          <div className="space-y-2">
            <Label htmlFor="location">{t("dialogs.setLocation.location")}</Label>
            <Input
              ref={effectiveInputRef}
              id="location"
              type="text"
              autoComplete="off"
              spellCheck={false}
              value={location}
              onChange={(e: ChangeEvent<HTMLInputElement>) => {
                setLocation(e.target.value)
                if (supportsPathAutocomplete) {
                  handleAutocompleteChange(e.target.value)
                }
              }}
              onKeyDown={handleKeyDown}
              onBlur={supportsPathAutocomplete ? handleAutocompleteBlur : undefined}
              placeholder={t("dialogs.setLocation.placeholder")}
              disabled={isPending}
            />
            {supportsPathAutocomplete && showSuggestions && suggestions.length > 0 && (
              <div className="relative">
                <div className="absolute z-50 mt-1 left-0 right-0 rounded-md border bg-popover text-popover-foreground shadow-md">
                  <div className="max-h-55 overflow-y-auto py-1">
                    {suggestions.map((entry, idx) => (
                      <button
                        key={entry}
                        type="button"
                        title={entry}
                        className={cn(
                          "w-full px-3 py-2 text-sm hover:bg-accent hover:text-accent-foreground",
                          (highlightedIndex === idx) ? "bg-accent text-accent-foreground" : "hover:bg-accent/70"
                        )}
                        onMouseDown={(e) => e.preventDefault()}
                        onClick={() => handleSelect(entry)}
                      >
                        <span className="block truncate text-left">{entry}</span>
                      </button>
                    ))}
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleCancel} disabled={isPending}>
            {t("dialogs.setLocation.cancel")}
          </Button>
          <Button
            onClick={handleConfirm}
            disabled={isPending || !location.trim()}
          >
            {t("dialogs.setLocation.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface RenameTorrentDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentName?: string
  onConfirm: (name: string) => void | Promise<void>
  isPending?: boolean
}

interface SetCommentDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  hashCount: number
  instanceId: number
  torrentHash?: string
  onConfirm: (comment: string) => void | Promise<void>
  isPending?: boolean
}

export const SetCommentDialog = memo(function SetCommentDialog({
  open,
  onOpenChange,
  hashCount,
  instanceId,
  torrentHash,
  onConfirm,
  isPending = false,
}: SetCommentDialogProps) {
  const { t } = useTranslation("torrents")
  const [comment, setComment] = useState("")
  const [isLoadingComment, setIsLoadingComment] = useState(false)
  const wasOpen = useRef(false)

  const focusCommentField = useCallback(() => {
    setTimeout(() => document.getElementById("torrentComment")?.focus({ preventScroll: true }), 0)
  }, [])

  useEffect(() => {
    if (!open) {
      setComment("")
      setIsLoadingComment(false)
      wasOpen.current = false
      return
    }

    const didOpen = !wasOpen.current
    wasOpen.current = true

    if (!didOpen || hashCount !== 1 || !torrentHash) {
      setComment("")
      setIsLoadingComment(false)
      if (didOpen) {
        focusCommentField()
      }
      return
    }

    let cancelled = false
    setIsLoadingComment(true)

    void api.getTorrentProperties(instanceId, torrentHash).then((properties) => {
      if (cancelled) {
        return
      }
      setComment(properties.comment ?? "")
      setIsLoadingComment(false)
      focusCommentField()
    }).catch(() => {
      if (cancelled) {
        return
      }
      setComment("")
      setIsLoadingComment(false)
      focusCommentField()
    })

    return () => {
      cancelled = true
    }
  }, [open, hashCount, instanceId, torrentHash, focusCommentField])

  const handleConfirm = useCallback(() => {
    onConfirm(comment)
  }, [comment, onConfirm])

  const handleClose = useCallback((nextOpen: boolean) => {
    if (!nextOpen) {
      setComment("")
    }
    onOpenChange(nextOpen)
  }, [onOpenChange])

  const countLabel = hashCount > 1 ? ` (${hashCount})` : ""

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("commentDialog.title")}{countLabel}</DialogTitle>
          <DialogDescription>
            {hashCount > 1? t("commentDialog.descriptionMultiple"): t("commentDialog.descriptionSingle")}
          </DialogDescription>
        </DialogHeader>
        <div className="py-4 space-y-2">
          <Label htmlFor="torrentComment">{t("commentDialog.label")}</Label>
          <Textarea
            id="torrentComment"
            value={comment}
            onChange={(e: ChangeEvent<HTMLTextAreaElement>) => setComment(e.target.value)}
            placeholder={isLoadingComment ? t("commentDialog.loadingPlaceholder") : t("commentDialog.placeholder")}
            disabled={isPending || isLoadingComment}
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => handleClose(false)} disabled={isPending}>
            {t("commentDialog.cancel")}
          </Button>
          <Button onClick={handleConfirm} disabled={isPending || isLoadingComment}>
            {isPending ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                {t("commentDialog.saving")}
              </>
            ) : (
              t("commentDialog.save")
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

export const RenameTorrentDialog = memo(function RenameTorrentDialog({
  open,
  onOpenChange,
  currentName = "",
  onConfirm,
  isPending = false,
}: RenameTorrentDialogProps) {
  const { t } = useTranslation("torrents")
  const [name, setName] = useState("")
  const wasOpen = useRef(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (open && !wasOpen.current) {
      setName(currentName)
      setTimeout(() => inputRef.current?.focus({ preventScroll: true }), 0)
    }
    wasOpen.current = open
  }, [open, currentName])

  const handleConfirm = useCallback(() => {
    const trimmed = name.trim()
    if (!trimmed) return
    onConfirm(trimmed)
  }, [name, onConfirm])

  const handleClose = useCallback((nextOpen: boolean) => {
    if (!nextOpen) {
      setName("")
    }
    onOpenChange(nextOpen)
  }, [onOpenChange])

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("dialogs.renameTorrent.title")}</DialogTitle>
          <DialogDescription>
            {t("dialogs.renameTorrent.description")}
          </DialogDescription>
        </DialogHeader>
        <div className="py-4 space-y-4">
          <div className="space-y-2">
            <Label htmlFor="torrentName">{t("dialogs.renameTorrent.nameLabel")}</Label>
            <Input
              ref={inputRef}
              id="torrentName"
              value={name}
              onChange={(e: ChangeEvent<HTMLInputElement>) => setName(e.target.value)}
              placeholder={t("dialogs.renameTorrent.placeholder")}
              disabled={isPending}
              onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
                if (e.key === "Enter" && !isPending && name.trim()) {
                  e.preventDefault()
                  handleConfirm()
                }
              }}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => handleClose(false)} disabled={isPending}>
            {t("dialogs.renameTorrent.cancel")}
          </Button>
          <Button onClick={handleConfirm} disabled={isPending || !name.trim()}>
            {t("dialogs.renameTorrent.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface RenameTorrentFileDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  files?: { name: string }[]
  isLoading?: boolean
  onConfirm: (payload: { oldPath: string; newPath: string }) => void | Promise<void>
  isPending?: boolean
  initialPath?: string
}

export const RenameTorrentFileDialog = memo(function RenameTorrentFileDialog({
  open,
  onOpenChange,
  files = [],
  isLoading = false,
  onConfirm,
  isPending = false,
  initialPath,
}: RenameTorrentFileDialogProps) {
  const { t } = useTranslation("torrents")
  const [newName, setNewName] = useState("")
  const inputRef = useRef<HTMLInputElement>(null)

  // Parse the initial path into folder and filename
  const { folderPath, fileName } = useMemo(() => {
    if (!initialPath) return { folderPath: "", fileName: "" }
    const lastSlash = initialPath.lastIndexOf("/")
    if (lastSlash === -1) return { folderPath: "", fileName: initialPath }
    return {
      folderPath: initialPath.slice(0, lastSlash),
      fileName: initialPath.slice(lastSlash + 1),
    }
  }, [initialPath])

  // Check if file exists in the list
  const fileExists = useMemo(() => {
    return initialPath ? files.some(f => f.name === initialPath) : false
  }, [files, initialPath])

  // Initialize newName when dialog opens or path changes
  useEffect(() => {
    if (open && fileName) {
      setNewName(fileName)
      // Focus and select the filename (without extension) after a brief delay
      setTimeout(() => {
        if (inputRef.current) {
          inputRef.current.focus()
          const dotIndex = fileName.lastIndexOf(".")
          if (dotIndex > 0) {
            inputRef.current.setSelectionRange(0, dotIndex)
          } else {
            inputRef.current.select()
          }
        }
      }, 50)
    }
    if (!open) {
      setNewName("")
    }
  }, [open, fileName])

  const newPath = useMemo(() => {
    const trimmed = newName.trim()
    if (!trimmed) return ""
    return folderPath ? `${folderPath}/${trimmed}` : trimmed
  }, [folderPath, newName])

  const hasChanges = newName.trim() !== fileName

  const handleConfirm = useCallback(() => {
    if (!initialPath || !newName.trim()) return
    onConfirm({ oldPath: initialPath, newPath })
  }, [initialPath, newName, newPath, onConfirm])

  const handleClose = useCallback((nextOpen: boolean) => {
    if (!nextOpen) {
      setNewName("")
    }
    onOpenChange(nextOpen)
  }, [onOpenChange])

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="w-[calc(100vw-2.5rem)] max-w-md sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("dialogs.renameFile.title")}</DialogTitle>
        </DialogHeader>
        <div className="py-2 space-y-4">
          {isLoading ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="h-5 w-5 animate-spin mr-2" />
              {t("dialogs.renameFile.loading")}
            </div>
          ) : !initialPath || !fileExists ? (
            <div className="rounded-md border border-dashed py-6 text-center text-sm text-muted-foreground">
              {t("dialogs.renameFile.noFileSelected")}
            </div>
          ) : (
            <>
              {/* Current path display */}
              {folderPath && (
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">{t("dialogs.renameFile.location")}</Label>
                  <div className="text-xs font-mono text-muted-foreground bg-muted/50 rounded px-2.5 py-1.5 break-all">
                    {folderPath}/
                  </div>
                </div>
              )}

              {/* New name input */}
              <div className="space-y-1.5">
                <Label htmlFor="fileName">{t("dialogs.renameFile.fileName")}</Label>
                <Input
                  ref={inputRef}
                  id="fileName"
                  value={newName}
                  onChange={(e: ChangeEvent<HTMLInputElement>) => setNewName(e.target.value)}
                  placeholder={t("dialogs.renameFile.placeholder")}
                  disabled={isPending}
                  className="font-mono"
                  onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
                    if (e.key === "Enter" && !isPending && newName.trim() && hasChanges) {
                      e.preventDefault()
                      handleConfirm()
                    }
                  }}
                />
              </div>

            </>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => handleClose(false)} disabled={isPending}>
            {t("dialogs.renameFile.cancel")}
          </Button>
          <Button
            onClick={handleConfirm}
            disabled={isPending || !initialPath || !newName.trim() || !hasChanges || !fileExists}
          >
            {isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
            {t("dialogs.renameFile.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface RenameTorrentFolderDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  folders?: { name: string }[]
  isLoading?: boolean
  onConfirm: (payload: { oldPath: string; newPath: string }) => void | Promise<void>
  isPending?: boolean
  initialPath?: string
}

export const RenameTorrentFolderDialog = memo(function RenameTorrentFolderDialog({
  open,
  onOpenChange,
  folders = [],
  isLoading = false,
  onConfirm,
  isPending = false,
  initialPath,
}: RenameTorrentFolderDialogProps) {
  const { t } = useTranslation("torrents")
  const [selectedPath, setSelectedPath] = useState("")
  const [newName, setNewName] = useState("")
  const inputRef = useRef<HTMLInputElement>(null)

  const sortedFolders = useMemo(() => {
    return folders.slice().sort((a, b) => a.name.localeCompare(b.name))
  }, [folders])

  // Parse the selected path into parent and folder name
  const { parentPath, folderName } = useMemo(() => {
    const path = selectedPath || initialPath || ""
    if (!path) return { parentPath: "", folderName: "" }
    const lastSlash = path.lastIndexOf("/")
    if (lastSlash === -1) return { parentPath: "", folderName: path }
    return {
      parentPath: path.slice(0, lastSlash),
      folderName: path.slice(lastSlash + 1),
    }
  }, [selectedPath, initialPath])

  // Check if folder exists
  const folderExists = useMemo(() => {
    const path = selectedPath || initialPath
    return path ? folders.some(f => f.name === path) : false
  }, [folders, selectedPath, initialPath])

  // Initialize when dialog opens
  useEffect(() => {
    if (open) {
      const path = initialPath || sortedFolders[0]?.name || ""
      setSelectedPath(path)
      if (path) {
        const segments = path.split("/")
        const name = segments[segments.length - 1] || path
        setNewName(name)
        setTimeout(() => {
          if (inputRef.current) {
            inputRef.current.focus()
            inputRef.current.select()
          }
        }, 50)
      }
    }
    if (!open) {
      setSelectedPath("")
      setNewName("")
    }
  }, [open, initialPath, sortedFolders])

  const newPath = useMemo(() => {
    const trimmed = newName.trim()
    if (!trimmed) return ""
    return parentPath ? `${parentPath}/${trimmed}` : trimmed
  }, [parentPath, newName])

  const hasChanges = newName.trim() !== folderName

  const handleConfirm = useCallback(() => {
    const path = selectedPath || initialPath
    if (!path || !newName.trim()) return
    onConfirm({ oldPath: path, newPath })
  }, [selectedPath, initialPath, newName, newPath, onConfirm])

  const handleFolderSelect = useCallback((value: string) => {
    setSelectedPath(value)
    const segments = value.split("/")
    setNewName(segments[segments.length - 1] || value)
  }, [])

  const handleClose = useCallback((nextOpen: boolean) => {
    if (!nextOpen) {
      setSelectedPath("")
      setNewName("")
    }
    onOpenChange(nextOpen)
  }, [onOpenChange])

  // If we have an initialPath, show simplified UI. Otherwise show folder selector.
  const showFolderSelector = !initialPath && sortedFolders.length > 1

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="w-[calc(100vw-2.5rem)] max-w-md sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("dialogs.renameFolder.title")}</DialogTitle>
        </DialogHeader>
        <div className="py-2 space-y-4 overflow-hidden">
          {isLoading ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="h-5 w-5 animate-spin mr-2" />
              {t("dialogs.renameFolder.loading")}
            </div>
          ) : sortedFolders.length === 0 ? (
            <div className="rounded-md border border-dashed py-6 text-center text-sm text-muted-foreground">
              {t("dialogs.renameFolder.noFoldersAvailable")}
            </div>
          ) : (
            <>
              {/* Folder selector - only if no initialPath and multiple folders */}
              {showFolderSelector && (
                <div className="space-y-1.5">
                  <Label htmlFor="folderSelect">{t("dialogs.renameFolder.selectFolder")}</Label>
                  <Select value={selectedPath} onValueChange={handleFolderSelect}>
                    <SelectTrigger id="folderSelect" className="font-mono text-xs">
                      <SelectValue placeholder={t("dialogs.renameFolder.chooseFolder")} />
                    </SelectTrigger>
                    <SelectContent>
                      {sortedFolders.map((folder) => (
                        <SelectItem key={folder.name} value={folder.name} className="font-mono text-xs">
                          {folder.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              )}

              {/* Parent path display */}
              {parentPath && (
                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">{t("dialogs.renameFolder.location")}</Label>
                  <div className="text-xs font-mono text-muted-foreground bg-muted/50 rounded px-2.5 py-1.5 break-all">
                    {parentPath}/
                  </div>
                </div>
              )}

              {/* New name input */}
              <div className="space-y-1.5">
                <Label htmlFor="folderName">{t("dialogs.renameFolder.folderName")}</Label>
                <Input
                  ref={inputRef}
                  id="folderName"
                  value={newName}
                  onChange={(e: ChangeEvent<HTMLInputElement>) => setNewName(e.target.value)}
                  placeholder={t("dialogs.renameFolder.placeholder")}
                  disabled={isPending}
                  className="font-mono"
                  onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
                    if (e.key === "Enter" && !isPending && newName.trim() && hasChanges) {
                      e.preventDefault()
                      handleConfirm()
                    }
                  }}
                />
              </div>


              <p className="text-xs text-muted-foreground">
                {t("dialogs.renameFolder.moveWarning")}
              </p>
            </>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => handleClose(false)} disabled={isPending}>
            {t("dialogs.renameFolder.cancel")}
          </Button>
          <Button
            onClick={handleConfirm}
            disabled={isPending || !folderExists || !newName.trim() || !hasChanges}
          >
            {isPending && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
            {t("dialogs.renameFolder.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

export const SetCategoryDialog = memo(function SetCategoryDialog({
  open,
  onOpenChange,
  availableCategories,
  hashCount,
  onConfirm,
  isPending = false,
  initialCategory = "",
  isLoadingCategories = false,
  useSubcategories = false,
}: SetCategoryDialogProps) {
  const { t } = useTranslation("torrents")
  const [categoryInput, setCategoryInput] = useState("")
  const [searchQuery, setSearchQuery] = useState("")
  const [dialogCategories, setDialogCategories] = useState<Record<string, Category>>({})
  const [dialogUseSubcategories, setDialogUseSubcategories] = useState(useSubcategories)
  const wasOpen = useRef(false)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const availableCategoryCount = Object.keys(availableCategories || {}).length
  const dialogCategoryCount = Object.keys(dialogCategories).length

  // Freeze the category list while the dialog is open so background table refreshes
  // do not reshuffle the scroll container. If the dialog opened before categories
  // finished loading, hydrate exactly once when the first non-empty list arrives.
  useEffect(() => {
    if (open && !wasOpen.current) {
      setCategoryInput(initialCategory)
      setSearchQuery("")
      setDialogCategories(availableCategories || {})
      setDialogUseSubcategories(useSubcategories)
    } else if (open && dialogCategoryCount === 0 && availableCategoryCount > 0) {
      setDialogCategories(availableCategories || {})
      setDialogUseSubcategories(useSubcategories)
    } else if (!open && wasOpen.current) {
      setDialogCategories({})
      setDialogUseSubcategories(useSubcategories)
    }
    wasOpen.current = open
  }, [availableCategories, availableCategoryCount, dialogCategoryCount, initialCategory, open, useSubcategories])

  const handleConfirm = useCallback(() => {
    onConfirm(categoryInput)
    setCategoryInput("")
    setSearchQuery("")
  }, [categoryInput, onConfirm])

  const handleCancel = useCallback(() => {
    setCategoryInput("")
    setSearchQuery("")
    onOpenChange(false)
  }, [onOpenChange])

  // Filter categories based on search, with subcategory support
  const categoryList = useMemo(() => Object.keys(dialogCategories).sort(), [dialogCategories])

  const filteredCategories = useMemo(() => {
    const query = searchQuery.trim().toLowerCase()

    if (dialogUseSubcategories) {
      const tree = buildCategoryTree(dialogCategories, {})
      const shouldIncludeCache = new Map<CategoryNode, boolean>()

      const shouldIncludeNode = (node: CategoryNode): boolean => {
        const cached = shouldIncludeCache.get(node)
        if (cached !== undefined) {
          return cached
        }

        const nodeMatches = query === "" || node.name.toLowerCase().includes(query)
        if (nodeMatches) {
          shouldIncludeCache.set(node, true)
          return true
        }

        for (const child of node.children) {
          if (shouldIncludeNode(child)) {
            shouldIncludeCache.set(node, true)
            return true
          }
        }

        shouldIncludeCache.set(node, false)
        return false
      }

      const flattened: Array<{ name: string; displayName: string; level: number }> = []

      const visitNodes = (nodes: CategoryNode[]) => {
        for (const node of nodes) {
          if (shouldIncludeNode(node)) {
            flattened.push({
              name: node.name,
              displayName: node.displayName,
              level: node.level,
            })
            visitNodes(node.children)
          }
        }
      }

      visitNodes(tree)
      return flattened
    }

    const names = categoryList
    const namesFiltered = query ? names.filter(cat => cat.toLowerCase().includes(query)) : names

    return namesFiltered.map((name) => ({
      name,
      displayName: name,
      level: 0,
    }))
  }, [categoryList, dialogCategories, dialogUseSubcategories, searchQuery])

  const showLoadingCategories = isLoadingCategories && dialogCategoryCount === 0
  const showSearch = !showLoadingCategories && categoryList.length > 10

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t("dialogs.setCategory.title", { count: hashCount })}</DialogTitle>
          <DialogDescription>
            {t("dialogs.setCategory.description")}
          </DialogDescription>
        </DialogHeader>
        <div className="py-4 space-y-4">
          {/* Search bar for categories */}
          <div className={showSearch ? "space-y-2" : "hidden"} aria-hidden={!showSearch}>
            {showSearch && (
              <>
                <Label htmlFor="categorySearch">{t("dialogs.setCategory.searchCategories")}</Label>
                <Input
                  id="categorySearch"
                  placeholder={t("dialogs.setCategory.searchPlaceholder")}
                  value={searchQuery}
                  onChange={(e: ChangeEvent<HTMLInputElement>) => setSearchQuery(e.target.value)}
                />
              </>
            )}
          </div>

          {/* Category list with optional virtualization */}
          <div className="space-y-2">
            <Label>{t("dialogs.setCategory.selectCategory")}</Label>
            <div
              ref={scrollContainerRef}
              className="max-h-64 border rounded-md overflow-y-auto"
            >
              {showLoadingCategories ? (
                <div className="p-3 flex items-center justify-center">
                  <div className="flex items-center gap-2 text-muted-foreground">
                    <Loader2 className="h-4 w-4 animate-spin" />
                    <span className="text-sm">{t("dialogs.setCategory.loadingCategories")}</span>
                  </div>
                </div>
              ) : (
                <>
                  {/* No category option */}
                  <button
                    type="button"
                    onClick={() => setCategoryInput("")}
                    className={`w-full text-left px-3 py-2 hover:bg-accent transition-colors ${
                      categoryInput === "" ? "bg-accent" : ""
                    }`}
                  >
                    <span className="text-sm text-muted-foreground italic">{t("dialogs.setCategory.noCategory")}</span>
                  </button>

                  <div>
                    {filteredCategories.map((category) => (
                      <button
                        key={category.name}
                        type="button"
                        onClick={() => setCategoryInput(category.name)}
                        className={`w-full text-left px-3 py-2 hover:bg-accent transition-colors ${
                          categoryInput === category.name ? "bg-accent" : ""
                        }`}
                        title={category.name}
                      >
                        <span
                          className="text-sm"
                          style={category.level > 0 ? { paddingLeft: category.level * 12 } : undefined}
                        >
                          {category.displayName}
                        </span>
                      </button>
                    ))}
                  </div>

                  {filteredCategories.length === 0 && searchQuery && (
                    <div className="px-3 py-6 text-center text-sm text-muted-foreground">
                      {t("dialogs.setCategory.noCategoriesFound", { query: searchQuery })}
                    </div>
                  )}
                </>
              )}
            </div>
          </div>

          {/* Option to enter new category */}
          <div className="space-y-2">
            <Label htmlFor="newCategory">{t("dialogs.setCategory.createNew")}</Label>
            <Input
              id="newCategory"
              placeholder={t("dialogs.setCategory.createPlaceholder")}
              value={categoryInput && !categoryList.includes(categoryInput) ? categoryInput : ""}
              onChange={(e: ChangeEvent<HTMLInputElement>) => setCategoryInput(e.target.value)}
              onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
                if (e.key === "Enter") {
                  handleConfirm()
                }
              }}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleCancel}>{t("dialogs.tagEditor.cancel")}</Button>
          <Button
            onClick={handleConfirm}
            disabled={isPending}
          >
            {t("dialogs.setCategory.setCategory")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface CreateAndAssignCategoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  hashCount: number
  onConfirm: (category: string) => void
  isPending?: boolean
}

export const CreateAndAssignCategoryDialog = memo(function CreateAndAssignCategoryDialog({
  open,
  onOpenChange,
  hashCount,
  onConfirm,
  isPending = false,
}: CreateAndAssignCategoryDialogProps) {
  const { t } = useTranslation("torrents")
  const [categoryName, setCategoryName] = useState("")
  const wasOpen = useRef(false)

  // Reset when dialog opens
  useEffect(() => {
    if (open && !wasOpen.current) {
      setCategoryName("")
    }
    wasOpen.current = open
  }, [open])

  const handleConfirm = useCallback(() => {
    if (categoryName.trim()) {
      onConfirm(categoryName.trim())
      setCategoryName("")
    }
  }, [categoryName, onConfirm])

  const handleCancel = useCallback(() => {
    setCategoryName("")
    onOpenChange(false)
  }, [onOpenChange])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("dialogs.createAndAssignCategory.title")}</DialogTitle>
          <DialogDescription>
            {t("dialogs.createAndAssignCategory.description", { count: hashCount })}
          </DialogDescription>
        </DialogHeader>
        <div className="py-4 space-y-2">
          <Label htmlFor="categoryName">{t("dialogs.createAndAssignCategory.categoryName")}</Label>
          <Input
            id="categoryName"
            placeholder={t("dialogs.createAndAssignCategory.placeholder")}
            value={categoryName}
            onChange={(e: ChangeEvent<HTMLInputElement>) => setCategoryName(e.target.value)}
            onKeyDown={(e: KeyboardEvent<HTMLInputElement>) => {
              if (e.key === "Enter" && categoryName.trim()) {
                handleConfirm()
              }
            }}
            autoFocus
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleCancel}>{t("dialogs.createAndAssignCategory.cancel")}</Button>
          <Button
            onClick={handleConfirm}
            disabled={isPending || !categoryName.trim()}
          >
            {t("dialogs.createAndAssignCategory.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})


interface EditTrackerDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  tracker: string
  trackerURLs?: string[]
  loadingURLs?: boolean
  selectedHashes: string[]
  onConfirm: (oldURL: string, newURL: string) => void
  isPending?: boolean
  onConvertHttpToHttps?: () => void
  isConverting?: boolean
}

export const EditTrackerDialog = memo(function EditTrackerDialog({
  open,
  onOpenChange,
  instanceId: _instanceId, // eslint-disable-line @typescript-eslint/no-unused-vars
  tracker,
  trackerURLs = [],
  loadingURLs = false,
  selectedHashes,
  onConfirm,
  isPending = false,
  onConvertHttpToHttps,
  isConverting = false,
}: EditTrackerDialogProps) {
  const { t } = useTranslation("torrents")
  const [oldURL, setOldURL] = useState("")
  const [newURL, setNewURL] = useState("")
  const wasOpen = useRef(false)

  // Initialize URLs when dialog opens
  useEffect(() => {
    if (open && !wasOpen.current) {
      // Set the first tracker URL if available, otherwise clear
      if (trackerURLs && trackerURLs.length > 0) {
        setOldURL(trackerURLs[0])
      } else {
        setOldURL("")
      }
      setNewURL("")
    }
    wasOpen.current = open
  }, [open, tracker, trackerURLs])

  // Update oldURL selection when trackerURLs refresh (e.g., after HTTP→HTTPS conversion)
  // If the selected URL was converted, try to select its https equivalent or first available
  useEffect(() => {
    if (!open || !oldURL) return
    // If current selection still exists, keep it
    if (trackerURLs.includes(oldURL)) return
    // If it was an http:// URL, try to find its https:// equivalent by matching hostname/pathname
    if (oldURL.startsWith("http://")) {
      try {
        const parsed = new URL(oldURL)
        // Find an HTTPS URL with matching hostname and pathname (port may differ)
        const httpsMatch = trackerURLs.find((url) => {
          if (!url.startsWith("https://")) return false
          try {
            const candidate = new URL(url)
            return (
              candidate.hostname.toLowerCase() === parsed.hostname.toLowerCase() &&
              candidate.pathname === parsed.pathname &&
              candidate.search === parsed.search
            )
          } catch {
            return false
          }
        })
        if (httpsMatch) {
          setOldURL(httpsMatch)
          return
        }
      } catch {
        // Parsing failed, fall through to fallback
      }
    }
    // Fall back to first available URL
    if (trackerURLs.length > 0) {
      setOldURL(trackerURLs[0])
    }
  }, [open, oldURL, trackerURLs])

  const handleConfirm = useCallback((): void => {
    if (oldURL.trim() && newURL.trim()) {
      onConfirm(oldURL.trim(), newURL.trim())
      setOldURL("")
      setNewURL("")
    }
  }, [oldURL, newURL, onConfirm])

  const handleCancel = useCallback((): void => {
    setOldURL("")
    setNewURL("")
    onOpenChange(false)
  }, [onOpenChange])

  const hashCount = selectedHashes.length
  const isFilteredMode = hashCount === 0 // When no hashes provided, we're updating all torrents with this tracker

  // Check if there are any HTTP URLs that could be converted to HTTPS
  const hasHttpUrls = useMemo(
    () => trackerURLs.some((url) => url.startsWith("http://")),
    [trackerURLs]
  )

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className="max-w-xl">
        <AlertDialogHeader>
          <AlertDialogTitle>{t("dialogs.editTracker.title")} - {tracker}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("dialogs.editTracker.description", { tracker })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <Label htmlFor="oldURL">{t("dialogs.editTracker.currentUrl")}</Label>
            {loadingURLs ? (
              <div className="flex items-center justify-center py-3 text-sm text-muted-foreground">
                <span className="animate-pulse">{t("dialogs.editTracker.loadingUrls")}</span>
              </div>
            ) : trackerURLs && trackerURLs.length > 1 ? (
              <div className="space-y-2">
                <select
                  className="w-full px-3 py-2 text-sm font-mono border rounded-md bg-background"
                  value={oldURL}
                  onChange={(e) => setOldURL(e.target.value)}
                >
                  <option value="">{t("dialogs.editTracker.selectTrackerUrl")}</option>
                  {trackerURLs.map((url) => (
                    <option key={url} value={url}>
                      {url}
                    </option>
                  ))}
                </select>
                <p className="text-xs text-muted-foreground">
                  {t("dialogs.editTracker.multipleUrlsHelp")}
                </p>
              </div>
            ) : (
              <>
                <Input
                  id="oldURL"
                  value={oldURL}
                  onChange={(e) => setOldURL(e.target.value)}
                  placeholder={trackerURLs.length === 0 ? t("dialogs.editTracker.currentUrlPlaceholder", { tracker }) : ""}
                  className="font-mono text-sm"
                />
                {trackerURLs.length === 0 && (
                  <p className="text-xs text-muted-foreground">
                    {t("dialogs.editTracker.currentUrlNoDetectedHelp")}
                  </p>
                )}
                {trackerURLs.length === 1 && (
                  <p className="text-xs text-muted-foreground">
                    {t("dialogs.editTracker.currentUrlDetectedHelp")}
                  </p>
                )}
              </>
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="newURL">{t("dialogs.editTracker.newUrl")}</Label>
            <Input
              id="newURL"
              value={newURL}
              onChange={(e) => setNewURL(e.target.value)}
              placeholder={t("dialogs.editTracker.newUrlPlaceholder", { tracker })}
              className="font-mono text-sm"
            />
            <p className="text-xs text-muted-foreground">
              {t("dialogs.editTracker.newUrlHelp")}
            </p>
          </div>
          {isFilteredMode && (
            <div className="bg-muted p-3 rounded-md">
              <p className="text-sm text-muted-foreground">
                <strong>{t("dialogs.editTracker.noteLabel")}</strong> {t("dialogs.editTracker.filteredModeNote")}
              </p>
            </div>
          )}
          {hasHttpUrls && onConvertHttpToHttps && (
            <div className="pt-2 border-t">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={onConvertHttpToHttps}
                disabled={isConverting || loadingURLs || isPending}
                className="w-full"
              >
                {isConverting ? t("dialogs.editTracker.converting") : t("dialogs.editTracker.convertHttpToHttps")}
              </Button>
              <p className="text-xs text-muted-foreground mt-1">
                {t("dialogs.editTracker.convertHttpToHttpsHelp")}
              </p>
            </div>
          )}
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={handleCancel}>{t("dialogs.editTracker.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleConfirm}
            disabled={!oldURL.trim() || !newURL.trim() || oldURL === newURL || isPending || loadingURLs || isConverting}
          >
            {t("dialogs.editTracker.confirm")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
})

const SPEED_DEFAULT_LIMIT = 0

// Helper function to safely get numeric values with fallback
const safeNumber = (value: number | undefined, fallback: number) =>
  typeof value === "number" ? value : fallback

interface ShareLimitDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  hashCount: number
  torrents?: TorrentLimitSnapshot[]
  onConfirm: (ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number, shareLimitAction?: string, shareLimitsMode?: string) => void
  isPending?: boolean
  supportsShareLimitsAction?: boolean
  supportsShareLimitsMode?: boolean
}

// Share limit mode: matches qBittorrent sentinel values
type ShareLimitMode = "global" | "unlimited" | "custom"

interface ShareLimitFieldState {
  mode: ShareLimitMode
  customValue: number
  isMixed: boolean // True when selection has different values
}

// Convert a raw limit value to mode + custom value
function valueToFieldState(value: number | undefined, defaultCustom: number): Omit<ShareLimitFieldState, "isMixed"> {
  if (value === undefined || value === LIMIT_USE_GLOBAL) {
    return { mode: "global", customValue: defaultCustom }
  }
  if (value === LIMIT_UNLIMITED) {
    return { mode: "unlimited", customValue: defaultCustom }
  }
  return { mode: "custom", customValue: value }
}

function buildShareLimitFieldStates(torrents?: TorrentLimitSnapshot[]): {
  ratio: ShareLimitFieldState
  seedTime: ShareLimitFieldState
  inactiveTime: ShareLimitFieldState
} {
  const ratioCheck = checkFieldConsistency(torrents, t => t.ratio_limit)
  const seedTimeCheck = checkFieldConsistency(torrents, t => t.seeding_time_limit)
  const inactiveTimeCheck = checkFieldConsistency(torrents, t => t.inactive_seeding_time_limit)

  return {
    ratio: {
      ...valueToFieldState(ratioCheck.commonValue, 1.0),
      isMixed: ratioCheck.isMixed,
    },
    seedTime: {
      ...valueToFieldState(seedTimeCheck.commonValue, 1440),
      isMixed: seedTimeCheck.isMixed,
    },
    inactiveTime: {
      ...valueToFieldState(inactiveTimeCheck.commonValue, 10080),
      isMixed: inactiveTimeCheck.isMixed,
    },
  }
}

// Convert mode + custom value to API value
function fieldStateToValue(mode: ShareLimitMode, customValue: number, isRatio: boolean): number {
  switch (mode) {
    case "global":
      return LIMIT_USE_GLOBAL
    case "unlimited":
      return LIMIT_UNLIMITED
    case "custom":
      // Normalize ratio to 2 decimal places
      return isRatio ? Math.round(customValue * 100) / 100 : customValue
  }
}

export const ShareLimitDialog = memo(function ShareLimitDialog({
  open,
  onOpenChange,
  hashCount,
  torrents,
  onConfirm,
  isPending = false,
  supportsShareLimitsAction = false,
  supportsShareLimitsMode = false,
}: ShareLimitDialogProps) {
  const { t } = useTranslation("torrents")
  const [ratioMode, setRatioMode] = useState<ShareLimitMode>("global")
  const [ratioCustom, setRatioCustom] = useState(1.0)
  const [ratioMixed, setRatioMixed] = useState(false)
  const [ratioTouched, setRatioTouched] = useState(false) // User explicitly changed this field

  const [seedTimeMode, setSeedTimeMode] = useState<ShareLimitMode>("global")
  const [seedTimeCustom, setSeedTimeCustom] = useState(1440)
  const [seedTimeMixed, setSeedTimeMixed] = useState(false)
  const [seedTimeTouched, setSeedTimeTouched] = useState(false)

  const [inactiveTimeMode, setInactiveTimeMode] = useState<ShareLimitMode>("global")
  const [inactiveTimeCustom, setInactiveTimeCustom] = useState(10080)
  const [inactiveTimeMixed, setInactiveTimeMixed] = useState(false)
  const [inactiveTimeTouched, setInactiveTimeTouched] = useState(false)

  const [shareLimitAction, setShareLimitAction] = useState("default")
  const [shareLimitActionMixed, setShareLimitActionMixed] = useState(false)
  const [shareLimitActionTouched, setShareLimitActionTouched] = useState(false)

  const [shareLimitsMode, setShareLimitsMode] = useState("default")
  const [shareLimitsModeMixed, setShareLimitsModeMixed] = useState(false)
  const [shareLimitsModeTouched, setShareLimitsModeTouched] = useState(false)

  const wasOpen = useRef(false)
  const shareLimitsEdited = useRef(false)

  useEffect(() => {
    if (open && !wasOpen.current) {
      const states = buildShareLimitFieldStates(torrents)

      setRatioMode(states.ratio.isMixed ? "global" : states.ratio.mode)
      setRatioCustom(states.ratio.customValue)
      setRatioMixed(states.ratio.isMixed)
      setRatioTouched(false)

      setSeedTimeMode(states.seedTime.isMixed ? "global" : states.seedTime.mode)
      setSeedTimeCustom(states.seedTime.customValue)
      setSeedTimeMixed(states.seedTime.isMixed)
      setSeedTimeTouched(false)

      setInactiveTimeMode(states.inactiveTime.isMixed ? "global" : states.inactiveTime.mode)
      setInactiveTimeCustom(states.inactiveTime.customValue)
      setInactiveTimeMixed(states.inactiveTime.isMixed)
      setInactiveTimeTouched(false)

      setShareLimitActionTouched(false)
      setShareLimitsModeTouched(false)
    }
    if (open && supportsShareLimitsAction && !shareLimitsEdited.current) {
      const a = shareLimitEnumFieldFromTorrents(torrents, t => t.share_limit_action)
      setShareLimitAction(a.value)
      setShareLimitActionMixed(a.isMixed)
      if (supportsShareLimitsMode) {
        const m = shareLimitEnumFieldFromTorrents(torrents, t => t.share_limits_mode)
        setShareLimitsMode(m.value)
        setShareLimitsModeMixed(m.isMixed)
      } else {
        setShareLimitsMode("default")
        setShareLimitsModeMixed(false)
      }
    }
    if (open && !supportsShareLimitsAction) {
      setShareLimitAction("default")
      setShareLimitActionMixed(false)
      setShareLimitsMode("default")
      setShareLimitsModeMixed(false)
    }
    if (!open) {
      shareLimitsEdited.current = false
    }
    wasOpen.current = open
  }, [open, torrents, supportsShareLimitsAction, supportsShareLimitsMode])

  const hasUnresolvedMixed = (ratioMixed && !ratioTouched) ||
    (seedTimeMixed && !seedTimeTouched) ||
    (inactiveTimeMixed && !inactiveTimeTouched) ||
    (supportsShareLimitsAction && (shareLimitActionMixed && !shareLimitActionTouched)) ||
    (supportsShareLimitsMode && (shareLimitsModeMixed && !shareLimitsModeTouched))

  const handleConfirm = useCallback((): void => {
    onConfirm(
      fieldStateToValue(ratioMode, ratioCustom, true),
      fieldStateToValue(seedTimeMode, seedTimeCustom, false),
      fieldStateToValue(inactiveTimeMode, inactiveTimeCustom, false),
      shareLimitAction !== "default" ? shareLimitAction : undefined,
      supportsShareLimitsMode && shareLimitsMode !== "default" ? shareLimitsMode : undefined
    )
    setRatioMode("global")
    setRatioCustom(1.0)
    setRatioMixed(false)
    setRatioTouched(false)
    setSeedTimeMode("global")
    setSeedTimeCustom(1440)
    setSeedTimeMixed(false)
    setSeedTimeTouched(false)
    setInactiveTimeMode("global")
    setInactiveTimeCustom(10080)
    setInactiveTimeMixed(false)
    setInactiveTimeTouched(false)
    setShareLimitAction("default")
    setShareLimitActionMixed(false)
    setShareLimitActionTouched(false)
    setShareLimitsMode("default")
    setShareLimitsModeMixed(false)
    setShareLimitsModeTouched(false)
    onOpenChange(false)
  }, [
    onConfirm,
    onOpenChange,
    ratioMode,
    ratioCustom,
    seedTimeMode,
    seedTimeCustom,
    inactiveTimeMode,
    inactiveTimeCustom,
    shareLimitAction,
    shareLimitsMode,
    supportsShareLimitsMode,
  ])

  const handleCancel = useCallback((): void => {
    setRatioMode("global")
    setRatioCustom(1.0)
    setRatioMixed(false)
    setRatioTouched(false)
    setSeedTimeMode("global")
    setSeedTimeCustom(1440)
    setSeedTimeMixed(false)
    setSeedTimeTouched(false)
    setInactiveTimeMode("global")
    setInactiveTimeCustom(10080)
    setInactiveTimeMixed(false)
    setInactiveTimeTouched(false)
    setShareLimitAction("default")
    setShareLimitActionMixed(false)
    setShareLimitActionTouched(false)
    setShareLimitsMode("default")
    setShareLimitsModeMixed(false)
    setShareLimitsModeTouched(false)
    onOpenChange(false)
  }, [onOpenChange])

  const setAllGlobal = useCallback(() => {
    setRatioMode("global")
    setRatioTouched(true)
    setSeedTimeMode("global")
    setSeedTimeTouched(true)
    setInactiveTimeMode("global")
    setInactiveTimeTouched(true)
    if (supportsShareLimitsAction) {
      setShareLimitAction("default")
      setShareLimitActionTouched(true)
      shareLimitsEdited.current = true
    }
    if (supportsShareLimitsMode) {
      setShareLimitsMode("default")
      setShareLimitsModeTouched(true)
      shareLimitsEdited.current = true
    }
  }, [supportsShareLimitsAction, supportsShareLimitsMode])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t("dialogs.shareLimit.title", { count: hashCount })}</DialogTitle>
          <DialogDescription>
            {t("dialogs.shareLimit.description")}
          </DialogDescription>
        </DialogHeader>
        <div className="py-2 space-y-4">
          {/* Quick action: Set all to global */}
          <div className="flex justify-end">
            <Button variant="outline" size="sm" onClick={setAllGlobal}>
              {t("dialogs.shareLimit.setAllGlobal")}
            </Button>
          </div>

          {/* Ratio limit */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label className="text-sm font-medium">{t("dialogs.shareLimit.ratioLimit")}</Label>
              {ratioMixed && !ratioTouched && (
                <span className="text-xs text-yellow-600">{t("dialogs.shareLimit.selectValue")}</span>
              )}
              {ratioMixed && ratioTouched && (
                <span className="text-xs text-muted-foreground">{t("dialogs.shareLimit.wasMixed")}</span>
              )}
            </div>
            <div className="flex gap-2">
              <Select
                value={ratioMode}
                onValueChange={(value: ShareLimitMode) => {
                  setRatioMode(value)
                  setRatioTouched(true)
                }}
              >
                <SelectTrigger className="w-[140px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="global">{t("dialogs.shareLimit.useGlobal")}</SelectItem>
                  <SelectItem value="unlimited">{t("dialogs.shareLimit.unlimited")}</SelectItem>
                  <SelectItem value="custom">{t("dialogs.shareLimit.custom")}</SelectItem>
                </SelectContent>
              </Select>
              {ratioMode === "custom" && (
                <Input
                  type="number"
                  min="0"
                  step="0.01"
                  className="flex-1"
                  value={ratioCustom}
                  onChange={(e) => {
                    const val = parseFloat(e.target.value)
                    if (Number.isFinite(val)) setRatioCustom(val)
                  }}
                  placeholder={t("dialogs.shareLimit.ratioPlaceholder")}
                />
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              {ratioMode === "global" ? t("dialogs.shareLimit.ratioGlobalHelp") : ratioMode === "unlimited" ? t("dialogs.shareLimit.ratioUnlimitedHelp") : t("dialogs.shareLimit.ratioCustomHelp")}
            </p>
          </div>

          {/* Seeding time limit */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label className="text-sm font-medium">{t("dialogs.shareLimit.seedingTimeLimit")}</Label>
              {seedTimeMixed && !seedTimeTouched && (
                <span className="text-xs text-yellow-600">{t("dialogs.shareLimit.selectValue")}</span>
              )}
              {seedTimeMixed && seedTimeTouched && (
                <span className="text-xs text-muted-foreground">{t("dialogs.shareLimit.wasMixed")}</span>
              )}
            </div>
            <div className="flex gap-2">
              <Select
                value={seedTimeMode}
                onValueChange={(value: ShareLimitMode) => {
                  setSeedTimeMode(value)
                  setSeedTimeTouched(true)
                }}
              >
                <SelectTrigger className="w-[140px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="global">{t("dialogs.shareLimit.useGlobal")}</SelectItem>
                  <SelectItem value="unlimited">{t("dialogs.shareLimit.unlimited")}</SelectItem>
                  <SelectItem value="custom">{t("dialogs.shareLimit.custom")}</SelectItem>
                </SelectContent>
              </Select>
              {seedTimeMode === "custom" && (
                <Input
                  type="number"
                  min="0"
                  className="flex-1"
                  value={seedTimeCustom}
                  onChange={(e) => {
                    const val = parseInt(e.target.value, 10)
                    if (Number.isFinite(val)) setSeedTimeCustom(val)
                  }}
                  placeholder={t("dialogs.shareLimit.seedTimePlaceholder")}
                />
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              {seedTimeMode === "global" ? t("dialogs.shareLimit.seedTimeGlobalHelp") : seedTimeMode === "unlimited" ? t("dialogs.shareLimit.seedTimeUnlimitedHelp") : t("dialogs.shareLimit.seedTimeCustomHelp")}
            </p>
          </div>

          {/* Inactive seeding time limit */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label className="text-sm font-medium">{t("dialogs.shareLimit.inactiveSeedingLimit")}</Label>
              {inactiveTimeMixed && !inactiveTimeTouched && (
                <span className="text-xs text-yellow-600">{t("dialogs.shareLimit.selectValue")}</span>
              )}
              {inactiveTimeMixed && inactiveTimeTouched && (
                <span className="text-xs text-muted-foreground">{t("dialogs.shareLimit.wasMixed")}</span>
              )}
            </div>
            <div className="flex gap-2">
              <Select
                value={inactiveTimeMode}
                onValueChange={(value: ShareLimitMode) => {
                  setInactiveTimeMode(value)
                  setInactiveTimeTouched(true)
                }}
              >
                <SelectTrigger className="w-[140px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="global">{t("dialogs.shareLimit.useGlobal")}</SelectItem>
                  <SelectItem value="unlimited">{t("dialogs.shareLimit.unlimited")}</SelectItem>
                  <SelectItem value="custom">{t("dialogs.shareLimit.custom")}</SelectItem>
                </SelectContent>
              </Select>
              {inactiveTimeMode === "custom" && (
                <Input
                  type="number"
                  min="0"
                  className="flex-1"
                  value={inactiveTimeCustom}
                  onChange={(e) => {
                    const val = parseInt(e.target.value, 10)
                    if (Number.isFinite(val)) setInactiveTimeCustom(val)
                  }}
                  placeholder={t("dialogs.shareLimit.inactivePlaceholder")}
                />
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              {inactiveTimeMode === "global" ? t("dialogs.shareLimit.inactiveGlobalHelp") : inactiveTimeMode === "unlimited" ? t("dialogs.shareLimit.inactiveUnlimitedHelp") : t("dialogs.shareLimit.inactiveCustomHelp")}
            </p>
          </div>

          {supportsShareLimitsAction && (
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label className="text-sm font-medium">{t("shareLimits.whenLimitsReached")}</Label>
                {shareLimitActionMixed && !shareLimitActionTouched && (
                  <span className="text-xs text-yellow-600">{t("shareLimits.selectValue")}</span>
                )}
                {shareLimitActionMixed && shareLimitActionTouched && (
                  <span className="text-xs text-muted-foreground">{t("shareLimits.wasMixed")}</span>
                )}
              </div>
              <Select
                value={shareLimitAction}
                onValueChange={(v: string) => {
                  shareLimitsEdited.current = true
                  setShareLimitAction(v)
                  setShareLimitActionTouched(true)
                }}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="default">{t("shareLimits.defaultUseGlobal")}</SelectItem>
                  <SelectItem value="Stop">{t("shareLimits.stopTorrent")}</SelectItem>
                  <SelectItem value="Remove">{t("shareLimits.removeTorrent")}</SelectItem>
                  <SelectItem value="RemoveWithContent">{t("shareLimits.removeWithContent")}</SelectItem>
                  <SelectItem value="EnableSuperSeeding">{t("shareLimits.enableSuperSeeding")}</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                {t("shareLimits.actionWhenReached")}
              </p>
            </div>
          )}

          {supportsShareLimitsMode && (
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label className="text-sm font-medium">{t("shareLimits.limitsMatchingMode")}</Label>
                {shareLimitsModeMixed && !shareLimitsModeTouched && (
                  <span className="text-xs text-yellow-600">{t("shareLimits.selectValue")}</span>
                )}
                {shareLimitsModeMixed && shareLimitsModeTouched && (
                  <span className="text-xs text-muted-foreground">{t("shareLimits.wasMixed")}</span>
                )}
              </div>
              <Select
                value={shareLimitsMode}
                onValueChange={(v: string) => {
                  shareLimitsEdited.current = true
                  setShareLimitsMode(v)
                  setShareLimitsModeTouched(true)
                }}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="default">{t("shareLimits.defaultUseGlobal")}</SelectItem>
                  <SelectItem value="MatchAny">{t("shareLimits.matchAnyLimit")}</SelectItem>
                  <SelectItem value="MatchAll">{t("shareLimits.matchAllLimits")}</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                {t("shareLimits.matchingModeHelp")}
              </p>
            </div>
          )}
        </div>
        <DialogFooter className="flex-col sm:flex-row gap-2">
          {hasUnresolvedMixed && (
            <p className="text-xs text-yellow-600 text-left sm:flex-1">
              {t("dialogs.shareLimit.unresolvedMixed")}
            </p>
          )}
          <div className="flex gap-2">
            <Button variant="outline" onClick={handleCancel}>
              {t("dialogs.shareLimit.cancel")}
            </Button>
            <Button
              onClick={handleConfirm}
              disabled={isPending || hasUnresolvedMixed}
            >
              {isPending ? t("dialogs.shareLimit.setting") : t("dialogs.shareLimit.confirm")}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface SpeedLimitsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  hashCount: number
  torrents?: TorrentLimitSnapshot[]
  onConfirm: (uploadLimit: number, downloadLimit: number) => void
  isPending?: boolean
}

interface SpeedLimitFormState {
  uploadEnabled: boolean
  uploadLimit: number
  downloadEnabled: boolean
  downloadLimit: number
}

export const buildSpeedLimitInitialState = (torrents?: TorrentLimitSnapshot[]): SpeedLimitFormState => {
  const base: SpeedLimitFormState = {
    uploadEnabled: false,
    uploadLimit: SPEED_DEFAULT_LIMIT,
    downloadEnabled: false,
    downloadLimit: SPEED_DEFAULT_LIMIT,
  }

  if (!torrents || torrents.length === 0) {
    return base
  }

  const uploadValues = torrents.map((torrent) => safeNumber(torrent.up_limit, 0))
  const downloadValues = torrents.map((torrent) => safeNumber(torrent.dl_limit, 0))

  const uploadsMatch = uploadValues.every((value) => value === uploadValues[0])
  const downloadsMatch = downloadValues.every((value) => value === downloadValues[0])

  const firstUpload = uploadValues[0]
  const firstDownload = downloadValues[0]

  return {
    ...base,
    uploadEnabled: uploadsMatch && firstUpload > 0,
    uploadLimit: uploadsMatch && firstUpload > 0 ? Math.round(firstUpload / 1024) : base.uploadLimit,
    downloadEnabled: downloadsMatch && firstDownload > 0,
    downloadLimit: downloadsMatch && firstDownload > 0 ? Math.round(firstDownload / 1024) : base.downloadLimit,
  }
}

export const SpeedLimitsDialog = memo(function SpeedLimitsDialog({
  open,
  onOpenChange,
  hashCount,
  torrents,
  onConfirm,
  isPending = false,
}: SpeedLimitsDialogProps) {
  const { t } = useTranslation("torrents")
  const [uploadEnabled, setUploadEnabled] = useState(false)
  const [uploadLimit, setUploadLimit] = useState(SPEED_DEFAULT_LIMIT)
  const [downloadEnabled, setDownloadEnabled] = useState(false)
  const [downloadLimit, setDownloadLimit] = useState(SPEED_DEFAULT_LIMIT)
  const wasOpen = useRef(false)

  const speedInitialState = useMemo(() => buildSpeedLimitInitialState(torrents), [torrents])

  // Reset form when dialog opens with torrent values
  useEffect(() => {
    if (open && !wasOpen.current) {
      setUploadEnabled(speedInitialState.uploadEnabled)
      setUploadLimit(speedInitialState.uploadLimit)
      setDownloadEnabled(speedInitialState.downloadEnabled)
      setDownloadLimit(speedInitialState.downloadLimit)
    }
    wasOpen.current = open
  }, [open, speedInitialState])

  const handleConfirm = useCallback((): void => {
    onConfirm(
      uploadEnabled ? uploadLimit : 0,  // 0 means use global limit
      downloadEnabled ? downloadLimit : 0  // 0 means use global limit
    )
    // Reset form
    setUploadEnabled(false)
    setUploadLimit(SPEED_DEFAULT_LIMIT)
    setDownloadEnabled(false)
    setDownloadLimit(SPEED_DEFAULT_LIMIT)
  }, [onConfirm, uploadEnabled, uploadLimit, downloadEnabled, downloadLimit])

  const handleCancel = useCallback((): void => {
    setUploadEnabled(false)
    setUploadLimit(SPEED_DEFAULT_LIMIT)
    setDownloadEnabled(false)
    setDownloadLimit(SPEED_DEFAULT_LIMIT)
    onOpenChange(false)
  }, [onOpenChange])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t("dialogs.speedLimits.title", { count: hashCount })}</DialogTitle>
          <DialogDescription>
            {t("dialogs.speedLimits.description")}
          </DialogDescription>
        </DialogHeader>
        <div className="py-2 space-y-4">
          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="uploadEnabled"
                checked={uploadEnabled}
                onCheckedChange={setUploadEnabled}
              />
              <Label htmlFor="uploadEnabled">{t("dialogs.speedLimits.uploadLimit")}</Label>
            </div>
            <Input
              type="number"
              min="0"
              value={uploadLimit}
              disabled={!uploadEnabled}
              onChange={(e) => setUploadLimit(parseInt(e.target.value) || 0)}
              placeholder="0"
            />
          </div>

          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="downloadEnabled"
                checked={downloadEnabled}
                onCheckedChange={setDownloadEnabled}
              />
              <Label htmlFor="downloadEnabled">{t("dialogs.speedLimits.downloadLimit")}</Label>
            </div>
            <Input
              type="number"
              min="0"
              value={downloadLimit}
              disabled={!downloadEnabled}
              onChange={(e) => setDownloadLimit(parseInt(e.target.value) || 0)}
              placeholder="0"
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleCancel}>
            {t("dialogs.speedLimits.cancel")}
          </Button>
          <Button
            onClick={handleConfirm}
            disabled={isPending}
          >
            {isPending ? t("dialogs.speedLimits.setting") : t("dialogs.speedLimits.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface TmmConfirmDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  count: number
  enable: boolean
  onConfirm: () => void
  isPending?: boolean
}

export const TmmConfirmDialog = memo(function TmmConfirmDialog({
  open,
  onOpenChange,
  count,
  enable,
  onConfirm,
  isPending = false,
}: TmmConfirmDialogProps) {
  const { t } = useTranslation("torrents")
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <AlertTriangle className="h-5 w-5 text-warning" />
            {enable ? t("dialogs.tmm.enableTitle", { count }) : t("dialogs.tmm.disableTitle", { count })}
          </DialogTitle>
          <DialogDescription>
            {enable ? t("dialogs.tmm.enableDescription") : t("dialogs.tmm.disableDescription")}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("dialogs.tmm.cancel")}
          </Button>
          <Button className="bg-destructive text-destructive-foreground hover:bg-destructive/90" onClick={onConfirm} disabled={isPending}>
            {enable ? t("dialogs.tmm.enable") : t("dialogs.tmm.disable")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})

interface LocationWarningDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  count: number
  onConfirm: () => void
  isPending?: boolean
}

export const LocationWarningDialog = memo(function LocationWarningDialog({
  open,
  onOpenChange,
  count,
  onConfirm,
  isPending = false,
}: LocationWarningDialogProps) {
  const { t } = useTranslation("torrents")
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <AlertTriangle className="h-5 w-5 text-warning" />
            {t("dialogs.locationWarning.title", { count })}
          </DialogTitle>
          <DialogDescription>
            {t("dialogs.locationWarning.description")}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("dialogs.tmm.cancel")}
          </Button>
          <Button className="bg-destructive text-destructive-foreground hover:bg-destructive/90" onClick={onConfirm} disabled={isPending}>
            {t("dialogs.locationWarning.continue")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
})
