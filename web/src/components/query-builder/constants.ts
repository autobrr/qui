/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TFunction } from "i18next";
import type { ConditionField } from "@/types";

// find-hardcoded-i18n-literals.mjs skips this file: its English is only a t() defaultValue.
// A table rendered raw, instead of through a getTranslated* helper, ships English unnoticed.

// Clock fields cycle, so a BETWEEN range whose minimum is above its maximum wraps
// past the end of the field. Mirrors ConditionField.WrapsBetween in the backend.
export const WRAPPING_BETWEEN_FIELDS: ReadonlySet<ConditionField> = new Set<ConditionField>([
  "SYSTEM_HOUR",
  "SYSTEM_MINUTE",
  "SYSTEM_DAY_OF_WEEK",
  "SYSTEM_MONTH",
]);

// Value type of every condition field; it picks the operators and the value input.
export const CONDITION_FIELD_TYPES = {
  // String fields
  NAME: "string",
  HASH: "string",
  INFOHASH_V1: "string",
  INFOHASH_V2: "string",
  MAGNET_URI: "string",
  CATEGORY: "string",
  TAGS: "string",
  SAVE_PATH: "string",
  CONTENT_PATH: "string",
  DOWNLOAD_PATH: "string",
  CREATED_BY: "string",
  // Legacy alias of TRACKER, which now matches every tracker too. Kept out of
  // FIELD_GROUPS so it is no longer offered; saved rules can still use it.
  TRACKERS: "string",
  CONTENT_TYPE: "string",
  EFFECTIVE_NAME: "string",
  RLS_SOURCE: "string",
  RLS_RESOLUTION: "string",
  RLS_CODEC: "string",
  RLS_HDR: "string",
  RLS_AUDIO: "string",
  RLS_CHANNELS: "string",
  RLS_GROUP: "string",
  RLS_YEAR: "integer",
  STATE: "state",
  TRACKER: "string",
  TRACKER_STATUS: "trackerStatus",
  TRACKER_MESSAGE: "string",
  COMMENT: "string",

  // Size fields (bytes)
  SIZE: "bytes",
  TOTAL_SIZE: "bytes",
  COMPLETED: "bytes",
  DOWNLOADED: "bytes",
  DOWNLOADED_SESSION: "bytes",
  UPLOADED: "bytes",
  UPLOADED_SESSION: "bytes",
  AMOUNT_LEFT: "bytes",
  FREE_SPACE: "bytes",

  // Timestamp-backed fields represented as ages (seconds since event)
  ADDED_ON: "duration",
  COMPLETION_ON: "duration",
  LAST_ACTIVITY: "duration",
  SEEN_COMPLETE: "duration",

  // Duration fields (seconds)
  ETA: "duration",
  REANNOUNCE: "duration",
  SEEDING_TIME: "duration",
  TIME_ACTIVE: "duration",
  MAX_SEEDING_TIME: "duration",
  MAX_INACTIVE_SEEDING_TIME: "duration",
  SEEDING_TIME_LIMIT: "duration",
  INACTIVE_SEEDING_TIME_LIMIT: "duration",
  ADDED_ON_AGE: "duration",
  COMPLETION_ON_AGE: "duration",
  LAST_ACTIVITY_AGE: "duration",

  // System Time fields
  SYSTEM_HOUR: "integer",
  SYSTEM_MINUTE: "integer",
  SYSTEM_DAY_OF_WEEK: "integer",
  SYSTEM_DAY: "integer",
  SYSTEM_MONTH: "integer",
  SYSTEM_YEAR: "integer",

  // Float fields
  RATIO: "float",
  RATIO_LIMIT: "float",
  MAX_RATIO: "float",
  UPLOADED_OVER_SIZE: "float",
  PROGRESS: "percentage",
  AVAILABILITY: "float",
  POPULARITY: "float",

  // Speed fields (bytes/s)
  DL_SPEED: "speed",
  UP_SPEED: "speed",
  DL_LIMIT: "speed",
  UP_LIMIT: "speed",

  // Count fields
  NUM_SEEDS: "integer",
  NUM_LEECHS: "integer",
  NUM_COMPLETE: "integer",
  NUM_INCOMPLETE: "integer",
  TRACKERS_COUNT: "integer",
  PRIORITY: "integer",
  GROUP_SIZE: "integer",

  // Boolean fields
  PRIVATE: "boolean",
  AUTO_MANAGED: "boolean",
  FIRST_LAST_PIECE_PRIO: "boolean",
  FORCE_START: "boolean",
  SEQUENTIAL_DOWNLOAD: "boolean",
  SUPER_SEEDING: "boolean",
  IS_UNREGISTERED: "boolean",
  HAS_MISSING_FILES: "boolean",
  HAS_SKIPPED_FILES: "boolean",
  IS_GROUPED: "boolean",
  EXISTS_ON_OTHER_INSTANCE: "boolean",
  SEEDING_ON_OTHER_INSTANCE: "boolean",
  EXISTS_ON_SAME_INSTANCE: "boolean",
  SEEDING_ON_SAME_INSTANCE: "boolean",
  CROSS_SEED_TAGS: "string",
  SEASON_PACK_STATUS: "seasonPackStatus",
  SEASON_PACK_STATUS_ANY_INSTANCE: "seasonPackStatus",

  // Enum-like fields
  HARDLINK_SCOPE: "hardlinkScope",
  HARDLINK_SCOPE_CROSS: "hardlinkScope",
} as const satisfies Record<ConditionField, FieldType>;

