/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import { ALL_INSTANCES_ID } from "@/lib/instances"
import type { TagUpdatePlan } from "@/lib/tag-editor"
import { getTorrentTargetInstanceId, type TorrentActionTarget } from "@/lib/torrent-action-targets"
import type { Torrent } from "@/types"
import type { QueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

// i18n-namespace: torrents

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
  SET_COMMENT: "setComment",
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

type TagBulkActionResult = {
  action: "add" | "remove"
  status: "success" | "failed"
  error?: Error
}

// Pending post-action refetch timers; a burst of mutations collapses into a
// single trailing refetch pair instead of one pair per action (each refetch
// re-delivers the whole loaded window).
let pendingListRefetches: ReturnType<typeof setTimeout>[] = []
let pendingFinalRefetchAt = 0

// Rows outside the SSE live window are only updated by these delayed
// refetches. The first one can read the backend before its post-action
// sync has landed, so schedule a second pass to self-correct a read that
// came back too early. Every mutation that changes list-visible data must
// refetch through this — including the details panel's delete handlers,
// which call api.bulkAction directly.
// The whole ["torrents-list"] family, not a per-instance key: in the
// all-instances view the active list query is keyed under instance id 0,
// which a row's real instance id can never prefix-match. type: "active"
// keeps it to the visible view.
export function scheduleTorrentListRefetches(queryClient: QueryClient, firstDelay: number) {
  const refetchLists = () => {
    queryClient.refetchQueries({
      queryKey: ["torrents-list"],
      exact: false,
      type: "active",
    })
  }
  // Collapsing a burst must not shorten a slower action's pending verification
  // window: a pause right after a delete-with-files would otherwise replace the
  // 5s/8s pair with 1s/4s and leave the last read racing the file deletion.
  // Keep the furthest final deadline; a stale past deadline goes negative and
  // loses the max.
  const finalDelay = Math.max(firstDelay + 3000, pendingFinalRefetchAt - Date.now())
  pendingFinalRefetchAt = Date.now() + finalDelay
  pendingListRefetches.forEach(clearTimeout)
  pendingListRefetches = [
    setTimeout(refetchLists, firstDelay),
    setTimeout(refetchLists, finalDelay),
  ]
}

// Test-only: the debounce state above is module-level and outlives a test's
// fake-timer clock, so a deadline left mid-window would stretch the next
// test's timers.
export function resetPendingListRefetchesForTests() {
  pendingListRefetches.forEach(clearTimeout)
  pendingListRefetches = []
  pendingFinalRefetchAt = 0
}

// Removes the deleted rows from the cached lists of the instance at once, before the refetch.
export function removeDeletedTorrents(queryClient: QueryClient, instanceId: number, variables: {
  hashes: string[]
  targets?: TorrentActionTarget[]
  excludeTargets?: TorrentActionTarget[]
  clientHashes?: string[]
  clientCount?: number
}) {
  const cache = queryClient.getQueryCache()
  const queries = cache.findAll({
    queryKey: ["torrents-list", instanceId],
    exact: false,
  })

  let hashesToRemove = variables.hashes
  if (variables.clientHashes && variables.clientHashes.length > 0) {
    hashesToRemove = variables.clientHashes
  }
  // A target on another instance is not in the list of this instance.
  const otherInstanceCount = instanceId === ALL_INSTANCES_ID ? 0 : variables.targets?.filter(t => t.instanceId !== instanceId).length ?? 0
  const optimisticRemoveCount = (variables.clientCount ?? hashesToRemove.length) - otherInstanceCount
  // Cross-seeded copies share a hash, so a targeted delete removes only the addressed rows.
  const targetKey = (t: { instanceId: number; hash: string }) => `${t.instanceId}:${t.hash}`
  const targetKeys = variables.targets?.length ? new Set(variables.targets.map(targetKey)) : undefined
  const excludedKeys = new Set(variables.excludeTargets?.map(targetKey))

  queries.forEach((query) => {
    queryClient.setQueryData(query.queryKey, (oldData: {
      torrents?: Torrent[]
      total?: number
      totalCount?: number
    }) => {
      if (!oldData) return oldData
      return {
        ...oldData,
        torrents: oldData.torrents?.filter((t: Torrent) => {
          const key = `${getTorrentTargetInstanceId(t, instanceId)}:${t.hash}`
          if (excludedKeys.has(key)) return true
          return targetKeys ? !targetKeys.has(key) : !hashesToRemove.includes(t.hash)
        }) || [],
        total: Math.max(0, (oldData.total || 0) - optimisticRemoveCount),
        totalCount: Math.max(0, (oldData.totalCount || oldData.total || 0) - optimisticRemoveCount),
      }
    })
  })
}

class TagBulkActionError extends Error {
  results: TagBulkActionResult[]

  constructor(results: TagBulkActionResult[]) {
    const succeeded = results.filter(result => result.status === "success").map(result => result.action)
    const failed = results.filter(result => result.status === "failed").map(result => result.action)
    const summary = [
      failed.length > 0 ? `failed to ${failed.join(" and ")}` : "",
      succeeded.length > 0 ? `after ${succeeded.join(" and ")}` : "",
    ].filter(Boolean).join(" ")

    super(summary || "Failed to update tags")
    this.name = "TagBulkActionError"
    this.results = results
  }
}

type TagRequest = Omit<Parameters<typeof api.bulkAction>[1], "action" | "tags">

// The tag update of a tag editor plan: the removal first, then the addition.
// A failure of either step throws a TagBulkActionError that names both steps.
export async function updateTags(scope: number, plan: TagUpdatePlan, request: TagRequest) {
  const results: TagBulkActionResult[] = []

  const runTagBulkAction = async (action: "add" | "remove", tags: string[]) => {
    try {
      await api.bulkAction(scope, {
        ...request,
        action: action === "remove" ? "removeTags" : "addTags",
        tags: tags.join(","),
      })
      results.push({ action, status: "success" })
    } catch (error) {
      results.push({
        action,
        status: "failed",
        error: error instanceof Error ? error : new Error("Unknown tag update failure"),
      })
    }
  }

  if (plan.remove.length > 0) {
    await runTagBulkAction("remove", plan.remove)
  }

  if (plan.add.length > 0) {
    await runTagBulkAction("add", plan.add)
  }

  if (results.some(result => result.status === "failed")) {
    throw new TagBulkActionError(results)
  }
}

// The failure toast of a tag update. It names the step that failed and the step that succeeded.
export function showTagUpdateError(t: Translate, error: Error, count: number) {
  if (error instanceof TagBulkActionError) {
    const succeeded = error.results.filter(result => result.status === "success").map(result => result.action)
    const failed = error.results.filter(result => result.status === "failed")
    const succeededLabel = succeeded.length > 0? t("actionToasts.partialTags.succeeded", {
      actions: succeeded.map((action) => t(`actionToasts.partialTags.actions.${action}`)).join(` ${t("actionToasts.partialTags.and")} `),
    }): ""
    const failedLabel = failed.length > 0? t("actionToasts.partialTags.failed", {
      actions: failed.map((result) => t(`actionToasts.partialTags.actions.${result.action}`)).join(` ${t("actionToasts.partialTags.and")} `),
    }): t("actionToasts.partialTags.tagUpdateFailed")
    const description = failed
      .map(result => result.error?.message)
      .filter((message): message is string => Boolean(message))
      .join("; ")

    toast.error(t("actionToasts.partialTags.title", { count }), {
      description: [succeededLabel, failedLabel, description].filter(Boolean).join(". "),
    })
    return
  }

  toast.error(t("actionToasts.updateTagsFailed", { count }), {
    description: error.message || t("actionToasts.unexpectedError"),
  })
}

export async function invalidateInstanceCaches(queryClient: QueryClient, instanceId: number) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ["torrents-list", instanceId], exact: false }),
    queryClient.invalidateQueries({ queryKey: ["instance-metadata", instanceId], exact: false }),
  ])
}

