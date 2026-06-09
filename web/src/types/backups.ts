/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export type BackupRunKind = "manual" | "hourly" | "daily" | "weekly" | "monthly" | "import"

export type BackupRunStatus = "pending" | "running" | "success" | "failed" | "canceled"

export interface BackupSettings {
  instanceId: number
  enabled: boolean
  hourlyEnabled: boolean
  dailyEnabled: boolean
  weeklyEnabled: boolean
  monthlyEnabled: boolean
  keepHourly: number
  keepDaily: number
  keepWeekly: number
  keepMonthly: number
  includeCategories: boolean
  includeTags: boolean
  createdAt?: string
  updatedAt?: string
}

export interface BackupRun {
  id: number
  instanceId: number
  kind: BackupRunKind
  status: BackupRunStatus
  requestedBy: string
  requestedAt: string
  startedAt?: string
  completedAt?: string
  manifestPath?: string | null
  totalBytes: number
  torrentCount: number
  categoryCounts?: Record<string, number>
  categories?: Record<string, BackupCategorySnapshot>
  tags?: string[]
  errorMessage?: string | null
  progressCurrent?: number
  progressTotal?: number
  progressPercentage?: number
}

export interface BackupRunsResponse {
  runs: BackupRun[]
  hasMore: boolean
}

export interface BackupManifestItem {
  hash: string
  name: string
  category?: string | null
  sizeBytes: number
  infohashV1?: string | null
  infohashV2?: string | null
  tags?: string[]
  torrentBlob?: string
}

export interface BackupCategorySnapshot {
  savePath?: string | null
}

export interface BackupManifest {
  instanceId: number
  kind: BackupRunKind
  generatedAt: string
  torrentCount: number
  categories?: Record<string, BackupCategorySnapshot>
  tags?: string[]
  items: BackupManifestItem[]
}

export type RestoreMode = "incremental" | "overwrite" | "complete"

export interface RestorePlanCategorySpec {
  name: string
  savePath?: string | null
}

export interface RestorePlanCategoryUpdate {
  name: string
  currentPath: string
  desiredPath: string
}

export interface RestoreDiffChange {
  field: string
  supported: boolean
  current?: unknown
  desired?: unknown
  message?: string
}

export interface RestorePlanTorrentSpec {
  manifest: BackupManifestItem
}

export interface RestorePlanTorrentUpdate {
  hash: string
  current: {
    hash: string
    name: string
    category: string
    tags: string[]
    trackerUrls?: string[]
    infoHashV1?: string
    infoHashV2?: string
    sizeBytes?: number
  }
  desired: BackupManifestItem & { torrentBlob?: string }
  changes: RestoreDiffChange[]
}

export interface RestorePlan {
  mode: RestoreMode
  runId: number
  instanceId: number
  categories: {
    create?: RestorePlanCategorySpec[]
    update?: RestorePlanCategoryUpdate[]
    delete?: string[]
  }
  tags: {
    create?: { name: string }[]
    delete?: string[]
  }
  torrents: {
    add?: RestorePlanTorrentSpec[]
    update?: RestorePlanTorrentUpdate[]
    delete?: string[]
  }
}

export interface RestoreAppliedCategories {
  created?: string[]
  updated?: string[]
  deleted?: string[]
}

export interface RestoreAppliedTags {
  created?: string[]
  deleted?: string[]
}

export interface RestoreAppliedTorrents {
  added?: string[]
  updated?: string[]
  deleted?: string[]
}

export interface RestoreAppliedTotals {
  categories: RestoreAppliedCategories
  tags: RestoreAppliedTags
  torrents: RestoreAppliedTorrents
}

export interface RestoreErrorItem {
  operation: string
  target: string
  message: string
}

export interface RestoreResult {
  mode: RestoreMode
  runId: number
  instanceId: number
  dryRun: boolean
  plan: RestorePlan
  applied: RestoreAppliedTotals
  warnings?: string[]
  errors?: RestoreErrorItem[]
}
