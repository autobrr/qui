/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Orphan Scan types
export type OrphanScanRunStatus =
  | "pending"
  | "scanning"
  | "preview_ready"
  | "deleting"
  | "completed"
  | "failed"
  | "canceled"

export type OrphanScanTriggerType = "manual" | "scheduled"

export type OrphanScanFileStatus = "pending" | "deleted" | "skipped" | "failed"

export type OrphanScanPreviewSort = "size_desc" | "directory_size_desc"

export interface OrphanScanSettings {
  id?: number
  instanceId: number
  enabled: boolean
  gracePeriodMinutes: number
  ignorePaths: string[]
  scanIntervalHours: number
  previewSort: OrphanScanPreviewSort
  maxFilesPerRun: number
  autoCleanupEnabled: boolean
  autoCleanupMaxFiles: number
  createdAt?: string
  updatedAt?: string
}

export interface OrphanScanSettingsUpdate {
  enabled?: boolean
  gracePeriodMinutes?: number
  ignorePaths?: string[]
  scanIntervalHours?: number
  previewSort?: OrphanScanPreviewSort
  maxFilesPerRun?: number
  autoCleanupEnabled?: boolean
  autoCleanupMaxFiles?: number
}

export interface OrphanScanRun {
  id: number
  instanceId: number
  status: OrphanScanRunStatus
  triggeredBy: OrphanScanTriggerType
  scanPaths: string[]
  filesFound: number
  filesDeleted: number
  foldersDeleted: number
  bytesReclaimed: number
  truncated: boolean
  errorMessage?: string | null
  startedAt: string
  completedAt?: string | null
}

export interface OrphanScanFile {
  id: number
  runId: number
  filePath: string
  fileSize: number
  modifiedAt?: string | null
  status: OrphanScanFileStatus
  errorMessage?: string | null
}

export interface OrphanScanRunWithFiles extends OrphanScanRun {
  files: OrphanScanFile[]
}