export type FieldType = "string" | "state" | "trackerStatus" | "bytes" | "duration" | "float" | "percentage" | "speed" | "integer" | "boolean" | "hardlinkScope" | "seasonPackStatus";

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
  trackerStatus: [
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
  seasonPackStatus: [
    { value: "EQUAL", label: "is" },
    { value: "NOT_EQUAL", label: "is not" },
  ],
};

// Hardlink scope values (matches backend wire format)
export const HARDLINK_SCOPE_VALUES = [
  { value: "none", label: "None" },
  { value: "torrents_only", label: "Only other torrents" },
  { value: "inside_qbittorrent", label: "Inside qBittorrent (even if also linked outside)" },
  { value: "outside_qbittorrent", label: "Outside qBittorrent (library/import)" },
];

// Content types from release parsing. Mirrors releases.ContentTypes in
// pkg/releases/content_type.go, which constants.test.ts enforces. A rule-forceable
// subset of the same values lives in cross-seed/CategoryMappingRulesEditor.tsx.
export const CONTENT_TYPE_VALUES = [
  { value: "movie", label: "Movie" },
  { value: "tv", label: "TV" },
  { value: "music", label: "Music" },
  { value: "audiobook", label: "Audiobook" },
  { value: "book", label: "Book" },
  { value: "comic", label: "Comic" },
  { value: "game", label: "Game" },
  { value: "app", label: "App" },
  { value: "adult", label: "Adult" },
  { value: "unknown", label: "Unknown" },
];

// Season pack status values (matches backend wire format)
export const SEASON_PACK_STATUS_VALUES = [
  { value: "pack", label: "Season pack" },
  { value: "packed", label: "Episode covered by a season pack" },
  { value: "unpacked", label: "Episode with no season pack" },
];

// qBittorrent torrent states
export const TORRENT_STATES = [
  // Status buckets (same as sidebar)
  { value: "downloading", label: "Downloading" },
  { value: "uploading", label: "Seeding" },
  { value: "completed", label: "Completed" },
  { value: "stopped", label: "Stopped" },
  { value: "active", label: "Active" },
  { value: "inactive", label: "Inactive" },
  { value: "running", label: "Running" },
  { value: "stalled", label: "Stalled" },
  { value: "stalled_uploading", label: "Stalled Up" },
  { value: "stalled_downloading", label: "Stalled Down" },
  { value: "errored", label: "Error" },
  { value: "tracker_down", label: "Tracker Down" },
  { value: "tracker_error", label: "Tracker Error" },
  { value: "checking", label: "Checking" },
  { value: "checkingResumeData", label: "Checking Resume Data" },
  { value: "moving", label: "Moving" },

  // Specific qBittorrent state (kept for targeting missing-file issues)
  { value: "missingFiles", label: "Missing Files" },
];

