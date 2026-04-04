/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Field definitions with metadata for the query builder UI
export const CONDITION_FIELDS = {
  // String fields
  NAME: { labelKey: "fieldCombobox.fields.name", label: "Name", type: "string" as const, description: "Torrent name" },
  HASH: { labelKey: "fieldCombobox.fields.hash", label: "Hash", type: "string" as const, description: "Torrent info hash" },
  INFOHASH_V1: { labelKey: "fieldCombobox.fields.infohashV1", label: "Infohash v1", type: "string" as const, description: "BitTorrent v1 info hash" },
  INFOHASH_V2: { labelKey: "fieldCombobox.fields.infohashV2", label: "Infohash v2", type: "string" as const, description: "BitTorrent v2 info hash" },
  MAGNET_URI: { labelKey: "fieldCombobox.fields.magnetUri", label: "Magnet URI", type: "string" as const, description: "Magnet link for the torrent" },
  CATEGORY: { labelKey: "fieldCombobox.fields.category", label: "Category", type: "string" as const, description: "Torrent category" },
  TAGS: { labelKey: "fieldCombobox.fields.tags", label: "Tags", type: "string" as const, description: "Comma-separated tags" },
  SAVE_PATH: { labelKey: "fieldCombobox.fields.savePath", label: "Save Path", type: "string" as const, description: "Download location" },
  CONTENT_PATH: { labelKey: "fieldCombobox.fields.contentPath", label: "Content Path", type: "string" as const, description: "Content location" },
  DOWNLOAD_PATH: { labelKey: "fieldCombobox.fields.downloadPath", label: "Download Path", type: "string" as const, description: "Session download path from qBittorrent" },
  CREATED_BY: { labelKey: "fieldCombobox.fields.createdBy", label: "Created By", type: "string" as const, description: "Torrent creator metadata" },
  TRACKERS: { labelKey: "fieldCombobox.fields.trackers", label: "Trackers (All)", type: "string" as const, description: "All tracker URLs/domains/display names for this torrent" },
  CONTENT_TYPE: { labelKey: "fieldCombobox.fields.contentType", label: "Content Type", type: "string" as const, description: "Detected content type (movie, tv, music, etc) from release parsing" },
  EFFECTIVE_NAME: { labelKey: "fieldCombobox.fields.effectiveName", label: "Effective Name", type: "string" as const, description: "Parsed item key (title/year or SxxEyy) for grouping across trackers" },
  RLS_SOURCE: { labelKey: "fieldCombobox.fields.rlsSource", label: "Source (RLS)", type: "string" as const, description: "Parsed source (normalized: WEBDL, WEBRIP, BLURAY, etc)" },
  RLS_RESOLUTION: { labelKey: "fieldCombobox.fields.rlsResolution", label: "Resolution (RLS)", type: "string" as const, description: "Parsed resolution (e.g. 1080P, 2160P)" },
  RLS_CODEC: { labelKey: "fieldCombobox.fields.rlsCodec", label: "Codec (RLS)", type: "string" as const, description: "Parsed video codec (normalized: AVC, HEVC, etc)" },
  RLS_HDR: { labelKey: "fieldCombobox.fields.rlsHdr", label: "HDR (RLS)", type: "string" as const, description: "Parsed HDR tags (e.g. DV, HDR10, HDR)" },
  RLS_AUDIO: { labelKey: "fieldCombobox.fields.rlsAudio", label: "Audio (RLS)", type: "string" as const, description: "Parsed audio tags (e.g. DTS, TRUEHD, AAC)" },
  RLS_CHANNELS: { labelKey: "fieldCombobox.fields.rlsChannels", label: "Channels (RLS)", type: "string" as const, description: "Parsed audio channels (e.g. 5.1, 7.1)" },
  RLS_GROUP: { labelKey: "fieldCombobox.fields.rlsGroup", label: "Group (RLS)", type: "string" as const, description: "Parsed release group (e.g. NTb, FLUX, FraMeSToR)" },
  STATE: { labelKey: "fieldCombobox.fields.state", label: "State", type: "state" as const, description: "Torrent status (matches sidebar filters)" },
  TRACKER: { labelKey: "fieldCombobox.fields.tracker", label: "Tracker", type: "string" as const, description: "Primary tracker (URL, domain, or display name)" },
  COMMENT: { labelKey: "fieldCombobox.fields.comment", label: "Comment", type: "string" as const, description: "Torrent comment" },

  // Size fields (bytes)
  SIZE: { labelKey: "fieldCombobox.fields.size", label: "Size", type: "bytes" as const, description: "Selected file size" },
  TOTAL_SIZE: { labelKey: "fieldCombobox.fields.totalSize", label: "Total Size", type: "bytes" as const, description: "Total torrent size" },
  COMPLETED: { labelKey: "fieldCombobox.fields.completed", label: "Completed", type: "bytes" as const, description: "Completed bytes" },
  DOWNLOADED: { labelKey: "fieldCombobox.fields.downloaded", label: "Downloaded", type: "bytes" as const, description: "Total downloaded" },
  DOWNLOADED_SESSION: { labelKey: "fieldCombobox.fields.downloadedSession", label: "Downloaded (Session)", type: "bytes" as const, description: "Downloaded in current session" },
  UPLOADED: { labelKey: "fieldCombobox.fields.uploaded", label: "Uploaded", type: "bytes" as const, description: "Total uploaded" },
  UPLOADED_SESSION: { labelKey: "fieldCombobox.fields.uploadedSession", label: "Uploaded (Session)", type: "bytes" as const, description: "Uploaded in current session" },
  AMOUNT_LEFT: { labelKey: "fieldCombobox.fields.amountLeft", label: "Amount Left", type: "bytes" as const, description: "Remaining to download" },
  FREE_SPACE: { labelKey: "fieldCombobox.fields.freeSpace", label: "Free Space", type: "bytes" as const, description: "Free space on the instance's filesystem" },

  // Timestamp-backed fields represented as ages (seconds since event)
  ADDED_ON: { labelKey: "fieldCombobox.fields.addedOn", label: "Added Age", type: "duration" as const, description: "Time since torrent was added" },
  COMPLETION_ON: { labelKey: "fieldCombobox.fields.completionOn", label: "Completed Age", type: "duration" as const, description: "Time since download completed" },
  LAST_ACTIVITY: { labelKey: "fieldCombobox.fields.lastActivity", label: "Inactive Time", type: "duration" as const, description: "Time since last activity" },
  SEEN_COMPLETE: { labelKey: "fieldCombobox.fields.seenComplete", label: "Seen Complete Age", type: "duration" as const, description: "Time since torrent was last seen complete" },

  // Duration fields (seconds)
  ETA: { labelKey: "fieldCombobox.fields.eta", label: "ETA", type: "duration" as const, description: "Estimated seconds to completion" },
  REANNOUNCE: { labelKey: "fieldCombobox.fields.reannounce", label: "Reannounce In", type: "duration" as const, description: "Seconds until next reannounce" },
  SEEDING_TIME: { labelKey: "fieldCombobox.fields.seedingTime", label: "Seeding Time", type: "duration" as const, description: "Time spent seeding" },
  TIME_ACTIVE: { labelKey: "fieldCombobox.fields.timeActive", label: "Time Active", type: "duration" as const, description: "Total active time" },
  MAX_SEEDING_TIME: { labelKey: "fieldCombobox.fields.maxSeedingTime", label: "Max Seeding Time", type: "duration" as const, description: "Configured max seeding time" },
  MAX_INACTIVE_SEEDING_TIME: { labelKey: "fieldCombobox.fields.maxInactiveSeedingTime", label: "Max Inactive Seeding Time", type: "duration" as const, description: "Configured max inactive seeding time" },
  SEEDING_TIME_LIMIT: { labelKey: "fieldCombobox.fields.seedingTimeLimit", label: "Seeding Time Limit", type: "duration" as const, description: "Torrent seeding time limit" },
  INACTIVE_SEEDING_TIME_LIMIT: { labelKey: "fieldCombobox.fields.inactiveSeedingTimeLimit", label: "Inactive Seeding Time Limit", type: "duration" as const, description: "Torrent inactive seeding time limit" },
  ADDED_ON_AGE: { labelKey: "fieldCombobox.fields.addedOnAge", label: "Added Age (legacy)", type: "duration" as const, description: "Legacy alias for Added Age" },
  COMPLETION_ON_AGE: { labelKey: "fieldCombobox.fields.completionOnAge", label: "Completed Age (legacy)", type: "duration" as const, description: "Legacy alias for Completed Age" },
  LAST_ACTIVITY_AGE: { labelKey: "fieldCombobox.fields.lastActivityAge", label: "Inactive Time (legacy)", type: "duration" as const, description: "Legacy alias for Inactive Time" },

  // System Time fields
  SYSTEM_HOUR: { labelKey: "fieldCombobox.fields.systemHour", label: "System Hour", type: "integer" as const, description: "Current system hour (0-23)" },
  SYSTEM_MINUTE: { labelKey: "fieldCombobox.fields.systemMinute", label: "System Minute", type: "integer" as const, description: "Current system minute (0-59)" },
  SYSTEM_DAY_OF_WEEK: { labelKey: "fieldCombobox.fields.systemDayOfWeek", label: "System Day of Week", type: "integer" as const, description: "Current system day of week (0=Sun to 6=Sat)" },
  SYSTEM_DAY: { labelKey: "fieldCombobox.fields.systemDay", label: "System Day", type: "integer" as const, description: "Current system day of month (1-31)" },
  SYSTEM_MONTH: { labelKey: "fieldCombobox.fields.systemMonth", label: "System Month", type: "integer" as const, description: "Current system month (1-12)" },
  SYSTEM_YEAR: { labelKey: "fieldCombobox.fields.systemYear", label: "System Year", type: "integer" as const, description: "Current system year" },

  // Float fields
  RATIO: { labelKey: "fieldCombobox.fields.ratio", label: "Ratio", type: "float" as const, description: "Upload/download ratio" },
  RATIO_LIMIT: { labelKey: "fieldCombobox.fields.ratioLimit", label: "Ratio Limit", type: "float" as const, description: "Configured ratio limit" },
  MAX_RATIO: { labelKey: "fieldCombobox.fields.maxRatio", label: "Max Ratio", type: "float" as const, description: "Maximum ratio value from qBittorrent" },
  PROGRESS: { labelKey: "fieldCombobox.fields.progress", label: "Progress", type: "percentage" as const, description: "Download progress (0-100%)" },
  AVAILABILITY: { labelKey: "fieldCombobox.fields.availability", label: "Availability", type: "float" as const, description: "Distributed copies" },
  POPULARITY: { labelKey: "fieldCombobox.fields.popularity", label: "Popularity", type: "float" as const, description: "Swarm popularity metric" },

  // Speed fields (bytes/s)
  DL_SPEED: { labelKey: "fieldCombobox.fields.dlSpeed", label: "Download Speed", type: "speed" as const, description: "Current download speed" },
  UP_SPEED: { labelKey: "fieldCombobox.fields.upSpeed", label: "Upload Speed", type: "speed" as const, description: "Current upload speed" },
  DL_LIMIT: { labelKey: "fieldCombobox.fields.dlLimit", label: "Download Limit", type: "speed" as const, description: "Configured download speed limit" },
  UP_LIMIT: { labelKey: "fieldCombobox.fields.upLimit", label: "Upload Limit", type: "speed" as const, description: "Configured upload speed limit" },

  // Count fields
  NUM_SEEDS: { labelKey: "fieldCombobox.fields.numSeeds", label: "Active Seeders", type: "integer" as const, description: "Seeders currently connected to" },
  NUM_LEECHS: { labelKey: "fieldCombobox.fields.numLeechs", label: "Active Leechers", type: "integer" as const, description: "Leechers currently connected to" },
  NUM_COMPLETE: { labelKey: "fieldCombobox.fields.numComplete", label: "Total Seeders", type: "integer" as const, description: "Total seeders in swarm (tracker-reported)" },
  NUM_INCOMPLETE: { labelKey: "fieldCombobox.fields.numIncomplete", label: "Total Leechers", type: "integer" as const, description: "Total leechers in swarm (tracker-reported)" },
  TRACKERS_COUNT: { labelKey: "fieldCombobox.fields.trackersCount", label: "Trackers", type: "integer" as const, description: "Number of trackers" },
  PRIORITY: { labelKey: "fieldCombobox.fields.priority", label: "Queue Priority", type: "integer" as const, description: "Torrent queue priority value" },
  GROUP_SIZE: { labelKey: "fieldCombobox.fields.groupSize", label: "Group Size", type: "integer" as const, description: "Number of torrents in the selected group for this condition" },

  // Boolean fields
  PRIVATE: { labelKey: "fieldCombobox.fields.private", label: "Private", type: "boolean" as const, description: "Private tracker torrent" },
  AUTO_MANAGED: { labelKey: "fieldCombobox.fields.autoManaged", label: "Auto-managed", type: "boolean" as const, description: "Managed by automatic torrent management" },
  FIRST_LAST_PIECE_PRIO: { labelKey: "fieldCombobox.fields.firstLastPiecePrio", label: "First/Last Piece Priority", type: "boolean" as const, description: "First and last pieces are prioritized" },
  FORCE_START: { labelKey: "fieldCombobox.fields.forceStart", label: "Force Start", type: "boolean" as const, description: "Ignores queue limits and starts immediately" },
  SEQUENTIAL_DOWNLOAD: { labelKey: "fieldCombobox.fields.sequentialDownload", label: "Sequential Download", type: "boolean" as const, description: "Downloads pieces sequentially" },
  SUPER_SEEDING: { labelKey: "fieldCombobox.fields.superSeeding", label: "Super Seeding", type: "boolean" as const, description: "Super-seeding mode enabled" },
  IS_UNREGISTERED: { labelKey: "fieldCombobox.fields.isUnregistered", label: "Unregistered", type: "boolean" as const, description: "Tracker reports torrent as unregistered" },
  HAS_MISSING_FILES: { labelKey: "fieldCombobox.fields.hasMissingFiles", label: "Has Missing Files", type: "boolean" as const, description: "Completed torrent has files missing on disk. Requires Local Filesystem Access." },
  IS_GROUPED: { labelKey: "fieldCombobox.fields.isGrouped", label: "Is Grouped", type: "boolean" as const, description: "True when group size > 1 for the selected group in this condition" },
  EXISTS_ON_OTHER_INSTANCE: { labelKey: "fieldCombobox.fields.existsOnOtherInstance", label: "Exists on Other Instance", type: "boolean" as const, description: "A matching torrent exists on at least one other active instance" },
  SEEDING_ON_OTHER_INSTANCE: { labelKey: "fieldCombobox.fields.seedingOnOtherInstance", label: "Seeding on Other Instance", type: "boolean" as const, description: "A matching torrent is actively seeding on at least one other active instance" },
  EXISTS_ON_SAME_INSTANCE: { labelKey: "fieldCombobox.fields.existsOnSameInstance", label: "Cross-seed Exists on Same Instance", type: "boolean" as const, description: "A cross-seed (same content, different hash) exists on this instance" },
  SEEDING_ON_SAME_INSTANCE: { labelKey: "fieldCombobox.fields.seedingOnSameInstance", label: "Cross-seed Seeding on Same Instance", type: "boolean" as const, description: "A cross-seed is actively seeding on this instance" },

  // Enum-like fields
  HARDLINK_SCOPE: { labelKey: "fieldCombobox.fields.hardlinkScope", label: "Hardlink scope", type: "hardlinkScope" as const, description: "Where hardlinks for this torrent's files exist. Requires Local Filesystem Access." },
} as const;

