/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { Torrent } from "@/types"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useCallback, useState } from "react"
import { toast } from "sonner"

// Const object for better developer experience and refactoring safety
export const TORRENT_ACTIONS = {
  PAUSE: "pause",
  RESUME: "resume",
  DELETE: "delete",
  RECHECK: "recheck",
  REANNOUNCE: "reannounce",
  INCREASE_PRIORITY: "increasePriority",
  DECREASE_PRIORITY: "decreasePriority",
  TOP_PRIORITY: "topPriority",
  BOTTOM_PRIORITY: "bottomPriority",
  ADD_TAGS: "addTags",
  REMOVE_TAGS: "removeTags",
  SET_TAGS: "setTags",
  SET_CATEGORY: "setCategory",
  TOGGLE_AUTO_TMM: "toggleAutoTMM",
  SET_SHARE_LIMIT: "setShareLimit",
  SET_UPLOAD_LIMIT: "setUploadLimit",
  SET_DOWNLOAD_LIMIT: "setDownloadLimit",
  SET_LOCATION: "setLocation",
} as const

// Derive the type from the const object - single source of truth
export type TorrentAction = typeof TORRENT_ACTIONS[keyof typeof TORRENT_ACTIONS]

interface UseTorrentActionsProps {
  instanceId: number
  onActionComplete?: () => void
}

interface TorrentActionData {
  action: TorrentAction
  hashes: string[]
  deleteFiles?: boolean
  tags?: string
  category?: string
  enable?: boolean
  ratioLimit?: number
  seedingTimeLimit?: number
  inactiveSeedingTimeLimit?: number
  uploadLimit?: number
  downloadLimit?: number
  location?: string
  selectAll?: boolean
  filters?: {
    status: string[]
    categories: string[]
    tags: string[]
    trackers: string[]
  }
  search?: string
  excludeHashes?: string[]
}

