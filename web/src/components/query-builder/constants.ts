/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Field definitions with metadata for the query builder UI
export const CONDITION_FIELDS = {
  // String fields
  NAME: { label: "Name", type: "string" as const, description: "Torrent name" },
  HASH: { label: "Hash", type: "string" as const, description: "Torrent info hash" },
  INFOHASH_V1: { label: "Infohash v1", type: "string" as const, description: "BitTorrent v1 info hash" },
  INFOHASH_V2: { label: "Infohash v2", type: "string" as const, description: "BitTorrent v2 info hash" },
  MAGNET_URI: { label: "Magnet URI", type: "string" as const, description: "Magnet link for the torrent" },
  CATEGORY: { label: "Category", type: "string" as const, description: "Torrent category" },
  TAGS: { label: "Tags", type: "string" as const, description: "Comma-separated tags" },
  SAVE_PATH: { label: "Save Path", type: "string" as const, description: "Download location" },
  CONTENT_PATH: { label: "Content Path", type: "string" as const, description: "Content location" },
  DOWNLOAD_PATH: { label: "Download Path", type: "string" as const, description: "Session download path from qBittorrent" },
  CREATED_BY: { label: "Created By", type: "string" as const, description: "Torrent creator metadata" },
  TRACKERS: { label: "Trackers (All)", type: "string" as const, description: "All tracker URLs/domains/display names for this torrent" },
  CONTENT_TYPE: { label: "Content Type", type: "string" as const, description: "Detected content type (movie, tv, music, etc) from release parsing" },
  EFFECTIVE_NAME: { label: "Effective Name", type: "string" as const, description: "Parsed item key (title/year or SxxEyy) for grouping across trackers" },
  RLS_SOURCE: { label: "Source (RLS)", type: "string" as const, description: "Parsed source (normalized: WEBDL, WEBRIP, BLURAY, etc)" },
  RLS_RESOLUTION: { label: "Resolution (RLS)", type: "string" as const, description: "Parsed resolution (e.g. 1080P, 2160P)" },
  RLS_CODEC: { label: "Codec (RLS)", type: "string" as const, description: "Parsed video codec (normalized: AVC, HEVC, etc)" },
  RLS_HDR: { label: "HDR (RLS)", type: "string" as const, description: "Parsed HDR tags (e.g. DV, HDR10, HDR)" },
  RLS_AUDIO: { label: "Audio (RLS)", type: "string" as const, description: "Parsed audio tags (e.g. DTS, TRUEHD, AAC)" },
  RLS_CHANNELS: { label: "Channels (RLS)", type: "string" as const, description: "Parsed audio channels (e.g. 5.1, 7.1)" },
  RLS_GROUP: { label: "Group (RLS)", type: "string" as const, description: "Parsed release group (e.g. NTb, FLUX, FraMeSToR)" },
  STATE: { label: "State", type: "state" as const, description: "Torrent status (matches sidebar filters)" },
  TRACKER: { label: "Tracker", type: "string" as const, description: "Primary tracker (URL, domain, or display name)" },
  COMMENT: { label: "Comment", type: "string" as const, description: "Torrent comment" },

  // Size fields (bytes)
  SIZE: { label: "Size", type: "bytes" as const, description: "Selected file size" },
  TOTAL_SIZE: { label: "Total Size", type: "bytes" as const, description: "Total torrent size" },
  COMPLETED: { label: "Completed", type: "bytes" as const, description: "Completed bytes" },
  DOWNLOADED: { label: "Downloaded", type: "bytes" as const, description: "Total downloaded" },
  DOWNLOADED_SESSION: { label: "Downloaded (Session)", type: "bytes" as const, description: "Downloaded in current session" },
  UPLOADED: { label: "Uploaded", type: "bytes" as const, description: "Total uploaded" },
  UPLOADED_SESSION: { label: "Uploaded (Session)", type: "bytes" as const, description: "Uploaded in current session" },
  AMOUNT_LEFT: { label: "Amount Left", type: "bytes" as const, description: "Remaining to download" },
  FREE_SPACE: { label: "Free Space", type: "bytes" as const, description: "Free space on the instance's filesystem" },

  // Timestamp-backed fields represented as ages (seconds since event)
  ADDED_ON: { label: "Added Age", type: "duration" as const, description: "Time since torrent was added" },
  COMPLETION_ON: { label: "Completed Age", type: "duration" as const, description: "Time since download completed" },
  LAST_ACTIVITY: { label: "Inactive Time", type: "duration" as const, description: "Time since last activity" },
  SEEN_COMPLETE: { label: "Seen Complete Age", type: "duration" as const, description: "Time since torrent was last seen complete" },

  // Duration fields (seconds)
  ETA: { label: "ETA", type: "duration" as const, description: "Estimated seconds to completion" },
  REANNOUNCE: { label: "Reannounce In", type: "duration" as const, description: "Seconds until next reannounce" },
  SEEDING_TIME: { label: "Seeding Time", type: "duration" as const, description: "Time spent seeding" },
  TIME_ACTIVE: { label: "Time Active", type: "duration" as const, description: "Total active time" },
  MAX_SEEDING_TIME: { label: "Max Seeding Time", type: "duration" as const, description: "Configured max seeding time" },
  MAX_INACTIVE_SEEDING_TIME: { label: "Max Inactive Seeding Time", type: "duration" as const, description: "Configured max inactive seeding time" },
  SEEDING_TIME_LIMIT: { label: "Seeding Time Limit", type: "duration" as const, description: "Torrent seeding time limit" },
  INACTIVE_SEEDING_TIME_LIMIT: { label: "Inactive Seeding Time Limit", type: "duration" as const, description: "Torrent inactive seeding time limit" },
  ADDED_ON_AGE: { label: "Added Age (legacy)", type: "duration" as const, description: "Legacy alias for Added Age" },
  COMPLETION_ON_AGE: { label: "Completed Age (legacy)", type: "duration" as const, description: "Legacy alias for Completed Age" },
  LAST_ACTIVITY_AGE: { label: "Inactive Time (legacy)", type: "duration" as const, description: "Legacy alias for Inactive Time" },

  // Float fields
  RATIO: { label: "Ratio", type: "float" as const, description: "Upload/download ratio" },
  RATIO_LIMIT: { label: "Ratio Limit", type: "float" as const, description: "Configured ratio limit" },
  MAX_RATIO: { label: "Max Ratio", type: "float" as const, description: "Maximum ratio value from qBittorrent" },
  PROGRESS: { label: "Progress", type: "percentage" as const, description: "Download progress (0-100%)" },
  AVAILABILITY: { label: "Availability", type: "float" as const, description: "Distributed copies" },
  POPULARITY: { label: "Popularity", type: "float" as const, description: "Swarm popularity metric" },

  // Speed fields (bytes/s)
  DL_SPEED: { label: "Download Speed", type: "speed" as const, description: "Current download speed" },
  UP_SPEED: { label: "Upload Speed", type: "speed" as const, description: "Current upload speed" },
  DL_LIMIT: { label: "Download Limit", type: "speed" as const, description: "Configured download speed limit" },
  UP_LIMIT: { label: "Upload Limit", type: "speed" as const, description: "Configured upload speed limit" },

  // Count fields
  NUM_SEEDS: { label: "Active Seeders", type: "integer" as const, description: "Seeders currently connected to" },
  NUM_LEECHS: { label: "Active Leechers", type: "integer" as const, description: "Leechers currently connected to" },
  NUM_COMPLETE: { label: "Total Seeders", type: "integer" as const, description: "Total seeders in swarm (tracker-reported)" },
  NUM_INCOMPLETE: { label: "Total Leechers", type: "integer" as const, description: "Total leechers in swarm (tracker-reported)" },
  TRACKERS_COUNT: { label: "Trackers", type: "integer" as const, description: "Number of trackers" },
  PRIORITY: { label: "Queue Priority", type: "integer" as const, description: "Torrent queue priority value" },
  GROUP_SIZE: { label: "Group Size", type: "integer" as const, description: "Number of torrents in the selected group for this condition" },

  // Boolean fields
  PRIVATE: { label: "Private", type: "boolean" as const, description: "Private tracker torrent" },
  AUTO_MANAGED: { label: "Auto-managed", type: "boolean" as const, description: "Managed by automatic torrent management" },
  FIRST_LAST_PIECE_PRIO: { label: "First/Last Piece Priority", type: "boolean" as const, description: "First and last pieces are prioritized" },
  FORCE_START: { label: "Force Start", type: "boolean" as const, description: "Ignores queue limits and starts immediately" },
  SEQUENTIAL_DOWNLOAD: { label: "Sequential Download", type: "boolean" as const, description: "Downloads pieces sequentially" },
  SUPER_SEEDING: { label: "Super Seeding", type: "boolean" as const, description: "Super-seeding mode enabled" },
  IS_UNREGISTERED: { label: "Unregistered", type: "boolean" as const, description: "Tracker reports torrent as unregistered" },
  HAS_MISSING_FILES: { label: "Has Missing Files", type: "boolean" as const, description: "Completed torrent has files missing on disk. Requires Local Filesystem Access." },
  IS_GROUPED: { label: "Is Grouped", type: "boolean" as const, description: "True when group size > 1 for the selected group in this condition" },

  // Enum-like fields
  HARDLINK_SCOPE: { label: "Hardlink scope", type: "hardlinkScope" as const, description: "Where hardlinks for this torrent's files exist. Requires Local Filesystem Access." },
} as const;

