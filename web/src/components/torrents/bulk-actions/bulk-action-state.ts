/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorrentSelectionSummary } from "@/contexts/TorrentSelectionContext"
import type { api } from "@/lib/api"
import { ALL_INSTANCES_ID } from "@/lib/instances"
import type { TorrentActionTarget } from "@/lib/torrent-action-targets"
import { actionTargetRequest } from "@/lib/torrent-selection"
import { getTotalSize } from "@/lib/torrent-utils"

type QueueAction = "topPriority" | "increasePriority" | "decreasePriority" | "bottomPriority"

// The Bulk action that the user picked on an Action surface, with its option.
// It holds no Action target and no request: the module reads the published target.
export type BulkActionPick =
  | { action: "pause" | "resume" | "recheck" | "reannounce" | "delete" | QueueAction }
  | { action: "forceStart" | "toggleSequentialDownload" | "toggleAutoTMM"; enable: boolean }

export type BulkDialog =
  | { name: "closed" }
  | { name: "recheck" }
  | { name: "reannounce" }
  | { name: "toggleAutoTMM"; enable: boolean }
  | { name: "delete"; deleteCrossSeeds: boolean }

type OpenBulkDialog = Exclude<BulkDialog, { name: "closed" }>
type ConfirmationDialog = Exclude<OpenBulkDialog, { name: "delete" }>

export type BulkDialogEvent =
  | { type: "open"; dialog: OpenBulkDialog }
  | { type: "close" }
  | { type: "requestSucceeded"; action: BulkActionPick["action"] }
  | { type: "deleteCrossSeeds"; checked: boolean }

export const CLOSED_DIALOG: BulkDialog = { name: "closed" }

export function bulkDialogReducer(state: BulkDialog, event: BulkDialogEvent): BulkDialog {
  switch (event.type) {
    case "open":
      return event.dialog
    case "close":
      return CLOSED_DIALOG
    case "requestSucceeded":
      return state.name === event.action ? CLOSED_DIALOG : state
    case "deleteCrossSeeds":
      return state.name === "delete" ? { ...state, deleteCrossSeeds: event.checked } : state
  }
}

export type PickOutcome = { kind: "run"; pick: BulkActionPick } | { kind: "open"; dialog: OpenBulkDialog }

// count is the count of the Action target. One torrent needs no confirmation.
export function resolvePick(pick: BulkActionPick, count: number): PickOutcome {
  switch (pick.action) {
    case "recheck":
    case "reannounce":
      return count > 1 ? { kind: "open", dialog: { name: pick.action } } : { kind: "run", pick }
    case "toggleAutoTMM":
      return { kind: "open", dialog: { name: pick.action, enable: pick.enable } }
    case "delete":
      return { kind: "open", dialog: { name: pick.action, deleteCrossSeeds: false } }
    default:
      return { kind: "run", pick }
  }
}

// The pick that the confirm button of an open dialog runs.
export function dialogPick(dialog: ConfirmationDialog): BulkActionPick {
  return dialog.name === "toggleAutoTMM" ? { action: dialog.name, enable: dialog.enable } : { action: dialog.name }
}

// The instance scope of a bulk request: the instance of the page, or the
// all-instances scope in the Unified view, where the target names each
// instance. A request for targets on another instance goes to the
// all-instances scope. #3784 and #3737 change this function.
export function requestInstanceId(published: TorrentSelectionSummary, targets: TorrentActionTarget[] = []): number {
  return targets.some(target => target.instanceId !== published.instanceId) ? ALL_INSTANCES_ID : published.instanceId
}

// The request of a pick on the published Action target (ADR 0034), with the
// count for the toast. A Select all target carries its own instance subset.
export function bulkRequest(pick: BulkActionPick, published: TorrentSelectionSummary) {
  const { actionTarget, instanceIds } = published
  return {
    scope: requestInstanceId(published),
    count: actionTarget.count,
    body: { ...pick, instanceIds, ...actionTargetRequest(actionTarget) },
  }
}

export interface DeleteRequest {
  scope: number
  count: number
  clientHashes: string[]
  body: Parameters<typeof api.bulkAction>[1]
}

// The requests of a delete of the published Action target with the chosen
// cross-seeds, in order, with the hashes and the count for the optimistic
// removal and the toast. A Select all request cannot list hashes, and a
// request on one instance leaves out targets on other instances, so these
// cross-seeds go in a second request.
export function deleteRequests(published: TorrentSelectionSummary, crossSeedTargets: TorrentActionTarget[], deleteFiles: boolean): DeleteRequest[] {
  const { actionTarget, instanceIds } = published
  const crossSeedScope = requestInstanceId(published, crossSeedTargets)
  const merged = actionTarget.kind === "selectAll" || crossSeedScope !== published.instanceId ? [] : crossSeedTargets
  const mergedHashes = merged.map(target => target.hash)
  const request = actionTargetRequest(actionTarget)
  const targetHashes = actionTarget.kind === "selectAll" ? actionTarget.previewTorrents.map(torrent => torrent.hash) : actionTarget.targets?.map(target => target.hash) ?? actionTarget.hashes
  const requests: DeleteRequest[] = [{
    scope: requestInstanceId(published),
    count: actionTarget.count + mergedHashes.length,
    clientHashes: [...targetHashes, ...mergedHashes],
    body: {
      action: "delete",
      deleteFiles,
      instanceIds,
      ...request,
      hashes: request.targets ? request.hashes : [...request.hashes, ...mergedHashes],
      targets: request.targets && [...request.targets, ...merged],
    },
  }]
  // The cross-seed warning leaves out every selected torrent, so the second
  // delete never repeats a torrent of the Select all delete (#3341).
  if (merged.length === 0 && crossSeedTargets.length > 0) {
    requests.push({
      scope: crossSeedScope,
      count: crossSeedTargets.length,
      clientHashes: crossSeedTargets.map(target => target.hash),
      body: { action: "delete", deleteFiles, instanceIds, hashes: [], targets: crossSeedTargets },
    })
  }
  return requests
}

// The size that the delete dialog shows. With no total size for the filter,
// it is the size of the loaded torrents of the Action target.
export function deleteSize({ actionTarget, totalSize, excludedSize }: TorrentSelectionSummary): number {
  const size = actionTarget.kind === "selectAll" ? Math.max(totalSize - excludedSize, 0) : getTotalSize(actionTarget.previewTorrents)
  return size > 0 ? size : getTotalSize(actionTarget.previewTorrents)
}
