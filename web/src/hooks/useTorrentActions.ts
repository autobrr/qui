/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { usePersistedDeleteFiles } from "@/hooks/usePersistedDeleteFiles"
import { usePersistedCrossSeedBlocklist } from "@/hooks/usePersistedCrossSeedBlocklist"
import { api } from "@/lib/api"
import type { Torrent, TorrentFilters } from "@/types"
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
  FORCE_START: "forceStart",
  SET_SHARE_LIMIT: "setShareLimit",
  SET_UPLOAD_LIMIT: "setUploadLimit",
  SET_DOWNLOAD_LIMIT: "setDownloadLimit",
  SET_LOCATION: "setLocation",
  TOGGLE_SEQUENTIAL_DOWNLOAD: "toggleSequentialDownload",
} as const

// Derive the type from the const object - single source of truth
export type TorrentAction = typeof TORRENT_ACTIONS[keyof typeof TORRENT_ACTIONS]

export type TorrentActionComplete =
  | TorrentAction
  | "renameTorrent"
  | "renameTorrentFile"
  | "renameTorrentFolder"

interface UseTorrentActionsProps {
  instanceId: number
  onActionComplete?: (action: TorrentActionComplete) => void
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
  filters?: TorrentFilters
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
  const queryClient = useQueryClient()

  // Dialog states
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const {
    deleteFiles,
    setDeleteFiles,
    isLocked: isDeleteFilesLocked,
    toggleLock: toggleDeleteFilesLock,
  } = usePersistedDeleteFiles(false)
  const { blockCrossSeeds, setBlockCrossSeeds } = usePersistedCrossSeedBlocklist(instanceId, false)
  const [deleteCrossSeeds, setDeleteCrossSeeds] = useState(false)
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
  const [showRenameTorrentDialog, setShowRenameTorrentDialog] = useState(false)
  const [showRenameFileDialog, setShowRenameFileDialog] = useState(false)
  const [showRenameFolderDialog, setShowRenameFolderDialog] = useState(false)
  const [showTmmDialog, setShowTmmDialog] = useState(false)
  const [pendingTmmEnable, setPendingTmmEnable] = useState(false)
  const [showLocationWarningDialog, setShowLocationWarningDialog] = useState(false)

  // Context state for dialogs
  const [contextHashes, setContextHashes] = useState<string[]>([])
  const [contextTorrents, setContextTorrents] = useState<Torrent[]>([])

