/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { Torrent } from "@/types"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useCallback, useState } from "react"
import type { TFunction } from "i18next"
import { useTranslation } from "react-i18next"
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
  // Client-side metadata used for optimistic updates and toast messages
  clientHashes?: string[]
  clientCount?: number
}

interface ClientMeta {
  clientHashes?: string[]
  totalSelected?: number
}

export function useTorrentActions({ instanceId, onActionComplete }: UseTorrentActionsProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  // Dialog states
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [deleteFiles, setDeleteFiles] = useState(false)
  const [showAddTagsDialog, setShowAddTagsDialog] = useState(false)
  const [showSetTagsDialog, setShowSetTagsDialog] = useState(false)
  const [showRemoveTagsDialog, setShowRemoveTagsDialog] = useState(false)
  const [showCategoryDialog, setShowCategoryDialog] = useState(false)
  const [showCreateCategoryDialog, setShowCreateCategoryDialog] = useState(false)
  const [showShareLimitDialog, setShowShareLimitDialog] = useState(false)
  const [showSpeedLimitDialog, setShowSpeedLimitDialog] = useState(false)
  const [showRecheckDialog, setShowRecheckDialog] = useState(false)
  const [showReannounceDialog, setShowReannounceDialog] = useState(false)
  const [showLocationDialog, setShowLocationDialog] = useState(false)

  // Context state for dialogs
  const [contextHashes, setContextHashes] = useState<string[]>([])
  const [contextTorrents, setContextTorrents] = useState<Torrent[]>([])

  const mutation = useMutation({
    mutationFn: (data: TorrentActionData) => {
      const { clientHashes, clientCount, ...payload } = data
      void clientHashes
      void clientCount
      return api.bulkAction(instanceId, {
        hashes: payload.hashes,
        action: payload.action,
        deleteFiles: payload.deleteFiles,
        tags: payload.tags,
        category: payload.category,
        enable: payload.enable,
        ratioLimit: payload.ratioLimit,
        seedingTimeLimit: payload.seedingTimeLimit,
        inactiveSeedingTimeLimit: payload.inactiveSeedingTimeLimit,
        uploadLimit: payload.uploadLimit,
        downloadLimit: payload.downloadLimit,
        location: payload.location,
        selectAll: payload.selectAll,
        filters: payload.filters,
        search: payload.search,
        excludeHashes: payload.excludeHashes,
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

        let hashesToRemove = variables.hashes
        if (variables.clientHashes && variables.clientHashes.length > 0) {
          hashesToRemove = variables.clientHashes
        }
        const optimisticRemoveCount = variables.clientCount ?? hashesToRemove.length

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
                !hashesToRemove.includes(t.hash)
              ) || [],
              total: Math.max(0, (oldData.total || 0) - optimisticRemoveCount),
              totalCount: Math.max(0, (oldData.totalCount || oldData.total || 0) - optimisticRemoveCount),
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
      let toastCount = variables.hashes.length
      if (variables.clientHashes && variables.clientHashes.length > 0) {
        toastCount = variables.clientHashes.length
      }
      if (typeof variables.clientCount === "number") {
        toastCount = variables.clientCount
      }
      showSuccessToast(t, variables.action, Math.max(1, toastCount), variables.deleteFiles, variables.enable)

      // Close dialogs after successful action
      if (variables.action === "delete") {
        setShowDeleteDialog(false)
      } else if (variables.action === "addTags") {
        setShowAddTagsDialog(false)
      } else if (variables.action === "setTags") {
        setShowSetTagsDialog(false)
      } else if (variables.action === "removeTags") {
        setShowRemoveTagsDialog(false)
      } else if (variables.action === "setCategory") {
        setShowCategoryDialog(false)
        setShowCreateCategoryDialog(false)
      } else if (variables.action === "setShareLimit") {
        setShowShareLimitDialog(false)
      } else if (variables.action === "setUploadLimit" || variables.action === "setDownloadLimit") {
        setShowSpeedLimitDialog(false)
      } else if (variables.action === "setLocation") {
        setShowLocationDialog(false)
      } else if (variables.action === "recheck") {
        setShowRecheckDialog(false)
      } else if (variables.action === "reannounce") {
        setShowReannounceDialog(false)
      }

      onActionComplete?.()
    },
    onError: (error: Error, variables) => {
      const count = variables.hashes.length || 1
      toast.error(t("hooks.useTorrentActions.failedToAction", { action: variables.action, count }), {
        description: error.message || t("hooks.useTorrentActions.unexpectedError"),
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
    excludeHashes?: string[],
    clientMeta?: ClientMeta
  ) => {
    const clientHashes = clientMeta?.clientHashes ?? hashes
    const clientCount = clientMeta?.totalSelected
      ?? (clientHashes?.length ?? hashes.length)
    await mutation.mutateAsync({
      action: "delete",
      deleteFiles,
      hashes: isAllSelected ? [] : hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
      clientHashes,
      clientCount,
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
    excludeHashes?: string[],
    clientMeta?: ClientMeta
  ) => {
    const clientHashes = clientMeta?.clientHashes ?? hashes
    const clientCount = clientMeta?.totalSelected
      ?? (clientHashes?.length ?? hashes.length)
    await mutation.mutateAsync({
      action: "addTags",
      tags: tags.join(","),
      hashes: isAllSelected ? [] : hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
      clientHashes,
      clientCount,
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
    excludeHashes?: string[],
    clientMeta?: ClientMeta
  ) => {
    const clientHashes = clientMeta?.clientHashes ?? hashes
    const clientCount = clientMeta?.totalSelected
      ?? (clientHashes?.length ?? hashes.length)
    try {
      await mutation.mutateAsync({
        action: "setTags",
        tags: tags.join(","),
        hashes: isAllSelected ? [] : hashes,
        selectAll: isAllSelected,
        filters: isAllSelected ? filters : undefined,
        search: isAllSelected ? search : undefined,
        excludeHashes: isAllSelected ? excludeHashes : undefined,
        clientHashes,
        clientCount,
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
          clientHashes,
          clientCount,
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
    excludeHashes?: string[],
    clientMeta?: ClientMeta
  ) => {
    const clientHashes = clientMeta?.clientHashes ?? hashes
    const clientCount = clientMeta?.totalSelected
      ?? (clientHashes?.length ?? hashes.length)
    await mutation.mutateAsync({
      action: "removeTags",
      tags: tags.join(","),
      hashes: isAllSelected ? [] : hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
      clientHashes,
      clientCount,
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
    excludeHashes?: string[],
    clientMeta?: ClientMeta
  ) => {
    const clientHashes = clientMeta?.clientHashes ?? hashes
    const clientCount = clientMeta?.totalSelected
      ?? (clientHashes?.length ?? hashes.length)
    await mutation.mutateAsync({
      action: "setCategory",
      category,
      hashes: isAllSelected ? [] : hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
      clientHashes,
      clientCount,
    })
    setShowCategoryDialog(false)
    setContextHashes([])
    setContextTorrents([])
  }, [mutation])

  const handleSetShareLimit = useCallback(async (
    ratioLimit: number,
    seedingTimeLimit: number,
    inactiveSeedingTimeLimit: number,
    hashes: string[],
    isAllSelected?: boolean,
    filters?: TorrentActionData["filters"],
    search?: string,
    excludeHashes?: string[],
    clientMeta?: ClientMeta
  ) => {
    const clientHashes = clientMeta?.clientHashes ?? hashes
    const clientCount = clientMeta?.totalSelected
      ?? (clientHashes?.length ?? hashes.length)
    await mutation.mutateAsync({
      action: "setShareLimit",
      hashes: isAllSelected ? [] : hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
      ratioLimit,
      seedingTimeLimit,
      inactiveSeedingTimeLimit,
      clientHashes,
      clientCount,
    })
    setShowShareLimitDialog(false)
    setContextHashes([])
    setContextTorrents([])
  }, [mutation])

  const handleSetSpeedLimits = useCallback(async (
    uploadLimit: number,
    downloadLimit: number,
    hashes: string[],
    isAllSelected?: boolean,
    filters?: TorrentActionData["filters"],
    search?: string,
    excludeHashes?: string[],
    clientMeta?: ClientMeta
  ) => {
    const clientHashes = clientMeta?.clientHashes ?? hashes
    const clientCount = clientMeta?.totalSelected
      ?? (clientHashes?.length ?? hashes.length)
    const sharedOptions = {
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
      clientHashes,
      clientCount,
    }
    const promises = []
    if (uploadLimit >= 0) {
      promises.push(mutation.mutateAsync({
        action: "setUploadLimit",
        hashes: isAllSelected ? [] : hashes,
        uploadLimit,
        ...sharedOptions,
      }))
    }
    if (downloadLimit >= 0) {
      promises.push(mutation.mutateAsync({
        action: "setDownloadLimit",
        hashes: isAllSelected ? [] : hashes,
        downloadLimit,
        ...sharedOptions,
      }))
    }
    if (promises.length > 0) {
      await Promise.all(promises)
    }
    setShowSpeedLimitDialog(false)
    setContextHashes([])
    setContextTorrents([])
  }, [mutation])

  const handleRecheck = useCallback(async (
    hashes: string[],
    isAllSelected?: boolean,
    filters?: TorrentActionData["filters"],
    search?: string,
    excludeHashes?: string[],
    clientMeta?: ClientMeta
  ) => {
    const clientHashes = clientMeta?.clientHashes ?? hashes
    const clientCount = clientMeta?.totalSelected
      ?? (clientHashes?.length ?? hashes.length)
    await mutation.mutateAsync({
      action: "recheck",
      hashes: isAllSelected ? [] : hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
      clientHashes,
      clientCount,
    })
    setShowRecheckDialog(false)
    setContextHashes([])
  }, [mutation])

  const handleReannounce = useCallback(async (
    hashes: string[],
    isAllSelected?: boolean,
    filters?: TorrentActionData["filters"],
    search?: string,
    excludeHashes?: string[],
    clientMeta?: ClientMeta
  ) => {
    const clientHashes = clientMeta?.clientHashes ?? hashes
    const clientCount = clientMeta?.totalSelected
      ?? (clientHashes?.length ?? hashes.length)
    await mutation.mutateAsync({
      action: "reannounce",
      hashes: isAllSelected ? [] : hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
      clientHashes,
      clientCount,
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
    excludeHashes?: string[],
    clientMeta?: ClientMeta
  ) => {
    const clientHashes = clientMeta?.clientHashes ?? hashes
    const clientCount = clientMeta?.totalSelected
      ?? (clientHashes?.length ?? hashes.length)
    await mutation.mutateAsync({
      action: "setLocation",
      location,
      hashes: isAllSelected ? [] : hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
      clientHashes,
      clientCount,
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

  const prepareCreateCategoryAction = useCallback((hashes: string[], torrents?: Torrent[]) => {
    setContextHashes(hashes)
    if (torrents) setContextTorrents(torrents)
    setShowCreateCategoryDialog(true)
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

  const prepareShareLimitAction = useCallback((hashes: string[], torrents?: Torrent[]) => {
    setContextHashes(hashes)
    if (torrents) setContextTorrents(torrents)
    setShowShareLimitDialog(true)
  }, [])

  const prepareSpeedLimitAction = useCallback((hashes: string[], torrents?: Torrent[]) => {
    setContextHashes(hashes)
    if (torrents) setContextTorrents(torrents)
    setShowSpeedLimitDialog(true)
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
    showCreateCategoryDialog,
    setShowCreateCategoryDialog,
    showShareLimitDialog,
    setShowShareLimitDialog,
    showSpeedLimitDialog,
    setShowSpeedLimitDialog,
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
    prepareCreateCategoryAction,
    prepareShareLimitAction,
    prepareSpeedLimitAction,
    prepareRecheckAction,
    prepareReannounceAction,
    prepareLocationAction,
  }
}

// Helper function for success toasts
function showSuccessToast(
  t: TFunction<"translation", undefined>,
  action: TorrentAction,
  count: number,
  deleteFiles?: boolean,
  enable?: boolean
) {
  switch (action) {
    case "resume":
      toast.success(t("hooks.useTorrentActions.resumed", { count }))
      break
    case "pause":
      toast.success(t("hooks.useTorrentActions.paused", { count }))
      break
    case "delete":
      if (deleteFiles) {
        toast.success(t("hooks.useTorrentActions.deletedAndFiles", { count }))
      } else {
        toast.success(t("hooks.useTorrentActions.deleted", { count }))
      }
      break
    case "recheck":
      toast.success(t("hooks.useTorrentActions.rechecked", { count }))
      break
    case "reannounce":
      toast.success(t("hooks.useTorrentActions.reannounced", { count }))
      break
    case "increasePriority":
      toast.success(t("hooks.useTorrentActions.increasedPriority", { count }))
      break
    case "decreasePriority":
      toast.success(t("hooks.useTorrentActions.decreasedPriority", { count }))
      break
    case "topPriority":
      toast.success(t("hooks.useTorrentActions.topPriority", { count }))
      break
    case "bottomPriority":
      toast.success(t("hooks.useTorrentActions.bottomPriority", { count }))
      break
    case "addTags":
      toast.success(t("hooks.useTorrentActions.addedTags", { count }))
      break
    case "removeTags":
      toast.success(t("hooks.useTorrentActions.removedTags", { count }))
      break
    case "setTags":
      toast.success(t("hooks.useTorrentActions.replacedTags", { count }))
      break
    case "setCategory":
      toast.success(t("hooks.useTorrentActions.setCategory", { count }))
      break
    case "toggleAutoTMM":
      if (enable) {
        toast.success(t("hooks.useTorrentActions.enabledAutoTMM", { count }))
      } else {
        toast.success(t("hooks.useTorrentActions.disabledAutoTMM", { count }))
      }
      break
    case "setShareLimit":
      toast.success(t("hooks.useTorrentActions.setShareLimits", { count }))
      break
    case "setUploadLimit":
      toast.success(t("hooks.useTorrentActions.setUploadLimit", { count }))
      break
    case "setDownloadLimit":
      toast.success(t("hooks.useTorrentActions.setDownloadLimit", { count }))
      break
    case "setLocation":
      toast.success(t("hooks.useTorrentActions.setLocation", { count }))
      break
  }
}