// Field groups for organized selection
export const FIELD_GROUPS = [
  {
    label: "Identity",
    fields: ["NAME", "HASH", "INFOHASH_V1", "INFOHASH_V2", "MAGNET_URI", "CATEGORY", "TAGS", "STATE", "CREATED_BY"],
  },
  {
    label: "Release",
    fields: ["CONTENT_TYPE", "EFFECTIVE_NAME", "RLS_SOURCE", "RLS_RESOLUTION", "RLS_CODEC", "RLS_HDR", "RLS_AUDIO", "RLS_CHANNELS", "RLS_GROUP", "RLS_YEAR"],
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
    label: "System Time",
    fields: ["SYSTEM_HOUR", "SYSTEM_MINUTE", "SYSTEM_DAY_OF_WEEK", "SYSTEM_DAY", "SYSTEM_MONTH", "SYSTEM_YEAR"],
  },
  {
    label: "Progress",
    fields: ["RATIO", "RATIO_LIMIT", "MAX_RATIO", "UPLOADED_OVER_SIZE", "PROGRESS", "AVAILABILITY", "POPULARITY"],
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
    fields: ["TRACKER", "TRACKERS_COUNT", "PRIVATE", "IS_UNREGISTERED", "TRACKER_STATUS", "TRACKER_MESSAGE", "COMMENT"],
  },
  {
    label: "Cross-Seed",
    fields: ["EXISTS_ON_OTHER_INSTANCE", "SEEDING_ON_OTHER_INSTANCE", "EXISTS_ON_SAME_INSTANCE", "SEEDING_ON_SAME_INSTANCE", "CROSS_SEED_TAGS", "SEASON_PACK_STATUS", "SEASON_PACK_STATUS_ANY_INSTANCE"],
  },
  {
    label: "Mode",
    fields: ["AUTO_MANAGED", "FIRST_LAST_PIECE_PRIO", "FORCE_START", "SEQUENTIAL_DOWNLOAD", "SUPER_SEEDING"],
  },
  {
    label: "Files",
    fields: ["HARDLINK_SCOPE", "HARDLINK_SCOPE_CROSS", "HAS_MISSING_FILES", "HAS_SKIPPED_FILES"],
  },
];

