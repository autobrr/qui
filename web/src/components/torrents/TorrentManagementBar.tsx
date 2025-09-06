/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
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
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import { getCommonCategory, getCommonTags } from "@/lib/torrent-utils"
import type { Torrent } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ArrowDown, ArrowUp, ChevronsDown, ChevronsUp, Folder, List, LoaderCircle, Pause, Play, Radio, Settings2, Tag, Trash2 } from "lucide-react"
import type { ChangeEvent } from "react"
import { memo, useCallback, useState } from "react"
import { toast } from "sonner"
import { AddTagsDialog, SetCategoryDialog, SetTagsDialog } from "./TorrentDialogs"

type BulkActionVariables = {
  action: "pause" | "resume" | "delete" | "recheck" | "reannounce" | "increasePriority" | "decreasePriority" | "topPriority" | "bottomPriority" | "addTags" | "removeTags" | "setTags" | "setCategory" | "toggleAutoTMM" | "setShareLimit" | "setUploadLimit" | "setDownloadLimit"
  deleteFiles?: boolean
  tags?: string
  category?: string
  enable?: boolean
  ratioLimit?: number
  seedingTimeLimit?: number
  inactiveSeedingTimeLimit?: number
  uploadLimit?: number
  downloadLimit?: number
}

interface TorrentManagementBarProps {
  instanceId?: number
  selectedHashes?: string[]
  selectedTorrents?: Torrent[]
  isAllSelected?: boolean
  totalSelectionCount?: number
  filters?: {
    status: string[]
    categories: string[]
    tags: string[]
    trackers: string[]
  }
  search?: string
  excludeHashes?: string[]
  onComplete?: () => void
}