export type FieldType = "string" | "state" | "bytes" | "duration" | "float" | "percentage" | "speed" | "integer" | "boolean" | "hardlinkScope";

// Operators available per field type
export const OPERATORS_BY_TYPE: Record<FieldType, { value: string; label: string }[]> = {
  string: [
    { value: "EQUAL", label: "equals" },
    { value: "NOT_EQUAL", label: "not equals" },
    { value: "CONTAINS", label: "contains" },
    { value: "NOT_CONTAINS", label: "not contains" },
    { value: "STARTS_WITH", label: "starts with" },
    { value: "ENDS_WITH", label: "ends with" },
    { value: "MATCHES", label: "matches regex" },
  ],
  state: [
    { value: "EQUAL", label: "is" },
    { value: "NOT_EQUAL", label: "is not" },
  ],
  bytes: [
    { value: "EQUAL", label: "=" },
    { value: "NOT_EQUAL", label: "!=" },
    { value: "GREATER_THAN", label: ">" },
    { value: "GREATER_THAN_OR_EQUAL", label: ">=" },
    { value: "LESS_THAN", label: "<" },
    { value: "LESS_THAN_OR_EQUAL", label: "<=" },
    { value: "BETWEEN", label: "between" },
  ],
  duration: [
    { value: "EQUAL", label: "=" },
    { value: "NOT_EQUAL", label: "!=" },
    { value: "GREATER_THAN", label: ">" },
    { value: "GREATER_THAN_OR_EQUAL", label: ">=" },
    { value: "LESS_THAN", label: "<" },
    { value: "LESS_THAN_OR_EQUAL", label: "<=" },
    { value: "BETWEEN", label: "between" },
  ],
  float: [
    { value: "EQUAL", label: "=" },
    { value: "NOT_EQUAL", label: "!=" },
    { value: "GREATER_THAN", label: ">" },
    { value: "GREATER_THAN_OR_EQUAL", label: ">=" },
    { value: "LESS_THAN", label: "<" },
    { value: "LESS_THAN_OR_EQUAL", label: "<=" },
    { value: "BETWEEN", label: "between" },
  ],
  percentage: [
    { value: "EQUAL", label: "=" },
    { value: "NOT_EQUAL", label: "!=" },
    { value: "GREATER_THAN", label: ">" },
    { value: "GREATER_THAN_OR_EQUAL", label: ">=" },
    { value: "LESS_THAN", label: "<" },
    { value: "LESS_THAN_OR_EQUAL", label: "<=" },
    { value: "BETWEEN", label: "between" },
  ],
  speed: [
    { value: "EQUAL", label: "=" },
    { value: "NOT_EQUAL", label: "!=" },
    { value: "GREATER_THAN", label: ">" },
    { value: "GREATER_THAN_OR_EQUAL", label: ">=" },
    { value: "LESS_THAN", label: "<" },
    { value: "LESS_THAN_OR_EQUAL", label: "<=" },
    { value: "BETWEEN", label: "between" },
  ],
  integer: [
    { value: "EQUAL", label: "=" },
    { value: "NOT_EQUAL", label: "!=" },
    { value: "GREATER_THAN", label: ">" },
    { value: "GREATER_THAN_OR_EQUAL", label: ">=" },
    { value: "LESS_THAN", label: "<" },
    { value: "LESS_THAN_OR_EQUAL", label: "<=" },
    { value: "BETWEEN", label: "between" },
  ],
  boolean: [
    { value: "EQUAL", label: "is" },
    { value: "NOT_EQUAL", label: "is not" },
  ],
  hardlinkScope: [
    { value: "EQUAL", label: "is" },
    { value: "NOT_EQUAL", label: "is not" },
  ],
};

