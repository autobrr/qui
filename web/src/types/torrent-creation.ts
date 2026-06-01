/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Torrent Creation Types
export type TorrentFormat = "v1" | "v2" | "hybrid"
export type TorrentCreationStatus = "Queued" | "Running" | "Finished" | "Failed"

export interface TorrentCreationParams {
  sourcePath: string
  torrentFilePath?: string
  private?: boolean
  format?: TorrentFormat
  optimizeAlignment?: boolean
  paddedFileSizeLimit?: number
  pieceSize?: number
  comment?: string
  source?: string
  trackers?: string[]
  urlSeeds?: string[]
  startSeeding?: boolean
}

export interface TorrentCreationTask {
  taskID: string
  sourcePath: string
  torrentFilePath?: string
  pieceSize: number
  private: boolean
  format?: TorrentFormat
  optimizeAlignment?: boolean
  paddedFileSizeLimit?: number
  status: TorrentCreationStatus
  comment?: string
  source?: string
  trackers?: string[]
  urlSeeds?: string[]
  timeAdded: string
  timeStarted?: string
  timeFinished?: string
  progress?: number
  errorMessage?: string
}

export interface TorrentCreationTaskResponse {
  taskID: string
}