export const TorrentManagementBar = memo(function TorrentManagementBar({
  instanceId,
  selectedHashes = [],
  selectedTorrents = [],
  isAllSelected = false,
  totalSelectionCount = 0,
  filters,
  search,
  excludeHashes = [],
  onComplete,
}: TorrentManagementBarProps) {
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [deleteFiles, setDeleteFiles] = useState(false)
  const [showAddTagsDialog, setShowAddTagsDialog] = useState(false)
  const [showTagsDialog, setShowTagsDialog] = useState(false)
  const [showCategoryDialog, setShowCategoryDialog] = useState(false)
  const [showRecheckDialog, setShowRecheckDialog] = useState(false)
  const [showReannounceDialog, setShowReannounceDialog] = useState(false)
  const queryClient = useQueryClient()

  // Fetch available tags
  const { data: availableTags = [] } = useQuery({
    queryKey: ["tags", instanceId],
    queryFn: () => instanceId ? api.getTags(instanceId) : Promise.resolve([]),
    enabled: !!instanceId,
    staleTime: 60000,
  })

  // Fetch available categories
  const { data: availableCategories = {} } = useQuery({
    queryKey: ["categories", instanceId],
    queryFn: () => instanceId ? api.getCategories(instanceId) : Promise.resolve({}),
    enabled: !!instanceId,
    staleTime: 60000,
  })

  const mutation = useMutation({
    mutationFn: (data: {
      action: "pause" | "resume" | "delete" | "recheck" | "reannounce" | "increasePriority" | "decreasePriority" | "topPriority" | "bottomPriority" | "addTags" | "removeTags" | "setTags" | "setCategory" | "toggleAutoTMM" | "setShareLimit" | "setUploadLimit" | "setDownloadLimit"
      deleteFiles?: boolean
      tags?: string
      category?: string
      enable?: boolean
      ratioLimit?: number
      seedingTimeLimit?: number
      inactiveSeedingTimeLimit?: number
      uploadLimit?: number
      downloadLimit?: number
    }) => {
      if (!instanceId) throw new Error("No instance selected")
      return api.bulkAction(instanceId, {
        hashes: isAllSelected ? [] : selectedHashes,
        action: data.action,
        deleteFiles: data.deleteFiles,
        tags: data.tags,
        category: data.category,
        enable: data.enable,
        selectAll: isAllSelected,
        filters: isAllSelected ? filters : undefined,
        search: isAllSelected ? search : undefined,
        excludeHashes: isAllSelected ? excludeHashes : undefined,
        ratioLimit: data.ratioLimit,
        seedingTimeLimit: data.seedingTimeLimit,
        inactiveSeedingTimeLimit: data.inactiveSeedingTimeLimit,
        uploadLimit: data.uploadLimit,
        downloadLimit: data.downloadLimit,
      })
    },
    onSuccess: async (_: unknown, variables: BulkActionVariables) => {
      // For delete operations, force immediate refetch
      if (variables.action === "delete") {
        queryClient.removeQueries({
          queryKey: ["torrents-list", instanceId],
          exact: false,
        })
        queryClient.removeQueries({
          queryKey: ["torrent-counts", instanceId],
          exact: false,
        })

        await queryClient.refetchQueries({
          queryKey: ["torrents-list", instanceId],
          exact: false,
        })
        await queryClient.refetchQueries({
          queryKey: ["torrent-counts", instanceId],
          exact: false,
        })
        onComplete?.()
      } else {
        const delay = variables.action === "resume" ? 2000 : 1000
        setTimeout(() => {
          queryClient.invalidateQueries({
            queryKey: ["torrents-list", instanceId],
            exact: false,
          })
          queryClient.invalidateQueries({
            queryKey: ["torrent-counts", instanceId],
            exact: false,
          })
        }, delay)
        onComplete?.()
      }

      // Show success toast
      const count = totalSelectionCount || selectedHashes.length
      const torrentText = count === 1 ? "torrent" : "torrents"

      switch (variables.action) {
        case "resume":
          toast.success(`Resumed ${count} ${torrentText}`)
          break
        case "pause":
          toast.success(`Paused ${count} ${torrentText}`)
          break
        case "delete":
          toast.success(`Deleted ${count} ${torrentText}${variables.deleteFiles ? " and files" : ""}`)
          break
        case "recheck":
          toast.success(`Started recheck for ${count} ${torrentText}`)
          break
        case "reannounce":
          toast.success(`Reannounced ${count} ${torrentText}`)
          break
        case "increasePriority":
          toast.success(`Increased priority for ${count} ${torrentText}`)
          break
        case "decreasePriority":
          toast.success(`Decreased priority for ${count} ${torrentText}`)
          break
        case "topPriority":
          toast.success(`Set ${count} ${torrentText} to top priority`)
          break
        case "bottomPriority":
          toast.success(`Set ${count} ${torrentText} to bottom priority`)
          break
        case "addTags":
          toast.success(`Added tags to ${count} ${torrentText}`)
          break
        case "removeTags":
          toast.success(`Removed tags from ${count} ${torrentText}`)
          break
        case "setTags":
          toast.success(`Replaced tags for ${count} ${torrentText}`)
          break
        case "setCategory":
          toast.success(`Set category for ${count} ${torrentText}`)
          break
        case "toggleAutoTMM":
          toast.success(`${variables.enable ? "Enabled" : "Disabled"} Auto TMM for ${count} ${torrentText}`)
          break
        case "setShareLimit":
          toast.success(`Set share limits for ${count} ${torrentText}`)
          break
        case "setUploadLimit":
          toast.success(`Set upload limit for ${count} ${torrentText}`)
          break
        case "setDownloadLimit":
          toast.success(`Set download limit for ${count} ${torrentText}`)
          break
      }
    },
    onError: (error: Error, variables: BulkActionVariables) => {
      const count = totalSelectionCount || selectedHashes.length
      const torrentText = count === 1 ? "torrent" : "torrents"
      const actionText = variables.action === "recheck" ? "recheck" : variables.action

      toast.error(`Failed to ${actionText} ${count} ${torrentText}`, {
        description: error.message || "An unexpected error occurred",
      })
    },
  })

  const handleDelete = useCallback(async () => {
    await mutation.mutateAsync({ action: "delete", deleteFiles })
    setShowDeleteDialog(false)
    setDeleteFiles(false)
  }, [mutation, deleteFiles])

  const handleAddTags = useCallback(async (tags: string[]) => {
    await mutation.mutateAsync({ action: "addTags", tags: tags.join(",") })
    setShowAddTagsDialog(false)
  }, [mutation])

  const handleSetTags = useCallback(async (tags: string[]) => {
    try {
      await mutation.mutateAsync({ action: "setTags", tags: tags.join(",") })
    } catch (error: unknown) {
      const err = error instanceof Error ? error : new Error("Unknown error occurred")
      if (err.message?.includes("requires qBittorrent")) {
        await mutation.mutateAsync({ action: "addTags", tags: tags.join(",") })
      } else {
        throw err
      }
    }
    setShowTagsDialog(false)
  }, [mutation])

  const handleSetCategory = useCallback(async (category: string) => {
    await mutation.mutateAsync({ action: "setCategory", category })
    setShowCategoryDialog(false)
  }, [mutation])


  const handleRecheck = useCallback(async () => {
    await mutation.mutateAsync({ action: "recheck" })
    setShowRecheckDialog(false)
  }, [mutation])

  const handleReannounce = useCallback(async () => {
    await mutation.mutateAsync({ action: "reannounce" })
    setShowReannounceDialog(false)
  }, [mutation])

  const handleRecheckClick = useCallback(() => {
    const count = totalSelectionCount || selectedHashes.length
    if (count > 1) {
      setShowRecheckDialog(true)
    } else {
      mutation.mutate({ action: "recheck" })
    }
  }, [totalSelectionCount, selectedHashes.length, mutation])

  const handleReannounceClick = useCallback(() => {
    const count = totalSelectionCount || selectedHashes.length
    if (count > 1) {
      setShowReannounceDialog(true)
    } else {
      mutation.mutate({ action: "reannounce" })
    }
  }, [totalSelectionCount, selectedHashes.length, mutation])

  const handleQueueAction = useCallback((action: "topPriority" | "increasePriority" | "decreasePriority" | "bottomPriority") => {
    mutation.mutate({ action })
  }, [mutation])

  const selectionCount = totalSelectionCount || selectedHashes.length
  const hasSelection = selectionCount > 0 || isAllSelected
  const isDisabled = !instanceId || !hasSelection

  return (
    <>
      <div className="flex items-center h-9 dark:bg-input/30 border border-input rounded-md px-3 py-2 animate-in slide-in-from-top-2 duration-200 gap-3 shadow-xs">
        <div className="flex items-center gap-3 flex-shrink-0 min-w-0">
          <span className="text-xs text-muted-foreground whitespace-nowrap min-w-[3ch] text-center">
            {selectionCount}
          </span>
        </div>

        <div className="flex items-center gap-1">
          {/* Primary Actions */}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => mutation.mutate({ action: "resume" })}
                disabled={mutation.isPending || isDisabled}
              >
                <Play className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Resume</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => mutation.mutate({ action: "pause" })}
                disabled={mutation.isPending || isDisabled}
              >
                <Pause className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Pause</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={handleRecheckClick}
                disabled={mutation.isPending || isDisabled}
              >
                <LoaderCircle className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Force Recheck</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={handleReannounceClick}
                disabled={mutation.isPending || isDisabled}
              >
                <Radio className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Reannounce</TooltipContent>
          </Tooltip>

          {/* Tag Actions */}
          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger asChild>
                <DropdownMenuTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={mutation.isPending || isDisabled}
                  >
                    <Tag className="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
              </TooltipTrigger>
              <TooltipContent>Tag Actions</TooltipContent>
            </Tooltip>
            <DropdownMenuContent align="center">
              <DropdownMenuItem
                onClick={() => setShowAddTagsDialog(true)}
                disabled={mutation.isPending || isDisabled}
              >
                <Tag className="h-4 w-4 mr-2" />
                Add Tags {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => setShowTagsDialog(true)}
                disabled={mutation.isPending || isDisabled}
              >
                <Tag className="h-4 w-4 mr-2" />
                Replace Tags {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setShowCategoryDialog(true)}
                disabled={mutation.isPending || isDisabled}
              >
                <Folder className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Set Category</TooltipContent>
          </Tooltip>

          {/* Queue Priority */}
          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger asChild>
                <DropdownMenuTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={mutation.isPending || isDisabled}
                  >
                    <List className="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
              </TooltipTrigger>
              <TooltipContent>Queue Priority</TooltipContent>
            </Tooltip>
            <DropdownMenuContent align="center">
              <DropdownMenuItem
                onClick={() => handleQueueAction("topPriority")}
                disabled={mutation.isPending || isDisabled}
              >
                <ChevronsUp className="h-4 w-4 mr-2" />
                Top Priority {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => handleQueueAction("increasePriority")}
                disabled={mutation.isPending || isDisabled}
              >
                <ArrowUp className="h-4 w-4 mr-2" />
                Increase Priority {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => handleQueueAction("decreasePriority")}
                disabled={mutation.isPending || isDisabled}
              >
                <ArrowDown className="h-4 w-4 mr-2" />
                Decrease Priority {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => handleQueueAction("bottomPriority")}
                disabled={mutation.isPending || isDisabled}
              >
                <ChevronsDown className="h-4 w-4 mr-2" />
                Bottom Priority {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>

          {/* TMM Toggle */}
          {(() => {
            const tmmStates = selectedTorrents?.map(t => t.auto_tmm) ?? []
            const allEnabled = tmmStates.length > 0 && tmmStates.every(state => state === true)
            const mixed = tmmStates.length > 0 && !tmmStates.every(state => state === allEnabled)

            return (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => mutation.mutate({ action: "toggleAutoTMM", enable: !allEnabled })}
                    disabled={mutation.isPending || isDisabled}
                  >
                    <Settings2 className="h-4 w-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  {mixed ? "TMM (Mixed)" : allEnabled ? "Disable TMM" : "Enable TMM"}
                </TooltipContent>
              </Tooltip>
            )
          })()}


          {/* Delete Action */}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setShowDeleteDialog(true)}
                disabled={mutation.isPending || isDisabled}
                className="text-destructive hover:text-destructive"
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Delete</TooltipContent>
          </Tooltip>
        </div>
      </div>

      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {totalSelectionCount || selectedHashes.length} torrent(s)?</AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone. The torrents will be removed from qBittorrent.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex items-center space-x-2 py-4">
            <input
              type="checkbox"
              id="deleteFiles"
              checked={deleteFiles}
              onChange={(e: ChangeEvent<HTMLInputElement>) => setDeleteFiles(e.target.checked)}
              className="rounded border-input"
            />
            <label htmlFor="deleteFiles" className="text-sm font-medium">
              Also delete files from disk
            </label>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Add Tags Dialog */}
      <AddTagsDialog
        open={showAddTagsDialog}
        onOpenChange={setShowAddTagsDialog}
        availableTags={availableTags}
        hashCount={totalSelectionCount || selectedHashes.length}
        onConfirm={handleAddTags}
        isPending={mutation.isPending}
      />

      {/* Set Tags Dialog */}
      <SetTagsDialog
        open={showTagsDialog}
        onOpenChange={setShowTagsDialog}
        availableTags={availableTags}
        hashCount={totalSelectionCount || selectedHashes.length}
        onConfirm={handleSetTags}
        isPending={mutation.isPending}
        initialTags={getCommonTags(selectedTorrents)}
      />

      {/* Set Category Dialog */}
      <SetCategoryDialog
        open={showCategoryDialog}
        onOpenChange={setShowCategoryDialog}
        availableCategories={availableCategories}
        hashCount={totalSelectionCount || selectedHashes.length}
        onConfirm={handleSetCategory}
        isPending={mutation.isPending}
        initialCategory={getCommonCategory(selectedTorrents)}
      />

      {/* Force Recheck Confirmation Dialog */}
      <Dialog open={showRecheckDialog} onOpenChange={setShowRecheckDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Force Recheck {totalSelectionCount || selectedHashes.length} torrent(s)?</DialogTitle>
            <DialogDescription>
              This will force qBittorrent to recheck all pieces of the selected torrents. This process may take some time and will temporarily pause the torrents.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowRecheckDialog(false)}>
              Cancel
            </Button>
            <Button onClick={handleRecheck} disabled={mutation.isPending}>
              Force Recheck
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Reannounce Confirmation Dialog */}
      <Dialog open={showReannounceDialog} onOpenChange={setShowReannounceDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Reannounce {totalSelectionCount || selectedHashes.length} torrent(s)?</DialogTitle>
            <DialogDescription>
              This will force the selected torrents to reannounce to all their trackers. This is useful when trackers are not responding or you want to refresh your connection.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowReannounceDialog(false)}>
              Cancel
            </Button>
            <Button onClick={handleReannounce} disabled={mutation.isPending}>
              Reannounce
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
})