// Hardlink scope values (matches backend wire format)
export const HARDLINK_SCOPE_VALUES = [
  { value: "none", label: "None", labelKey: "leafCondition.hardlinkScope.none" },
  { value: "torrents_only", label: "Only other torrents", labelKey: "leafCondition.hardlinkScope.torrentsOnly" },
  { value: "outside_qbittorrent", label: "Outside qBittorrent (library/import)", labelKey: "leafCondition.hardlinkScope.outsideQBittorrent" },
];

// qBittorrent torrent states
export const TORRENT_STATES = [
  // Status buckets (same as sidebar)
  { value: "downloading", label: "Downloading", labelKey: "leafCondition.torrentState.downloading" },
  { value: "uploading", label: "Seeding", labelKey: "leafCondition.torrentState.uploading" },
  { value: "completed", label: "Completed", labelKey: "leafCondition.torrentState.completed" },
  { value: "stopped", label: "Stopped", labelKey: "leafCondition.torrentState.stopped" },
  { value: "active", label: "Active", labelKey: "leafCondition.torrentState.active" },
  { value: "inactive", label: "Inactive", labelKey: "leafCondition.torrentState.inactive" },
  { value: "running", label: "Running", labelKey: "leafCondition.torrentState.running" },
  { value: "stalled", label: "Stalled", labelKey: "leafCondition.torrentState.stalled" },
  { value: "stalled_uploading", label: "Stalled Up", labelKey: "leafCondition.torrentState.stalledUploading" },
  { value: "stalled_downloading", label: "Stalled Down", labelKey: "leafCondition.torrentState.stalledDownloading" },
  { value: "errored", label: "Error", labelKey: "leafCondition.torrentState.error" },
  { value: "tracker_down", label: "Tracker Down", labelKey: "leafCondition.torrentState.trackerDown" },
  { value: "checking", label: "Checking", labelKey: "leafCondition.torrentState.checking" },
  { value: "checkingResumeData", label: "Checking Resume Data", labelKey: "leafCondition.torrentState.checkingResumeData" },
  { value: "moving", label: "Moving", labelKey: "leafCondition.torrentState.moving" },

  // Specific qBittorrent state (kept for targeting missing-file issues)
  { value: "missingFiles", label: "Missing Files", labelKey: "leafCondition.torrentState.missingFiles" },
];

