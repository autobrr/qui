/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// qBittorrent download-priority values exposed in its WebUI. libtorrent technically
// uses 0-7, but qBittorrent only surfaces these four; legacy values (2-5) collapse to
// Normal. See discussion #1007.
export const FILE_PRIORITY = {
  doNotDownload: 0,
  normal: 1,
  high: 6,
  maximum: 7,
} as const

export type FilePriorityValue = (typeof FILE_PRIORITY)[keyof typeof FILE_PRIORITY]

export const FILE_PRIORITY_OPTIONS: FilePriorityValue[] = [
  FILE_PRIORITY.doNotDownload,
  FILE_PRIORITY.normal,
  FILE_PRIORITY.high,
  FILE_PRIORITY.maximum,
]

// Collapse any qBittorrent priority (0-7) into one of the four canonical buckets so the
// dropdown always has a concrete value to display.
export function normalizeFilePriority(priority: number): FilePriorityValue {
  if (priority <= 0) return FILE_PRIORITY.doNotDownload
  if (priority >= 7) return FILE_PRIORITY.maximum
  if (priority >= 6) return FILE_PRIORITY.high
  return FILE_PRIORITY.normal
}

// A folder's effective priority: a single value when all descendant files share it,
// otherwise "mixed" (a display-only state, like qBittorrent's WebUI).
export type FolderPriority = FilePriorityValue | "mixed"

export function foldFolderPriority(a: FolderPriority, b: FolderPriority): FolderPriority {
  if (a === "mixed" || b === "mixed") return "mixed"
  return a === b ? a : "mixed"
}

// i18n keys (in the "torrents" namespace) for each priority level.
export const FILE_PRIORITY_LABEL_KEYS: Record<FilePriorityValue, string> = {
  [FILE_PRIORITY.doNotDownload]: "filePriority.doNotDownload",
  [FILE_PRIORITY.normal]: "filePriority.normal",
  [FILE_PRIORITY.high]: "filePriority.high",
  [FILE_PRIORITY.maximum]: "filePriority.maximum",
}