export type FieldType = "string" | "state" | "bytes" | "duration" | "float" | "percentage" | "speed" | "integer" | "boolean" | "hardlinkScope";

// Operators available per field type
export const OPERATORS_BY_TYPE: Record<FieldType, { value: string; labelKey: string; label: string }[]> = {
  string: [
    { value: "EQUAL", labelKey: "leafCondition.operator.labels.equal", label: "equals" },
    { value: "NOT_EQUAL", labelKey: "leafCondition.operator.labels.notEqual", label: "not equals" },
    { value: "CONTAINS", labelKey: "leafCondition.operator.labels.contains", label: "contains" },
    { value: "NOT_CONTAINS", labelKey: "leafCondition.operator.labels.notContains", label: "not contains" },
    { value: "STARTS_WITH", labelKey: "leafCondition.operator.labels.startsWith", label: "starts with" },
    { value: "ENDS_WITH", labelKey: "leafCondition.operator.labels.endsWith", label: "ends with" },
    { value: "MATCHES", labelKey: "leafCondition.operator.labels.matches", label: "matches regex" },
  ],
  state: [
    { value: "EQUAL", labelKey: "leafCondition.operator.labels.is", label: "is" },
    { value: "NOT_EQUAL", labelKey: "leafCondition.operator.labels.isNot", label: "is not" },
  ],
  bytes: [
    { value: "EQUAL", labelKey: "leafCondition.operator.labels.eqSymbol", label: "=" },
    { value: "NOT_EQUAL", labelKey: "leafCondition.operator.labels.notEqSymbol", label: "!=" },
    { value: "GREATER_THAN", labelKey: "leafCondition.operator.labels.gtSymbol", label: ">" },
    { value: "GREATER_THAN_OR_EQUAL", labelKey: "leafCondition.operator.labels.gteSymbol", label: ">=" },
    { value: "LESS_THAN", labelKey: "leafCondition.operator.labels.ltSymbol", label: "<" },
    { value: "LESS_THAN_OR_EQUAL", labelKey: "leafCondition.operator.labels.lteSymbol", label: "<=" },
    { value: "BETWEEN", labelKey: "leafCondition.operator.labels.between", label: "between" },
  ],
  duration: [
    { value: "EQUAL", labelKey: "leafCondition.operator.labels.eqSymbol", label: "=" },
    { value: "NOT_EQUAL", labelKey: "leafCondition.operator.labels.notEqSymbol", label: "!=" },
    { value: "GREATER_THAN", labelKey: "leafCondition.operator.labels.gtSymbol", label: ">" },
    { value: "GREATER_THAN_OR_EQUAL", labelKey: "leafCondition.operator.labels.gteSymbol", label: ">=" },
    { value: "LESS_THAN", labelKey: "leafCondition.operator.labels.ltSymbol", label: "<" },
    { value: "LESS_THAN_OR_EQUAL", labelKey: "leafCondition.operator.labels.lteSymbol", label: "<=" },
    { value: "BETWEEN", labelKey: "leafCondition.operator.labels.between", label: "between" },
  ],
  float: [
    { value: "EQUAL", labelKey: "leafCondition.operator.labels.eqSymbol", label: "=" },
    { value: "NOT_EQUAL", labelKey: "leafCondition.operator.labels.notEqSymbol", label: "!=" },
    { value: "GREATER_THAN", labelKey: "leafCondition.operator.labels.gtSymbol", label: ">" },
    { value: "GREATER_THAN_OR_EQUAL", labelKey: "leafCondition.operator.labels.gteSymbol", label: ">=" },
    { value: "LESS_THAN", labelKey: "leafCondition.operator.labels.ltSymbol", label: "<" },
    { value: "LESS_THAN_OR_EQUAL", labelKey: "leafCondition.operator.labels.lteSymbol", label: "<=" },
    { value: "BETWEEN", labelKey: "leafCondition.operator.labels.between", label: "between" },
  ],
  percentage: [
    { value: "EQUAL", labelKey: "leafCondition.operator.labels.eqSymbol", label: "=" },
    { value: "NOT_EQUAL", labelKey: "leafCondition.operator.labels.notEqSymbol", label: "!=" },
    { value: "GREATER_THAN", labelKey: "leafCondition.operator.labels.gtSymbol", label: ">" },
    { value: "GREATER_THAN_OR_EQUAL", labelKey: "leafCondition.operator.labels.gteSymbol", label: ">=" },
    { value: "LESS_THAN", labelKey: "leafCondition.operator.labels.ltSymbol", label: "<" },
    { value: "LESS_THAN_OR_EQUAL", labelKey: "leafCondition.operator.labels.lteSymbol", label: "<=" },
    { value: "BETWEEN", labelKey: "leafCondition.operator.labels.between", label: "between" },
  ],
  speed: [
    { value: "EQUAL", labelKey: "leafCondition.operator.labels.eqSymbol", label: "=" },
    { value: "NOT_EQUAL", labelKey: "leafCondition.operator.labels.notEqSymbol", label: "!=" },
    { value: "GREATER_THAN", labelKey: "leafCondition.operator.labels.gtSymbol", label: ">" },
    { value: "GREATER_THAN_OR_EQUAL", labelKey: "leafCondition.operator.labels.gteSymbol", label: ">=" },
    { value: "LESS_THAN", labelKey: "leafCondition.operator.labels.ltSymbol", label: "<" },
    { value: "LESS_THAN_OR_EQUAL", labelKey: "leafCondition.operator.labels.lteSymbol", label: "<=" },
    { value: "BETWEEN", labelKey: "leafCondition.operator.labels.between", label: "between" },
  ],
  integer: [
    { value: "EQUAL", labelKey: "leafCondition.operator.labels.eqSymbol", label: "=" },
    { value: "NOT_EQUAL", labelKey: "leafCondition.operator.labels.notEqSymbol", label: "!=" },
    { value: "GREATER_THAN", labelKey: "leafCondition.operator.labels.gtSymbol", label: ">" },
    { value: "GREATER_THAN_OR_EQUAL", labelKey: "leafCondition.operator.labels.gteSymbol", label: ">=" },
    { value: "LESS_THAN", labelKey: "leafCondition.operator.labels.ltSymbol", label: "<" },
    { value: "LESS_THAN_OR_EQUAL", labelKey: "leafCondition.operator.labels.lteSymbol", label: "<=" },
    { value: "BETWEEN", labelKey: "leafCondition.operator.labels.between", label: "between" },
  ],
  boolean: [
    { value: "EQUAL", labelKey: "leafCondition.operator.labels.is", label: "is" },
    { value: "NOT_EQUAL", labelKey: "leafCondition.operator.labels.isNot", label: "is not" },
  ],
  hardlinkScope: [
    { value: "EQUAL", labelKey: "leafCondition.operator.labels.is", label: "is" },
    { value: "NOT_EQUAL", labelKey: "leafCondition.operator.labels.isNot", label: "is not" },
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
    labelKey: "fieldCombobox.groups.identity",
    label: "Identity",
    fields: ["NAME", "HASH", "INFOHASH_V1", "INFOHASH_V2", "MAGNET_URI", "CATEGORY", "TAGS", "STATE", "CREATED_BY"],
  },
  {
    labelKey: "fieldCombobox.groups.release",
    label: "Release",
    fields: ["CONTENT_TYPE", "EFFECTIVE_NAME", "RLS_SOURCE", "RLS_RESOLUTION", "RLS_CODEC", "RLS_HDR", "RLS_AUDIO", "RLS_CHANNELS", "RLS_GROUP"],
  },
  {
    labelKey: "fieldCombobox.groups.grouping",
    label: "Grouping",
    fields: ["GROUP_SIZE", "IS_GROUPED"],
  },
  {
    labelKey: "fieldCombobox.groups.paths",
    label: "Paths",
    fields: ["SAVE_PATH", "CONTENT_PATH", "DOWNLOAD_PATH"],
  },
  {
    labelKey: "fieldCombobox.groups.size",
    label: "Size",
    fields: ["SIZE", "TOTAL_SIZE", "COMPLETED", "DOWNLOADED", "DOWNLOADED_SESSION", "UPLOADED", "UPLOADED_SESSION", "AMOUNT_LEFT", "FREE_SPACE"],
  },
  {
    labelKey: "fieldCombobox.groups.time",
    label: "Time",
    fields: ["ADDED_ON", "COMPLETION_ON", "LAST_ACTIVITY", "SEEN_COMPLETE", "ETA", "REANNOUNCE", "SEEDING_TIME", "TIME_ACTIVE", "MAX_SEEDING_TIME", "MAX_INACTIVE_SEEDING_TIME", "SEEDING_TIME_LIMIT", "INACTIVE_SEEDING_TIME_LIMIT"],
  },
  {
    labelKey: "fieldCombobox.groups.systemTime",
    label: "System Time",
    fields: ["SYSTEM_HOUR", "SYSTEM_MINUTE", "SYSTEM_DAY_OF_WEEK", "SYSTEM_DAY", "SYSTEM_MONTH", "SYSTEM_YEAR"],
  },
  {
    labelKey: "fieldCombobox.groups.progress",
    label: "Progress",
    fields: ["RATIO", "RATIO_LIMIT", "MAX_RATIO", "PROGRESS", "AVAILABILITY", "POPULARITY"],
  },
  {
    labelKey: "fieldCombobox.groups.speed",
    label: "Speed",
    fields: ["DL_SPEED", "UP_SPEED", "DL_LIMIT", "UP_LIMIT"],
  },
  {
    labelKey: "fieldCombobox.groups.peers",
    label: "Peers",
    fields: ["NUM_SEEDS", "NUM_LEECHS", "NUM_COMPLETE", "NUM_INCOMPLETE", "PRIORITY"],
  },
  {
    labelKey: "fieldCombobox.groups.tracker",
    label: "Tracker",
    fields: ["TRACKER", "TRACKERS", "TRACKERS_COUNT", "PRIVATE", "IS_UNREGISTERED", "COMMENT"],
  },
  {
    labelKey: "fieldCombobox.groups.crossSeed",
    label: "Cross-Seed",
    fields: ["EXISTS_ON_OTHER_INSTANCE", "SEEDING_ON_OTHER_INSTANCE", "EXISTS_ON_SAME_INSTANCE", "SEEDING_ON_SAME_INSTANCE"],
  },
  {
    labelKey: "fieldCombobox.groups.mode",
    label: "Mode",
    fields: ["AUTO_MANAGED", "FIRST_LAST_PIECE_PRIO", "FORCE_START", "SEQUENTIAL_DOWNLOAD", "SUPER_SEEDING"],
  },
  {
    labelKey: "fieldCombobox.groups.files",
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
  { value: "EXISTS_IN", labelKey: "leafCondition.operator.labels.existsIn", label: "exists in" },
  { value: "CONTAINS_IN", labelKey: "leafCondition.operator.labels.containsIn", label: "similar exists in" },
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