  const mutation = useMutation({
    mutationFn: (data: TorrentActionData) => {
      const { clientHashes, clientCount, ...payload } = data
      void clientHashes
      void clientCount
      const effectiveFilters = payload.filters? {
        ...payload.filters,
        categories: payload.filters.expandedCategories ?? payload.filters.categories ?? [],
        excludeCategories: payload.filters.expandedExcludeCategories ?? payload.filters.excludeCategories ?? [],
      }: undefined

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
        filters: effectiveFilters,
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
        const refetchDelay = variables.action === "resume" || variables.action === "forceStart" ? 2000 : 1000
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
      showSuccessToast(variables.action, Math.max(1, toastCount), variables.deleteFiles, variables.enable)

      // Close dialogs after successful action
      if (variables.action === "delete") {
        setShowDeleteDialog(false)
        setDeleteCrossSeeds(false)
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

      onActionComplete?.(variables.action)
    },
    onError: (error: Error, variables) => {
      const count = variables.hashes.length || 1
      const torrentText = count === 1 ? "torrent" : "torrents"
      toast.error(`Failed to ${variables.action} ${count} ${torrentText}`, {
        description: error.message || "An unexpected error occurred",
      })
    },
  })

  const renameTorrentMutation = useMutation({
    mutationFn: async ({ hash, name }: { hash: string; name: string }) => {
      await api.renameTorrent(instanceId, hash, name)
      return { hash, name }
    },
    onSuccess: async (_, variables) => {
      setShowRenameTorrentDialog(false)
      setContextHashes([])
      setContextTorrents([])

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
      }, 750)

      toast.success(`Renamed torrent to "${variables.name}"`)
      onActionComplete?.("renameTorrent")
    },
    onError: (error: Error) => {
      toast.error(`Failed to rename torrent: ${error.message}`)
    },
  })

  const renameFileMutation = useMutation({
    mutationFn: async ({ hash, oldPath, newPath }: { hash: string; oldPath: string; newPath: string }) => {
      await api.renameTorrentFile(instanceId, hash, oldPath, newPath)
      return { hash, oldPath, newPath }
    },
    onSuccess: async (_, variables) => {
      setShowRenameFileDialog(false)

      queryClient.invalidateQueries({
        queryKey: ["torrent-files", instanceId, variables.hash],
        exact: false,
      })

      setContextHashes([])
      setContextTorrents([])

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
      }, 750)

      const newFileName = variables.newPath.split("/").pop() ?? variables.newPath
      toast.success(`Renamed file to "${newFileName}"`)
      onActionComplete?.("renameTorrentFile")
    },
    onError: (error: Error) => {
      toast.error(`Failed to rename file: ${error.message}`)
    },
  })

  const renameFolderMutation = useMutation({
    mutationFn: async ({ hash, oldPath, newPath }: { hash: string; oldPath: string; newPath: string }) => {
      await api.renameTorrentFolder(instanceId, hash, oldPath, newPath)
      return { hash, oldPath, newPath }
    },
    onSuccess: async (_, variables) => {
      setShowRenameFolderDialog(false)

      queryClient.invalidateQueries({
        queryKey: ["torrent-files", instanceId, variables.hash],
        exact: false,
      })

      setContextHashes([])
      setContextTorrents([])

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
      }, 750)

      const newFolderName = variables.newPath.split("/").pop() ?? variables.newPath
      toast.success(`Renamed folder to "${newFolderName}"`)
      onActionComplete?.("renameTorrentFolder")
    },
    onError: (error: Error) => {
      toast.error(`Failed to rename folder: ${error.message}`)
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
    setDeleteCrossSeeds(false)
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

  const handleRenameTorrent = useCallback(async (hash: string, name: string) => {
    const trimmed = name.trim()
    if (!trimmed) {
      toast.error("Torrent name cannot be empty")
      return
    }
    await renameTorrentMutation.mutateAsync({ hash, name: trimmed })
  }, [renameTorrentMutation])

  const handleRenameFile = useCallback(async (hash: string, oldPath: string, newPath: string) => {
    const trimmedOldPath = oldPath.trim()
    const trimmedNewPath = newPath.trim()
    if (!trimmedOldPath || !trimmedNewPath) {
      toast.error("Both original and new file paths are required")
      return
    }
    if (trimmedOldPath === trimmedNewPath) {
      toast.success("File name unchanged")
      setShowRenameFileDialog(false)
      setContextHashes([])
      setContextTorrents([])
      return
    }
    await renameFileMutation.mutateAsync({ hash, oldPath: trimmedOldPath, newPath: trimmedNewPath })
  }, [renameFileMutation])

  const handleRenameFolder = useCallback(async (hash: string, oldPath: string, newPath: string) => {
    const trimmedOldPath = oldPath.trim()
    const trimmedNewPath = newPath.trim()
    if (!trimmedOldPath || !trimmedNewPath) {
      toast.error("Both original and new folder paths are required")
      return
    }
    if (trimmedOldPath === trimmedNewPath) {
      toast.success("Folder name unchanged")
      setShowRenameFolderDialog(false)
      setContextHashes([])
      setContextTorrents([])
      return
    }
    await renameFolderMutation.mutateAsync({ hash, oldPath: trimmedOldPath, newPath: trimmedNewPath })
  }, [renameFolderMutation])

  const prepareDeleteAction = useCallback((hashes: string[], torrents?: Torrent[]) => {
    setContextHashes(hashes)
    if (torrents) setContextTorrents(torrents)
    setDeleteCrossSeeds(false) // Reset on open to avoid stale state from previous dialog
    setShowDeleteDialog(true)
  }, [])

  const closeDeleteDialog = useCallback(() => {
    setShowDeleteDialog(false)
    setDeleteCrossSeeds(false)
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
    setShowLocationWarningDialog(true)
  }, [])

  const prepareTmmAction = useCallback((hashes: string[], _count?: number, enable?: boolean) => {
    setContextHashes(hashes)
    setPendingTmmEnable(enable ?? false)
    setShowTmmDialog(true)
  }, [])

  const handleTmmConfirm = useCallback((
    hashes: string[],
    isAllSelected?: boolean,
    filters?: TorrentActionData["filters"],
    search?: string,
    excludeHashes?: string[],
    clientMeta?: ClientMeta
  ) => {
    const clientHashes = clientMeta?.clientHashes ?? hashes
    const clientCount = clientMeta?.totalSelected ?? (clientHashes?.length ?? hashes.length)
    mutation.mutate({
      action: TORRENT_ACTIONS.TOGGLE_AUTO_TMM,
      hashes: isAllSelected ? [] : hashes,
      enable: pendingTmmEnable,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? search : undefined,
      excludeHashes: isAllSelected ? excludeHashes : undefined,
      clientHashes,
      clientCount,
    })
    setShowTmmDialog(false)
    setContextHashes([])
  }, [mutation, pendingTmmEnable])

  const proceedToLocationDialog = useCallback(() => {
    setShowLocationWarningDialog(false)
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

  const prepareRenameTorrentAction = useCallback((hashes: string[], torrents?: Torrent[]) => {
    if (hashes.length === 0) return
    setContextHashes(hashes)
    if (torrents) setContextTorrents(torrents)
    setShowRenameTorrentDialog(true)
  }, [])

  const prepareRenameFileAction = useCallback((hashes: string[], torrents?: Torrent[]) => {
    if (hashes.length === 0) return
    setContextHashes(hashes)
    if (torrents) setContextTorrents(torrents)
    setShowRenameFileDialog(true)
  }, [])

  const prepareRenameFolderAction = useCallback((hashes: string[], torrents?: Torrent[]) => {
    if (hashes.length === 0) return
    setContextHashes(hashes)
    if (torrents) setContextTorrents(torrents)
    setShowRenameFolderDialog(true)
  }, [])

  const isPending = mutation.isPending || renameTorrentMutation.isPending || renameFileMutation.isPending || renameFolderMutation.isPending


  return {
    // State
    showDeleteDialog,
    setShowDeleteDialog,
    closeDeleteDialog,
    deleteFiles,
    setDeleteFiles,
    isDeleteFilesLocked,
    toggleDeleteFilesLock,
    blockCrossSeeds,
    setBlockCrossSeeds,
    deleteCrossSeeds,
    setDeleteCrossSeeds,
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
    showRenameTorrentDialog,
    setShowRenameTorrentDialog,
    showRenameFileDialog,
    setShowRenameFileDialog,
    showRenameFolderDialog,
    setShowRenameFolderDialog,
    showTmmDialog,
    setShowTmmDialog,
    pendingTmmEnable,
    showLocationWarningDialog,
    setShowLocationWarningDialog,
    contextHashes,
    contextTorrents,

    // Mutation state
    isPending,

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
    handleRenameTorrent,
    handleRenameFile,
    handleRenameFolder,

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
    prepareRenameTorrentAction,
    prepareRenameFileAction,
    prepareRenameFolderAction,
    prepareTmmAction,
    handleTmmConfirm,
    proceedToLocationDialog,
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
    case "forceStart":
      toast.success(`${enable ? "Enabled" : "Disabled"} Force Start for ${count} ${torrentText}`)
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
    case "toggleSequentialDownload":
      toast.success(`${enable ? "Enabled" : "Disabled"} sequential download for ${count} ${torrentText}`)
      break
  }
}