type Translate = (key: string, options?: Record<string, unknown>) => string

// The delay of the first list refetch after a successful action, other than a delete.
export function actionRefetchDelay(action: TorrentAction) {
  return action === "resume" || action === "forceStart" ? 2000 : 1000
}

// The count that a failure toast names.
export function failedActionCount(request: { hashes: string[]; targets?: TorrentActionTarget[] }) {
  return request.hashes.length || request.targets?.length || 1
}

export function getActionErrorMessage(t: Translate, action: TorrentAction, count: number) {
  switch (action) {
    case "resume":
      return t("actionToasts.failed.resume", { count })
    case "pause":
      return t("actionToasts.failed.pause", { count })
    case "delete":
      return t("actionToasts.failed.delete", { count })
    case "recheck":
      return t("actionToasts.failed.recheck", { count })
    case "reannounce":
      return t("actionToasts.failed.reannounce", { count })
    case "increasePriority":
      return t("actionToasts.failed.increasePriority", { count })
    case "decreasePriority":
      return t("actionToasts.failed.decreasePriority", { count })
    case "topPriority":
      return t("actionToasts.failed.topPriority", { count })
    case "bottomPriority":
      return t("actionToasts.failed.bottomPriority", { count })
    case "addTags":
      return t("actionToasts.failed.addTags", { count })
    case "removeTags":
      return t("actionToasts.failed.removeTags", { count })
    case "setTags":
      return t("actionToasts.failed.setTags", { count })
    case "setCategory":
      return t("actionToasts.failed.setCategory", { count })
    case "toggleAutoTMM":
      return t("actionToasts.failed.toggleAutoTMM", { count })
    case "forceStart":
      return t("actionToasts.failed.forceStart", { count })
    case "setShareLimit":
      return t("actionToasts.failed.setShareLimit", { count })
    case "setUploadLimit":
      return t("actionToasts.failed.setUploadLimit", { count })
    case "setDownloadLimit":
      return t("actionToasts.failed.setDownloadLimit", { count })
    case "setLocation":
      return t("actionToasts.failed.setLocation", { count })
    case "toggleSequentialDownload":
      return t("actionToasts.failed.toggleSequentialDownload", { count })
  }
}

