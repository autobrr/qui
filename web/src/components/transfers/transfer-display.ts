/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TransferProgress, TransferRow, TransferStep } from "@/lib/transfers-api"

export function canRemove(row: TransferRow): boolean {
  return row.finishedAt !== null
}

export function canCancel(row: TransferRow): boolean {
  return row.finishedAt === null
}

export function canRetry(row: TransferRow): boolean {
  return row.state === "failed" || (row.state === "cancelled" && row.finishedAt !== null)
}

export function waitsForRetry(row: TransferRow): boolean {
  return row.state === "failed" && row.finishedAt === null && row.retryAt !== null
}

export function percentOf(progress: TransferProgress): number {
  if (progress.total <= 0) return 0
  return Math.min(100, Math.floor((progress.done / progress.total) * 100))
}

export function timeLeftMs(progress: TransferProgress): number | null {
  if (progress.speed <= 0) return null
  return Math.max(0, progress.total - progress.done) / progress.speed * 1000
}

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

const internalSteps = new Set(["names"])

export function shownSteps(steps: TransferStep[]): TransferStep[] {
  const added = new Set(steps.filter(step => step.name === "added").map(step => step.detail))
  return steps.filter(step => !internalSteps.has(step.name) && !(step.name === "add" && added.has(step.detail)))
}

const endStates = new Set(["done", "failed", "cancelled"])

export function stepKey(name: string): string | null {
  if (endStates.has(name)) return `states.${name}`
  return Object.hasOwn(stepKeys, name) ? `steps.${stepKeys[name]}` : null
}
