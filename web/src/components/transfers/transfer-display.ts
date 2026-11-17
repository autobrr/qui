/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TransferFilter, TransferProgress, TransferRow, TransferState } from "@/lib/transfers-api"

/** The chip of each state: the backend lists a row behind the same chip. */
const chipOfState: Record<TransferState, Exclude<TransferFilter, "all">> = {
  copying: "active",
  checking: "active",
  finishing: "active",
  waiting: "waiting",
  done: "finished",
  cancelled: "finished",
  failed: "failed",
}

export function chipOf(state: TransferState): Exclude<TransferFilter, "all"> {
  return chipOfState[state]
}

/** A finished row holds no torrent: the user can remove it. */
export function canRemove(row: TransferRow): boolean {
  return row.finishedAt !== null
}

/** A run that has not ended can be cancelled; the backend refuses once the source removal started. */
export function canCancel(row: TransferRow): boolean {
  return row.finishedAt === null
}

/** A failed or cancelled run runs again, also a failure that waits for its retry. */
export function canRetry(row: TransferRow): boolean {
  return row.state === "failed" || row.state === "cancelled"
}

/** A temporary failure that waits: the time of its retry. */
export function waitsForRetry(row: TransferRow): boolean {
  return row.state === "failed" && row.finishedAt === null && row.retryAt !== null
}

/** The share of the bytes that passed, 0 to 100. */
export function percentOf(progress: TransferProgress): number {
  if (progress.total <= 0) return 0
  return Math.min(100, Math.floor((progress.done / progress.total) * 100))
}

/** The time left at the current speed, in milliseconds, or null while qui knows no speed. */
export function timeLeftMs(progress: TransferProgress): number | null {
  if (progress.speed <= 0) return null
  return Math.max(0, progress.total - progress.done) / progress.speed * 1000
}

/** The i18n key below "steps." of each step that the backend records. */
const stepKeys: Record<string, string> = {
  "start": "start",
  "prepare": "prepare",
  "pass by BitTorrent": "passByBitTorrent",
  "passed by BitTorrent": "passedByBitTorrent",
  "fall back to SFTP": "fallBackToSftp",
  "copy a hardlink": "copyHardlink",
  "check on the target": "check",
  "check the files on the target": "checkFiles",
  "copy the missing files": "copyMissing",
  "check the partial files on the target": "checkPartials",
  "copy the pieces that failed the check": "copyFailedPieces",
  "place": "place",
  "add": "add",
  "added": "added",
  "after": "after",
  "remove from source": "removeFromSource",
  "clean up": "cleanUp",
  "remove the placed files": "removePlaced",
}

/** The last step of a Transfer names the state that it ended in. */
const endStates = new Set(["done", "failed", "cancelled"])

/** The i18n key of a step name, or null for a step that this version does not know. */
export function stepKey(name: string): string | null {
  if (endStates.has(name)) return `states.${name}`
  return Object.hasOwn(stepKeys, name) ? `steps.${stepKeys[name]}` : null
}
