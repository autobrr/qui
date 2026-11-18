/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TransferProgress, TransferRow, TransferStep } from "@/lib/transfers-api"

/** A finished row holds no torrent: the user can remove it. */
export function canRemove(row: TransferRow): boolean {
  return row.finishedAt !== null
}

/** A run that has not ended can be cancelled; the backend refuses once the source removal started. */
export function canCancel(row: TransferRow): boolean {
  return row.finishedAt === null
}

/** A failed run runs again, also a failure that waits for its retry. A cancelled run runs again once its cleanup ended. */
export function canRetry(row: TransferRow): boolean {
  return row.state === "failed" || (row.state === "cancelled" && row.finishedAt !== null)
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
  "rename": "rename",
  "after": "after",
  "remove from source": "removeFromSource",
  "clean up": "cleanUp",
  "remove the placed files": "removePlaced",
}

/** Steps that qui records only for its own resume, such as the file names of the first run. */
const internalSteps = new Set(["names"])

/** The steps that the Details panel lists. An add that ended shows once, as the step that added the torrent. */
export function shownSteps(steps: TransferStep[]): TransferStep[] {
  const added = new Set(steps.filter(step => step.name === "added").map(step => step.detail))
  return steps.filter(step => !internalSteps.has(step.name) && !(step.name === "add" && added.has(step.detail)))
}

/** The last step of a Transfer names the state that it ended in. */
const endStates = new Set(["done", "failed", "cancelled"])

/** The i18n key of a step name, or null for a step that this version does not know. */
export function stepKey(name: string): string | null {
  if (endStates.has(name)) return `states.${name}`
  return Object.hasOwn(stepKeys, name) ? `steps.${stepKeys[name]}` : null
}
