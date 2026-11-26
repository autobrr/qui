/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export type FilesystemMode = "none" | "local" | "remote"

export interface Instance {
  id: number
  name: string
  host: string
  username: string
  hasApiKey?: boolean
  basicUsername?: string
  tlsSkipVerify: boolean
  hasLocalFilesystemAccess: boolean
  capabilities: FilesystemCapabilities
  filesystemMode: FilesystemMode
  sshHost?: string
  sshPort?: number
  sshUsername?: string
  sshHostKeyPinned?: boolean
  sshTier?: SSHTier
  sshHardlink?: boolean
  useHardlinks: boolean
  hardlinkBaseDir: string
  hardlinkDirPreset: "flat" | "by-tracker" | "by-instance"
  useReflinks: boolean
  fallbackToRegularMode: boolean
  sortOrder: number
  isActive: boolean
  reannounceSettings: InstanceReannounceSettings
  publicPeerAddress?: string
  resolvedPublicPeerAddress?: string
}

export type SSHTier = "sftp" | "exec"

export interface SSHCredentials {
  host: string
  port: number
  username: string
  privateKey: string
}

export type SSHTestStatus = "unpinned" | "pinned" | "mismatch" | "pin_unreadable" | "error"

export interface SSHTestResponse {
  status: SSHTestStatus
  hostKey?: string
  fingerprint?: string
  keyType?: string
  pinnedFingerprint?: string
  pinnedKeyType?: string
  capabilities?: SSHProbe
  error?: string
}

export interface SSHProbe {
  sftp: boolean
  statvfs: boolean
  hardlink: boolean
  limits: boolean
  exec: boolean
  gnuUserland: boolean
}

export interface FilesystemCapabilities {
  read: boolean
  identity: boolean
  trustedIdentity: boolean
  write: boolean
  content: boolean
}

export interface InstanceFormData {
  name: string
  host: string
  username?: string
  password?: string
  apiKey?: string
  basicUsername?: string
  basicPassword?: string
  tlsSkipVerify: boolean
  hasLocalFilesystemAccess: boolean
  useHardlinks?: boolean
  hardlinkBaseDir?: string
  hardlinkDirPreset?: "flat" | "by-tracker" | "by-instance"
  useReflinks?: boolean
  fallbackToRegularMode?: boolean
  reannounceSettings: InstanceReannounceSettings
  publicPeerAddress?: string
}

export interface InstanceReannounceSettings {
  enabled: boolean
  initialWaitSeconds: number
  reannounceIntervalSeconds: number
  maxAgeSeconds: number
  maxRetries: number
  aggressive: boolean
  monitorAll: boolean
  excludeCategories: boolean
  categories: string[]
  excludeTags: boolean
  tags: string[]
  excludeTrackers: boolean
  trackers: string[]
}

export const REANNOUNCE_CONSTRAINTS = {
  MIN_INITIAL_WAIT: 5,
  MIN_INTERVAL: 5,
  MIN_MAX_AGE: 60,
  MIN_MAX_RETRIES: 1,
  MAX_MAX_RETRIES: 50,
  DEFAULT_MAX_RETRIES: 50,
} as const

export interface InstanceCrossSeedCompletionSettings {
  instanceId: number
  enabled: boolean
  categories: string[]
  tags: string[]
  excludeCategories: string[]
  excludeTags: string[]
  indexerIds: number[]
  bypassTorznabCache: boolean
  delaySeconds: number
}

export interface LocalCrossSeedMatch {
  instanceId: number
  instanceName: string
  hash: string
  name: string
  size: number
  progress: number
  savePath: string
  contentPath: string
  category: string
  tags: string
  state: string
  tracker: string
  trackerHealth?: string
  matchType: "content_path" | "hardlink" | "reflink" | "name" | "release"
}

export interface UncheckedInstance {
  instanceId: number
  instanceName: string
  reason: "offline" | "disabled"
}

export interface InstanceReannounceActivity {
  instanceId: number
  hash: string
  torrentName?: string
  trackers?: string
  outcome: "skipped" | "failed" | "succeeded" | "started"
  reason?: string
  timestamp: string
}

export interface InstanceReannounceCandidate {
  instanceId: number
  hash: string
  torrentName?: string
  trackers?: string
  timeActiveSeconds?: number
  category?: string
  tags?: string
  state: "watching" | "reannouncing" | "cooldown"
  hasTrackerProblem: boolean
  waitingForInitial: boolean
}

export interface InstanceError {
  id: number
  instanceId: number
  errorType: string
  errorMessage: string
  occurredAt: string
}

export interface InstanceResponse extends Instance {
  connected: boolean
  hasDecryptionError: boolean
  recentErrors?: InstanceError[]
  connectionStatus?: string
}

export interface InstanceCapabilities {
  supportsTorrentCreation: boolean
  supportsTorrentExport: boolean
  supportsSetTags: boolean
  supportsSetComment: boolean
  supportsTrackerHealth: boolean
  supportsTrackerEditing: boolean
  supportsRenameTorrent: boolean
  supportsRenameFile: boolean
  supportsRenameFolder: boolean
  supportsFilePriority: boolean
  supportsSubcategories: boolean
  subcategoriesAlwaysEnabled: boolean
  supportsTorrentTmpPath: boolean
  supportsPathAutocomplete: boolean
  supportsTestEmail: boolean
  supportsFreeSpacePathSource: boolean
  supportsSetRSSFeedURL: boolean
  supportsShareLimitsAction: boolean
  supportsShareLimitsMode?: boolean
  supportsFreeSpaceAtPath?: boolean
  supportsLiveReannounce: boolean
  supportsListFieldsV5: boolean
  supportsListCreatedBy: boolean
  webAPIVersion?: string
}