// Delete mode options
export const DELETE_MODES = [
  { value: "delete", label: "Remove from client" },
  { value: "deleteWithFiles", label: "Remove with files" },
  { value: "deleteWithFilesPreserveCrossSeeds", label: "Remove with files (preserve cross-seeds)" },
  { value: "deleteWithFilesIncludeCrossSeeds", label: "Remove with files (include cross-seeds)" },
];

// Field groups for organized selection
export const FIELD_GROUPS = [
  {
    label: "Identity",
    fields: ["NAME", "HASH", "INFOHASH_V1", "INFOHASH_V2", "MAGNET_URI", "CATEGORY", "TAGS", "STATE", "CREATED_BY"],
  },
  {
    label: "Release",
    fields: ["CONTENT_TYPE", "EFFECTIVE_NAME", "RLS_SOURCE", "RLS_RESOLUTION", "RLS_CODEC", "RLS_HDR", "RLS_AUDIO", "RLS_CHANNELS", "RLS_GROUP"],
  },
  {
    label: "Grouping",
    fields: ["GROUP_SIZE", "IS_GROUPED"],
  },
  {
    label: "Paths",
    fields: ["SAVE_PATH", "CONTENT_PATH", "DOWNLOAD_PATH"],
  },
  {
    label: "Size",
    fields: ["SIZE", "TOTAL_SIZE", "COMPLETED", "DOWNLOADED", "DOWNLOADED_SESSION", "UPLOADED", "UPLOADED_SESSION", "AMOUNT_LEFT", "FREE_SPACE"],
  },
  {
    label: "Time",
    fields: ["ADDED_ON", "COMPLETION_ON", "LAST_ACTIVITY", "SEEN_COMPLETE", "ETA", "REANNOUNCE", "SEEDING_TIME", "TIME_ACTIVE", "MAX_SEEDING_TIME", "MAX_INACTIVE_SEEDING_TIME", "SEEDING_TIME_LIMIT", "INACTIVE_SEEDING_TIME_LIMIT"],
  },
  {
    label: "Progress",
    fields: ["RATIO", "RATIO_LIMIT", "MAX_RATIO", "PROGRESS", "AVAILABILITY", "POPULARITY"],
  },
  {
    label: "Speed",
    fields: ["DL_SPEED", "UP_SPEED", "DL_LIMIT", "UP_LIMIT"],
  },
  {
    label: "Peers",
    fields: ["NUM_SEEDS", "NUM_LEECHS", "NUM_COMPLETE", "NUM_INCOMPLETE", "PRIORITY"],
  },
  {
    label: "Tracker",
    fields: ["TRACKER", "TRACKERS", "TRACKERS_COUNT", "PRIVATE", "IS_UNREGISTERED", "COMMENT"],
  },
  {
    label: "Mode",
    fields: ["AUTO_MANAGED", "FIRST_LAST_PIECE_PRIO", "FORCE_START", "SEQUENTIAL_DOWNLOAD", "SUPER_SEEDING"],
  },
  {
    label: "Files",
    fields: ["HARDLINK_SCOPE", "HAS_MISSING_FILES"],
  },
];

