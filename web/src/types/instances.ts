/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// How qui reaches an instance's torrent data. Remote is SSH with a confirmed host key.
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
  // SSH access; the key itself never reaches the browser.
  sshHost?: string
  sshPort?: number
  sshUsername?: string
  sshHostKeyPinned?: boolean
  // What the key lets qui run on the host, as the last probe found. Absent without credentials.
  sshTier?: SSHTier
  // Whether the host advertises hardlink@openssh.com. Absent until a probe answers.
  sshHardlink?: boolean
  // Hardlink mode settings (per-instance)
  useHardlinks: boolean
  hardlinkBaseDir: string
  hardlinkDirPreset: "flat" | "by-tracker" | "by-instance"
  // Reflink mode (copy-on-write) - mutually exclusive with hardlink mode
  useReflinks: boolean
  // Fallback to regular mode when reflink/hardlink fails
  fallbackToRegularMode: boolean
  sortOrder: number
  isActive: boolean
  reannounceSettings: InstanceReannounceSettings
}

export type SSHTier = "sftp" | "exec"

export interface SSHCredentials {
  host: string
  port: number
  username: string
  privateKey: string
}

export type SSHTestStatus = "unpinned" | "pinned" | "mismatch" | "pin_unreadable" | "error"

// POST /instances/{id}/ssh-test. hostKey is echoed back to pin it.
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

// What qui can do with an instance's files, from models.FilesystemCapabilitiesOf.
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
  // Hardlink mode settings (per-instance)
  useHardlinks?: boolean
  hardlinkBaseDir?: string
  hardlinkDirPreset?: "flat" | "by-tracker" | "by-instance"
  // Reflink mode (copy-on-write) - mutually exclusive with hardlink mode
  useReflinks?: boolean
  // Fallback to regular mode when reflink/hardlink fails
  fallbackToRegularMode?: boolean
  reannounceSettings: InstanceReannounceSettings
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

// Reannounce settings constraints - shared across components
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

/**
 * A torrent match found by the backend using proper release metadata parsing (rls library).
 */
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

/** An instance with local filesystem access that the delete check did not read. */
export interface UncheckedInstance {
  instanceId: number
  instanceName: string
  /** offline: qui cannot read its torrents. disabled: it is turned off in qui. */
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
