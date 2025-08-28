/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { memo, useState, useCallback } from "react"
import type { ChangeEvent } from "react"
import { useMutation, useQueryClient, useQuery, type Query } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { toast } from "sonner"
import { applyOptimisticUpdates } from "@/lib/torrent-state-utils"
import { getCommonTags, getCommonCategory } from "@/lib/torrent-utils"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { ChevronDown, Play, Pause, Trash2, CheckCircle, Tag, Folder, Radio, Settings2, Sparkles } from "lucide-react"
import { AddTagsDialog, SetTagsDialog, SetCategoryDialog } from "./TorrentDialogs"
import { ShareLimitSubmenu, SpeedLimitsSubmenu } from "./TorrentLimitSubmenus"
import { QueueSubmenu } from "./QueueSubmenu"
import type { Torrent, TorrentResponse } from "@/types"

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

interface TorrentActionsProps {
  instanceId: number
  selectedHashes: string[]
  selectedTorrents?: Torrent[]
  onComplete?: () => void
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
}

export const TorrentActions = memo(function TorrentActions({ instanceId, selectedHashes, selectedTorrents = [], onComplete, isAllSelected = false, totalSelectionCount, filters, search, excludeHashes }: TorrentActionsProps) {
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
    queryFn: () => api.getTags(instanceId),
    staleTime: 60000,
  })

  // Fetch available categories
  const { data: availableCategories = {} } = useQuery({
    queryKey: ["categories", instanceId],
    queryFn: () => api.getCategories(instanceId),
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
      return api.bulkAction(instanceId, {
        hashes: isAllSelected ? [] : selectedHashes,  // Empty hashes when selectAll is true
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
        // Remove the query data to force immediate UI update
        queryClient.removeQueries({
          queryKey: ["torrents-list", instanceId],
          exact: false,
        })
        // Also remove counts query
        queryClient.removeQueries({
          queryKey: ["torrent-counts", instanceId],
          exact: false,
        })
        
        // Then trigger a refetch
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
        // Apply optimistic updates for actions that change visible state
        const optimisticActions = ["pause", "resume", "delete", "deleteWithFiles", "recheck", "setCategory", "addTags", "removeTags", "setTags", "toggleAutoTMM"]
        
        if (optimisticActions.includes(variables.action)) {
          // Get all cached queries for this instance
          const cache = queryClient.getQueryCache()
          const queries = cache.findAll({
            queryKey: ["torrents-list", instanceId],
            exact: false,
          })
          
          // Build payload for the action
          const payload = {
            category: variables.category,
            tags: variables.tags,
            enable: variables.enable,
            deleteFiles: variables.deleteFiles,
          }
          
          // Optimistically update torrent states in all cached queries
          queries.forEach((query: Query) => {
            queryClient.setQueryData(query.queryKey, (oldData: TorrentResponse | undefined) => {
              if (!oldData?.torrents) return oldData
              
              // Check if this query has a status filter in its key
              // Query key structure: ['torrents-list', instanceId, currentPage, filters, search]
              const queryKey = query.queryKey as readonly unknown[]
              const filters = queryKey[3] as { status?: string[] } | undefined // filters is at index 3
              const statusFilters = filters?.status || []
              
              // Apply optimistic updates using our utility function
              const { torrents: updatedTorrents } = applyOptimisticUpdates(
                oldData.torrents,
                selectedHashes,
                variables.action,
                statusFilters,
                payload
              )
              
              return {
                ...oldData,
                torrents: updatedTorrents,
                total: updatedTorrents.length,
                totalCount: updatedTorrents.length,
              }
            })
          })
        }
        
        // For other operations, add delay to allow qBittorrent to process
        // Resume operations need more time for state transition
        const delay = variables.action === "resume" ? 2000 : 1000
        
        setTimeout(() => {
          // Always invalidate to get the real state from server
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
    // Use setTags action (with fallback to addTags for older versions)
    // The backend will handle the version check
    try {
      await mutation.mutateAsync({ action: "setTags", tags: tags.join(",") })
    } catch (error: unknown) {
      // If setTags fails due to version requirement, fall back to addTags
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

  const handleSetShareLimit = useCallback(async (ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number) => {
    await mutation.mutateAsync({ 
      action: "setShareLimit", 
      ratioLimit, 
      seedingTimeLimit, 
      inactiveSeedingTimeLimit, 
    })
  }, [mutation])

  const handleSetSpeedLimits = useCallback(async (uploadLimit: number, downloadLimit: number) => {
    // Set upload and download limits separately since they are different actions
    const promises = []
    if (uploadLimit >= 0) {
      promises.push(mutation.mutateAsync({ action: "setUploadLimit", uploadLimit }))
    }
    if (downloadLimit >= 0) {
      promises.push(mutation.mutateAsync({ action: "setDownloadLimit", downloadLimit }))
    }
    
    if (promises.length > 0) {
      await Promise.all(promises)
    }
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

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" disabled={selectedHashes.length === 0 && !isAllSelected}>
            Actions ({totalSelectionCount || selectedHashes.length})
            <ChevronDown className="ml-2 h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            onClick={() => mutation.mutate({ action: "resume" })}
            disabled={mutation.isPending}
          >
            <Play className="mr-2 h-4 w-4" />
            Resume
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => mutation.mutate({ action: "pause" })}
            disabled={mutation.isPending}
          >
            <Pause className="mr-2 h-4 w-4" />
            Pause
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={handleRecheckClick}
            disabled={mutation.isPending}
          >
            <CheckCircle className="mr-2 h-4 w-4" />
            Force Recheck
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={handleReannounceClick}
            disabled={mutation.isPending}
          >
            <Radio className="mr-2 h-4 w-4" />
            Reannounce
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <QueueSubmenu
            type="dropdown"
            hashCount={selectedHashes.length}
            onQueueAction={(action) => mutation.mutate({ action })}
            isPending={mutation.isPending}
          />
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onClick={() => setShowAddTagsDialog(true)}
            disabled={mutation.isPending}
          >
            <Tag className="mr-2 h-4 w-4" />
            Add Tags
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => setShowTagsDialog(true)}
            disabled={mutation.isPending}
          >
            <Tag className="mr-2 h-4 w-4" />
            Replace Tags
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => setShowCategoryDialog(true)}
            disabled={mutation.isPending}
          >
            <Folder className="mr-2 h-4 w-4" />
            Set Category
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <ShareLimitSubmenu
            type="dropdown"
            hashCount={selectedHashes.length}
            onConfirm={handleSetShareLimit}
            isPending={mutation.isPending}
          />
          <SpeedLimitsSubmenu
            type="dropdown"
            hashCount={selectedHashes.length}
            onConfirm={handleSetSpeedLimits}
            isPending={mutation.isPending}
          />
          <DropdownMenuSeparator />
          {(() => {
            // Check TMM state across selected torrents
            const tmmStates = selectedTorrents?.map(t => t.auto_tmm) ?? []
            const allEnabled = tmmStates.length > 0 && tmmStates.every(state => state === true)
            const allDisabled = tmmStates.length > 0 && tmmStates.every(state => state === false)
            const mixed = tmmStates.length > 0 && !allEnabled && !allDisabled
            
            if (mixed) {
              return (
                <>
                  <DropdownMenuItem
                    onClick={() => mutation.mutate({ action: "toggleAutoTMM", enable: true })}
                    disabled={mutation.isPending}
                  >
                    <Sparkles className="mr-2 h-4 w-4" />
                    Enable TMM (Mixed)
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onClick={() => mutation.mutate({ action: "toggleAutoTMM", enable: false })}
                    disabled={mutation.isPending}
                  >
                    <Settings2 className="mr-2 h-4 w-4" />
                    Disable TMM (Mixed)
                  </DropdownMenuItem>
                </>
              )
            }
            
            return (
              <DropdownMenuItem
                onClick={() => mutation.mutate({ action: "toggleAutoTMM", enable: !allEnabled })}
                disabled={mutation.isPending}
              >
                {allEnabled ? (
                  <>
                    <Settings2 className="mr-2 h-4 w-4" />
                    Disable TMM
                  </>
                ) : (
                  <>
                    <Sparkles className="mr-2 h-4 w-4" />
                    Enable TMM
                  </>
                )}
              </DropdownMenuItem>
            )
          })()}
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onClick={() => setShowDeleteDialog(true)}
            disabled={mutation.isPending}
            className="text-destructive"
          >
            <Trash2 className="mr-2 h-4 w-4" />
            Delete
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

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