export function useTorrentActions({ instanceId, onActionComplete }: UseTorrentActionsProps) {
  const queryClient = useQueryClient()

  // Dialog states
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [deleteFiles, setDeleteFiles] = useState(false)
  const [showAddTagsDialog, setShowAddTagsDialog] = useState(false)
  const [showSetTagsDialog, setShowSetTagsDialog] = useState(false)
  const [showRemoveTagsDialog, setShowRemoveTagsDialog] = useState(false)
  const [showCategoryDialog, setShowCategoryDialog] = useState(false)
  const [showRecheckDialog, setShowRecheckDialog] = useState(false)
  const [showReannounceDialog, setShowReannounceDialog] = useState(false)
  const [showLocationDialog, setShowLocationDialog] = useState(false)

  // Context state for dialogs
  const [contextHashes, setContextHashes] = useState<string[]>([])
  const [contextTorrents, setContextTorrents] = useState<Torrent[]>([])

  const mutation = useMutation({
    mutationFn: (data: TorrentActionData) => {
      return api.bulkAction(instanceId, {
        hashes: data.hashes,
        action: data.action,
        deleteFiles: data.deleteFiles,
        tags: data.tags,
        category: data.category,
        enable: data.enable,
        ratioLimit: data.ratioLimit,
        seedingTimeLimit: data.seedingTimeLimit,
        inactiveSeedingTimeLimit: data.inactiveSeedingTimeLimit,
        uploadLimit: data.uploadLimit,
        downloadLimit: data.downloadLimit,
        location: data.location,
        selectAll: data.selectAll,
        filters: data.filters,
        search: data.search,
        excludeHashes: data.excludeHashes,
      })
    },
    onSuccess: async (_, variables) => {
      // Handle delete operations with optimistic updates
      if (variables.action === "delete") {
        // Clear selection and context
        setContextHashes([])
        setContextTorrents([])

        // Optimistically remove torrents from cached queries
        const cache = queryClient.getQueryCache()
        const queries = cache.findAll({
          queryKey: ["torrents-list", instanceId],
          exact: false,
        })

        queries.forEach((query) => {
          queryClient.setQueryData(query.queryKey, (oldData: {
            torrents?: Torrent[]
            total?: number
            totalCount?: number
          }) => {
            if (!oldData) return oldData
            return {
              ...oldData,
              torrents: oldData.torrents?.filter((t: Torrent) =>
                !variables.hashes.includes(t.hash)
              ) || [],
              total: Math.max(0, (oldData.total || 0) - variables.hashes.length),
              totalCount: Math.max(0, (oldData.totalCount || oldData.total || 0) - variables.hashes.length),
            }
          })
        })

        // Refetch later to sync with server
        const refetchDelay = variables.deleteFiles ? 5000 : 2000
        setTimeout(() => {
          queryClient.refetchQueries({
            queryKey: ["torrents-list", instanceId],
            exact: false,
            type: "active",
          })
          queryClient.refetchQueries({
            queryKey: ["torrent-counts", instanceId],
            exact: false,
            type: "active",
          })
        }, refetchDelay)
      } else {
        // For other operations, refetch after delay
        const refetchDelay = variables.action === "resume" ? 2000 : 1000
        setTimeout(() => {
          queryClient.refetchQueries({
            queryKey: ["torrents-list", instanceId],
            exact: false,
            type: "active",
          })
          queryClient.refetchQueries({
            queryKey: ["torrent-counts", instanceId],
            exact: false,
            type: "active",
          })
        }, refetchDelay)
        setContextHashes([])
        setContextTorrents([])
      }

      // Show success toast
      showSuccessToast(variables.action, variables.hashes.length || 1, variables.deleteFiles, variables.enable)

      onActionComplete?.()
    },
    onError: (error: Error, variables) => {
      const count = variables.hashes.length || 1
      const torrentText = count === 1 ? "torrent" : "torrents"
      toast.error(`Failed to ${variables.action} ${count} ${torrentText}`, {
        description: error.message || "An unexpected error occurred",
      })
    },
  })

  // Action handlers
  const handleAction = useCallback((
    action: TorrentAction,
    hashes: string[],
    options?: Partial<TorrentActionData>
  ) => {
    mutation.mutate({
      action,
      hashes,
      ...options,
    })
  }, [mutation])

  const handleDelete = useCallback(async (
    hashes: string[],
    isAllSelected?: boolean,
    filters?: TorrentActionData["filters"],
    search?: string,
    excludeHashes?: string[]
  ) => {
    await mutation.mutateAsync({
      action: "delete",
      deleteFiles,
      hashes: isAllSelected ? [] : hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
    })
    setShowDeleteDialog(false)
    setDeleteFiles(false)
    setContextHashes([])
    setContextTorrents([])
  }, [mutation, deleteFiles])

  const handleAddTags = useCallback(async (
    tags: string[],
    hashes: string[],
    isAllSelected?: boolean,
    filters?: TorrentActionData["filters"],
    search?: string,
    excludeHashes?: string[]
  ) => {
    await mutation.mutateAsync({
      action: "addTags",
      tags: tags.join(","),
      hashes: isAllSelected ? [] : hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
    })
    setShowAddTagsDialog(false)
    setContextHashes([])
    setContextTorrents([])
  }, [mutation])

  const handleSetTags = useCallback(async (
    tags: string[],
    hashes: string[],
    isAllSelected?: boolean,
    filters?: TorrentActionData["filters"],
    search?: string,
    excludeHashes?: string[]
  ) => {
    try {
      await mutation.mutateAsync({
        action: "setTags",
        tags: tags.join(","),
        hashes: isAllSelected ? [] : hashes,
        selectAll: isAllSelected,
        filters: isAllSelected ? filters : undefined,
        search: isAllSelected ? search : undefined,
        excludeHashes: isAllSelected ? excludeHashes : undefined,
      })
    } catch (error) {
      // Fallback to addTags for older qBittorrent versions
      if ((error as Error).message?.includes("requires qBittorrent")) {
        await mutation.mutateAsync({
          action: "addTags",
          tags: tags.join(","),
          hashes: isAllSelected ? [] : hashes,
          selectAll: isAllSelected,
          filters: isAllSelected ? filters : undefined,
          search: isAllSelected ? search : undefined,
          excludeHashes: isAllSelected ? excludeHashes : undefined,
        })
      } else {
        throw error
      }
    }
    setShowSetTagsDialog(false)
    setContextHashes([])
    setContextTorrents([])
  }, [mutation])

  const handleRemoveTags = useCallback(async (
    tags: string[],
    hashes: string[],
    isAllSelected?: boolean,
    filters?: TorrentActionData["filters"],
    search?: string,
    excludeHashes?: string[]
  ) => {
    await mutation.mutateAsync({
      action: "removeTags",
      tags: tags.join(","),
      hashes: isAllSelected ? [] : hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
    })
    setShowRemoveTagsDialog(false)
    setContextHashes([])
    setContextTorrents([])
  }, [mutation])

  const handleSetCategory = useCallback(async (
    category: string,
    hashes: string[],
    isAllSelected?: boolean,
    filters?: TorrentActionData["filters"],
    search?: string,
    excludeHashes?: string[]
  ) => {
    await mutation.mutateAsync({
      action: "setCategory",
      category,
      hashes: isAllSelected ? [] : hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
    })
    setShowCategoryDialog(false)
    setContextHashes([])
    setContextTorrents([])
  }, [mutation])

  const handleSetShareLimit = useCallback(async (
    ratioLimit: number,
    seedingTimeLimit: number,
    inactiveSeedingTimeLimit: number,
    hashes: string[]
  ) => {
    await mutation.mutateAsync({
      action: "setShareLimit",
      hashes,
      ratioLimit,
      seedingTimeLimit,
      inactiveSeedingTimeLimit,
    })
    setContextHashes([])
    setContextTorrents([])
  }, [mutation])

  const handleSetSpeedLimits = useCallback(async (
    uploadLimit: number,
    downloadLimit: number,
    hashes: string[]
  ) => {
    const promises = []
    if (uploadLimit >= 0) {
      promises.push(mutation.mutateAsync({ action: "setUploadLimit", hashes, uploadLimit }))
    }
    if (downloadLimit >= 0) {
      promises.push(mutation.mutateAsync({ action: "setDownloadLimit", hashes, downloadLimit }))
    }
    if (promises.length > 0) {
      await Promise.all(promises)
    }
    setContextHashes([])
    setContextTorrents([])
  }, [mutation])

  const handleRecheck = useCallback(async (
    hashes: string[],
    isAllSelected?: boolean,
    filters?: TorrentActionData["filters"],
    search?: string,
    excludeHashes?: string[]
  ) => {
    await mutation.mutateAsync({
      action: "recheck",
      hashes: isAllSelected ? [] : hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
    })
    setShowRecheckDialog(false)
    setContextHashes([])
  }, [mutation])

  const handleReannounce = useCallback(async (
    hashes: string[],
    isAllSelected?: boolean,
    filters?: TorrentActionData["filters"],
    search?: string,
    excludeHashes?: string[]
  ) => {
    await mutation.mutateAsync({
      action: "reannounce",
      hashes: isAllSelected ? [] : hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
    })
    setShowReannounceDialog(false)
    setContextHashes([])
  }, [mutation])

  const handleSetLocation = useCallback(async (
    location: string,
    hashes: string[],
    isAllSelected?: boolean,
    filters?: TorrentActionData["filters"],
    search?: string,
    excludeHashes?: string[]
  ) => {
    await mutation.mutateAsync({
      action: "setLocation",
      location,
      hashes: isAllSelected ? [] : hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
    })
    setShowLocationDialog(false)
    setContextHashes([])
    setContextTorrents([])
  }, [mutation])

  const prepareDeleteAction = useCallback((hashes: string[], torrents?: Torrent[]) => {
    setContextHashes(hashes)
    if (torrents) setContextTorrents(torrents)
    setShowDeleteDialog(true)
  }, [])

  const prepareTagsAction = useCallback((action: "add" | "set" | "remove", hashes: string[], torrents?: Torrent[]) => {
    setContextHashes(hashes)
    if (torrents) setContextTorrents(torrents)

    if (action === "add") setShowAddTagsDialog(true)
    else if (action === "set") setShowSetTagsDialog(true)
    else if (action === "remove") setShowRemoveTagsDialog(true)
  }, [])

  const prepareCategoryAction = useCallback((hashes: string[], torrents?: Torrent[]) => {
    setContextHashes(hashes)
    if (torrents) setContextTorrents(torrents)
    setShowCategoryDialog(true)
  }, [])

  const prepareRecheckAction = useCallback((hashes: string[], count?: number) => {
    const actualCount = count || hashes.length
    setContextHashes(hashes)
    if (actualCount > 1) {
      setShowRecheckDialog(true)
    } else {
      handleAction("recheck", hashes)
    }
  }, [handleAction])

  const prepareReannounceAction = useCallback((hashes: string[], count?: number) => {
    const actualCount = count || hashes.length
    setContextHashes(hashes)
    if (actualCount > 1) {
      setShowReannounceDialog(true)
    } else {
      handleAction("reannounce", hashes)
    }
  }, [handleAction])

  const prepareLocationAction = useCallback((hashes: string[], torrents?: Torrent[]) => {
    setContextHashes(hashes)
    if (torrents) setContextTorrents(torrents)
    setShowLocationDialog(true)
  }, [])

  return {
    // State
    showDeleteDialog,
    setShowDeleteDialog,
    deleteFiles,
    setDeleteFiles,
    showAddTagsDialog,
    setShowAddTagsDialog,
    showSetTagsDialog,
    setShowSetTagsDialog,
    showRemoveTagsDialog,
    setShowRemoveTagsDialog,
    showCategoryDialog,
    setShowCategoryDialog,
    showRecheckDialog,
    setShowRecheckDialog,
    showReannounceDialog,
    setShowReannounceDialog,
    showLocationDialog,
    setShowLocationDialog,
    contextHashes,
    contextTorrents,

    // Mutation state
    isPending: mutation.isPending,

    // Direct action handlers
    handleAction,
    handleDelete,
    handleAddTags,
    handleSetTags,
    handleRemoveTags,
    handleSetCategory,
    handleSetShareLimit,
    handleSetSpeedLimits,
    handleRecheck,
    handleReannounce,
    handleSetLocation,

    // Preparation handlers (for showing dialogs)
    prepareDeleteAction,
    prepareTagsAction,
    prepareCategoryAction,
    prepareRecheckAction,
    prepareReannounceAction,
    prepareLocationAction,
  }
}

// Helper function for success toasts
function showSuccessToast(action: TorrentAction, count: number, deleteFiles?: boolean, enable?: boolean) {
  const torrentText = count === 1 ? "torrent" : "torrents"

  switch (action) {
    case "resume":
      toast.success(`Resumed ${count} ${torrentText}`)
      break
    case "pause":
      toast.success(`Paused ${count} ${torrentText}`)
      break
    case "delete":
      toast.success(`Deleted ${count} ${torrentText}${deleteFiles ? " and files" : ""}`)
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
      toast.success(`${enable ? "Enabled" : "Disabled"} Auto TMM for ${count} ${torrentText}`)
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
    case "setLocation":
      toast.success(`Set location for ${count} ${torrentText}`)
      break
  }
}