// Helper to get field type
export function getFieldType(field: string): FieldType {
  const fieldDef = CONDITION_FIELDS[field as keyof typeof CONDITION_FIELDS];
  return fieldDef?.type ?? "string";
}

// Special operators only available for NAME field (cross-category lookups)
export const NAME_SPECIAL_OPERATORS = [
  { value: "EXISTS_IN", label: "exists in" },
  { value: "CONTAINS_IN", label: "similar exists in" },
];

// Helper to get operators for a field
export function getOperatorsForField(field: string) {
  const type = getFieldType(field);
  const baseOperators = OPERATORS_BY_TYPE[type];

  // Add special cross-category operators for NAME field only
  if (field === "NAME") {
    return [...baseOperators, ...NAME_SPECIAL_OPERATORS];
  }

  return baseOperators;
}

// Unit conversion helpers for display
export const BYTE_UNITS = [
  { value: 1, label: "B" },
  { value: 1024, label: "KiB" },
  { value: 1024 * 1024, label: "MiB" },
  { value: 1024 * 1024 * 1024, label: "GiB" },
  { value: 1024 * 1024 * 1024 * 1024, label: "TiB" },
];

export const DURATION_UNITS = [
  { value: 1, label: "seconds" },
  { value: 60, label: "minutes" },
  { value: 3600, label: "hours" },
  { value: 86400, label: "days" },
];

export const SPEED_UNITS = [
  { value: 1, label: "B/s" },
  { value: 1024, label: "KiB/s" },
  { value: 1024 * 1024, label: "MiB/s" },
];

// Capability types for disabling fields/states in query builder
export type CapabilityKey = "trackerHealth" | "localFilesystemAccess"

export type Capabilities = Record<CapabilityKey, boolean>

export interface DisabledField {
  field: string
  reason: string
}

export interface DisabledStateValue {
  value: string
  reason: string
}

// Capability requirements for disabling fields/states in query builder
export const CAPABILITY_REASONS = {
  trackerHealth: "Requires qBittorrent 5.1+",
  localFilesystemAccess: "Requires Local Filesystem Access",
} as const;

export const FIELD_REQUIREMENTS = {
  IS_UNREGISTERED: "trackerHealth",
  HAS_MISSING_FILES: "localFilesystemAccess",
  HARDLINK_SCOPE: "localFilesystemAccess",
} as const;

export const STATE_VALUE_REQUIREMENTS = {
  tracker_down: "trackerHealth",
} as const;

// Uncategorized sentinel (Radix Select requires non-empty values)
export const CATEGORY_UNCATEGORIZED_VALUE = "__uncategorized__";