// Helper to get field type
export function getFieldType(field: string): FieldType {
  return CONDITION_FIELD_TYPES[field as ConditionField] ?? "string";
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

// Capability types for disabling fields/states in query builder
export type CapabilityKey = "trackerHealth" | "localFilesystemAccess" | "fileIdentity"

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

// "disabled" (status 0) is intentionally omitted: it only ever applies to qBittorrent's
// DHT/PeX/LSD pseudo-trackers, which the evaluator skips, so it could never match a real tracker.
export const TRACKER_STATUS_VALUES = [
  { value: "not_contacted", label: "Not contacted" },
  { value: "working", label: "Working" },
  { value: "updating", label: "Updating" },
  { value: "error", label: "Error" },
  { value: "tracker_error", label: "Tracker error" },
  { value: "unreachable", label: "Unreachable" },
] as const;

export const FIELD_REQUIREMENTS = {
  IS_UNREGISTERED: "trackerHealth",
  TRACKER_STATUS: "trackerHealth",
  TRACKER_MESSAGE: "trackerHealth",
  HAS_MISSING_FILES: "localFilesystemAccess",
  HARDLINK_SCOPE: "fileIdentity",
  HARDLINK_SCOPE_CROSS: "fileIdentity",
} as const;

export const STATE_VALUE_REQUIREMENTS = {
  tracker_down: "trackerHealth",
  tracker_error: "trackerHealth",
} as const;

// Uncategorized sentinel (Radix Select requires non-empty values)
export const CATEGORY_UNCATEGORIZED_VALUE = "__uncategorized__";

// --- i18n helper functions ---

/** Get translated label for a condition field */
export function getFieldLabel(field: string, t: TFunction): string {
  return t(`queryBuilder.fields.${field}`);
}

/** Get translated reason a field or state value is unavailable on this instance */
export function getCapabilityReason(capability: CapabilityKey, t: TFunction): string {
  // Only a local instance has file identity until #2726, so it shares that reason.
  const reason = capability === "fileIdentity" ? "localFilesystemAccess" : capability;
  return t(`queryBuilder.capabilityReasons.${reason}`, { ns: "automations", defaultValue: CAPABILITY_REASONS[reason] });
}

/** Get translated label for a field group */
export function getFieldGroupLabel(label: string, t: TFunction): string {
  return t(`queryBuilder.fieldGroups.${label}`, { defaultValue: label });
}

/** Operator label lookup keyed by the operator value string */
const OPERATOR_LABEL_KEYS: Record<string, string> = {
  EQUAL: "equals",
  NOT_EQUAL: "notEquals",
  CONTAINS: "contains",
  NOT_CONTAINS: "notContains",
  STARTS_WITH: "startsWith",
  ENDS_WITH: "endsWith",
  MATCHES: "matchesRegex",
  GREATER_THAN: ">",
  GREATER_THAN_OR_EQUAL: ">=",
  LESS_THAN: "<",
  LESS_THAN_OR_EQUAL: "<=",
  BETWEEN: "between",
  EXISTS_IN: "existsIn",
  CONTAINS_IN: "similarExistsIn",
};

/** Get translated operators for a field, preserving the original structure */
export function getTranslatedOperatorsForField(field: string, t: TFunction): { value: string; label: string }[] {
  const ops = getOperatorsForField(field);
  return ops.map((op) => {
    // Symbolic operators (=, !=, >, >=, <, <=) stay as-is
    if (/^[^a-zA-Z]/.test(op.label)) return op;
    const key = OPERATOR_LABEL_KEYS[op.value];
    if (!key) return op;
    // "is" / "is not" share keys with equals/notEquals for state/boolean types but have different labels
    const type = getFieldType(field);
    if ((type === "state" || type === "trackerStatus" || type === "boolean" || type === "hardlinkScope" || type === "seasonPackStatus") && (op.value === "EQUAL" || op.value === "NOT_EQUAL")) {
      return { value: op.value, label: t(`queryBuilder.operators.${op.value === "EQUAL" ? "is" : "isNot"}`, { defaultValue: op.label }) };
    }
    return { value: op.value, label: t(`queryBuilder.operators.${key}`, { defaultValue: op.label }) };
  });
}

/** Get translated torrent states */
export function getTranslatedTorrentStates(t: TFunction): { value: string; label: string }[] {
  return TORRENT_STATES.map((state) => ({
    value: state.value,
    label: t(`queryBuilder.torrentStates.${state.value}`, { defaultValue: state.label }),
  }));
}

/** Get translated tracker status values */
export function getTranslatedTrackerStatuses(t: TFunction): { value: string; label: string }[] {
  return TRACKER_STATUS_VALUES.map((status) => ({
    value: status.value,
    label: t(`queryBuilder.trackerStatuses.${status.value}`, { defaultValue: status.label }),
  }));
}

/** Get translated hardlink scope values */
export function getTranslatedHardlinkScopes(t: TFunction): { value: string; label: string }[] {
  return HARDLINK_SCOPE_VALUES.map((scope) => ({
    value: scope.value,
    label: t(`queryBuilder.hardlinkScopes.${scope.value}`, { defaultValue: scope.label }),
  }));
}

/** Get translated content type values */
export function getTranslatedContentTypes(t: TFunction): { value: string; label: string }[] {
  return CONTENT_TYPE_VALUES.map((contentType) => ({
    value: contentType.value,
    label: t(`common:contentTypeLabels.${contentType.value}`, { defaultValue: contentType.label }),
  }));
}

/** Get translated season pack status values */
export function getTranslatedSeasonPackStatuses(t: TFunction): { value: string; label: string }[] {
  return SEASON_PACK_STATUS_VALUES.map((status) => ({
    value: status.value,
    label: t(`queryBuilder.seasonPackStatuses.${status.value}`, { defaultValue: status.label }),
  }));
}
