/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Directory Scanner Types
export type DirScanMatchMode = "strict" | "flexible"

export type DirScanFileStatus =
  | "pending"
  | "matched"
  | "no_match"
  | "error"
  | "already_seeding"
  | "in_qbittorrent"

export type DirScanRunStatus =
  | "queued"
  | "scanning"
  | "searching"
  | "injecting"
  | "success"
  | "failed"
  | "canceled"

export interface DirScanSettings {
  id: number
  enabled: boolean
  matchMode: DirScanMatchMode
  sizeTolerancePercent: number
  minPieceRatio: number
  maxSearcheesPerRun: number
  maxSearcheeAgeDays: number
  allowPartial: boolean
  skipPieceBoundarySafetyCheck: boolean
  startPaused: boolean
  downloadMissingFiles: boolean
  category: string
  tags: string[]
  createdAt: string
  updatedAt: string
}

export interface DirScanSettingsUpdate {
  enabled?: boolean
  matchMode?: DirScanMatchMode
  sizeTolerancePercent?: number
  minPieceRatio?: number
  maxSearcheesPerRun?: number
  maxSearcheeAgeDays?: number
  allowPartial?: boolean
  skipPieceBoundarySafetyCheck?: boolean
  startPaused?: boolean
  downloadMissingFiles?: boolean
  category?: string
  tags?: string[]
}

export interface DirScanDirectory {
  id: number
  path: string
  qbitPathPrefix?: string
  category?: string
  tags: string[]
  allowedDownloadClients: string[]
  enabled: boolean
  arrInstanceId?: number
  targetInstanceId: number
  scanIntervalMinutes: number
  lastScanAt?: string
  createdAt: string
  updatedAt: string
}

export interface DirScanDirectoryCreate {
  path: string
  qbitPathPrefix?: string
  category?: string
  tags?: string[]
  allowedDownloadClients?: string[]
  enabled?: boolean
  arrInstanceId?: number
  targetInstanceId: number
  scanIntervalMinutes?: number
}

export interface DirScanDirectoryUpdate {
  path?: string
  qbitPathPrefix?: string
  category?: string
  tags?: string[]
  allowedDownloadClients?: string[]
  enabled?: boolean
  arrInstanceId?: number
  targetInstanceId?: number
  scanIntervalMinutes?: number
}

export interface DirScanRun {
  id: number
  directoryId: number
  status: DirScanRunStatus
  triggeredBy: string
  scanRoot?: string
  filesFound: number
  filesSkipped: number
  matchesFound: number
  torrentsAdded: number
  errorMessage?: string
  startedAt: string
  completedAt?: string
}

export interface DirScanTriggerResponse {
  runId: number
  directoryId: number
  directoryPath: string
  scanRoot: string
}

export type DirScanRunInjectionStatus = "added" | "failed"

export interface DirScanRunInjection {
  id: number
  runId: number
  directoryId: number
  status: DirScanRunInjectionStatus
  searcheeName: string
  torrentName: string
  infoHash: string
  contentType: string
  indexerName?: string
  trackerDomain?: string
  trackerDisplayName?: string
  linkMode?: string
  savePath?: string
  category?: string
  tags: string[]
  errorMessage?: string
  createdAt: string
}

export interface DirScanFile {
  id: number
  directoryId: number
  filePath: string
  fileSize: number
  fileModTime: string
  status: DirScanFileStatus
  matchedTorrentHash?: string
  matchedIndexerId?: number
  lastProcessedAt?: string
}