// Helper function for success toasts
export function showSuccessToast(t: Translate, action: TorrentAction, count: number, deleteFiles?: boolean, enable?: boolean) {
  switch (action) {
    case "resume":
      toast.success(t("actionToasts.success.resume", { count }))
      break
    case "pause":
      toast.success(t("actionToasts.success.pause", { count }))
      break
    case "delete":
      toast.success(t(deleteFiles ? "actionToasts.success.deleteWithFiles" : "actionToasts.success.delete", { count }))
      break
    case "recheck":
      toast.success(t("actionToasts.success.recheck", { count }))
      break
    case "reannounce":
      toast.success(t("actionToasts.success.reannounce", { count }))
      break
    case "increasePriority":
      toast.success(t("actionToasts.success.increasePriority", { count }))
      break
    case "decreasePriority":
      toast.success(t("actionToasts.success.decreasePriority", { count }))
      break
    case "topPriority":
      toast.success(t("actionToasts.success.topPriority", { count }))
      break
    case "bottomPriority":
      toast.success(t("actionToasts.success.bottomPriority", { count }))
      break
    case "addTags":
      toast.success(t("actionToasts.success.addTags", { count }))
      break
    case "removeTags":
      toast.success(t("actionToasts.success.removeTags", { count }))
      break
    case "setTags":
      toast.success(t("actionToasts.success.setTags", { count }))
      break
    case "setComment":
      toast.success(t("actionToasts.success.setComment", { count }))
      break
    case "setCategory":
      toast.success(t("actionToasts.success.setCategory", { count }))
      break
    case "toggleAutoTMM":
      toast.success(t(enable ? "actionToasts.success.toggleAutoTMMEnable" : "actionToasts.success.toggleAutoTMMDisable", { count }))
      break
    case "forceStart":
      toast.success(t(enable ? "actionToasts.success.forceStartEnable" : "actionToasts.success.forceStartDisable", { count }))
      break
    case "setShareLimit":
      toast.success(t("actionToasts.success.setShareLimit", { count }))
      break
    case "setUploadLimit":
      toast.success(t("actionToasts.success.setUploadLimit", { count }))
      break
    case "setDownloadLimit":
      toast.success(t("actionToasts.success.setDownloadLimit", { count }))
      break
    case "setLocation":
      toast.success(t("actionToasts.success.setLocation", { count }))
      break
    case "toggleSequentialDownload":
      toast.success(t(enable ? "actionToasts.success.toggleSequentialDownloadEnable" : "actionToasts.success.toggleSequentialDownloadDisable", { count }))
      break
  }
}
