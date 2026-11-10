/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorrentSelectionSummary } from "@/contexts/TorrentSelectionContext"
import { actionTargetRequest } from "@/lib/torrent-selection"

type QueueAction = "topPriority" | "increasePriority" | "decreasePriority" | "bottomPriority"

// The Bulk action that the user picked on an Action surface, with its option.
// It holds no Action target and no request: the module reads the published target.
export type BulkActionPick =
  | { action: "pause" | "resume" | "recheck" | "reannounce" | QueueAction }
  | { action: "forceStart" | "toggleSequentialDownload" | "toggleAutoTMM"; enable: boolean }

export type BulkDialog =
  | { name: "closed" }
  | { name: "recheck" }
  | { name: "reannounce" }
  | { name: "toggleAutoTMM"; enable: boolean }

type OpenBulkDialog = Exclude<BulkDialog, { name: "closed" }>

export type BulkDialogEvent =
  | { type: "open"; dialog: OpenBulkDialog }
  | { type: "close" }
  | { type: "requestSucceeded"; action: BulkActionPick["action"] }

export const CLOSED_DIALOG: BulkDialog = { name: "closed" }

export function bulkDialogReducer(state: BulkDialog, event: BulkDialogEvent): BulkDialog {
  switch (event.type) {
    case "open":
      return event.dialog
    case "close":
      return CLOSED_DIALOG
    case "requestSucceeded":
      return state.name === event.action ? CLOSED_DIALOG : state
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
    default:
      return { kind: "run", pick }
  }
}

// The pick that the confirm button of an open dialog runs.
export function dialogPick(dialog: OpenBulkDialog): BulkActionPick {
  return dialog.name === "toggleAutoTMM" ? { action: dialog.name, enable: dialog.enable } : { action: dialog.name }
}

// The instance scope of a bulk request on the published Action target: the
// instance of the page, or the all-instances scope in the Unified view, where
// the target names each instance. #3784 and #3737 change this function.
export function requestInstanceId(published: TorrentSelectionSummary): number {
  return published.instanceId
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
