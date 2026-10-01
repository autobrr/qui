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

// Field definitions with metadata for the query builder UI
export const CONDITION_FIELDS = {
  // String fields
  NAME: { type: "string" as const },
  HASH: { type: "string" as const },
  INFOHASH_V1: { type: "string" as const },
  INFOHASH_V2: { type: "string" as const },
  MAGNET_URI: { type: "string" as const },
  CATEGORY: { type: "string" as const },
  TAGS: { type: "string" as const },
  SAVE_PATH: { type: "string" as const },
  CONTENT_PATH: { type: "string" as const },
  DOWNLOAD_PATH: { type: "string" as const },
  CREATED_BY: { type: "string" as const },
  // Legacy alias of TRACKER, which now matches every tracker too. Kept out of
  // FIELD_GROUPS so it is no longer offered, and kept here because the locale-key
  // test reads these keys, so it still covers the label saved rules using it render.
  TRACKERS: { type: "string" as const },
  CONTENT_TYPE: { type: "string" as const },
  EFFECTIVE_NAME: { type: "string" as const },
  RLS_SOURCE: { type: "string" as const },
  RLS_RESOLUTION: { type: "string" as const },
  RLS_CODEC: { type: "string" as const },
  RLS_HDR: { type: "string" as const },
  RLS_AUDIO: { type: "string" as const },
  RLS_CHANNELS: { type: "string" as const },
  RLS_GROUP: { type: "string" as const },
  RLS_YEAR: { type: "integer" as const },
  STATE: { type: "state" as const },
  TRACKER: { type: "string" as const },
  TRACKER_STATUS: { type: "trackerStatus" as const },
  TRACKER_MESSAGE: { type: "string" as const },
  COMMENT: { type: "string" as const },

  // Size fields (bytes)
  SIZE: { type: "bytes" as const },
  TOTAL_SIZE: { type: "bytes" as const },
  COMPLETED: { type: "bytes" as const },
  DOWNLOADED: { type: "bytes" as const },
  DOWNLOADED_SESSION: { type: "bytes" as const },
  UPLOADED: { type: "bytes" as const },
  UPLOADED_SESSION: { type: "bytes" as const },
  AMOUNT_LEFT: { type: "bytes" as const },
  FREE_SPACE: { type: "bytes" as const },

  // Timestamp-backed fields represented as ages (seconds since event)
  ADDED_ON: { type: "duration" as const },
  COMPLETION_ON: { type: "duration" as const },
  LAST_ACTIVITY: { type: "duration" as const },
  SEEN_COMPLETE: { type: "duration" as const },

  // Duration fields (seconds)
  ETA: { type: "duration" as const },
  REANNOUNCE: { type: "duration" as const },
  SEEDING_TIME: { type: "duration" as const },
  TIME_ACTIVE: { type: "duration" as const },
  MAX_SEEDING_TIME: { type: "duration" as const },
  MAX_INACTIVE_SEEDING_TIME: { type: "duration" as const },
  SEEDING_TIME_LIMIT: { type: "duration" as const },
  INACTIVE_SEEDING_TIME_LIMIT: { type: "duration" as const },
  ADDED_ON_AGE: { type: "duration" as const },
  COMPLETION_ON_AGE: { type: "duration" as const },
  LAST_ACTIVITY_AGE: { type: "duration" as const },

  // System Time fields
  SYSTEM_HOUR: { type: "integer" as const },
  SYSTEM_MINUTE: { type: "integer" as const },
  SYSTEM_DAY_OF_WEEK: { type: "integer" as const },
  SYSTEM_DAY: { type: "integer" as const },
  SYSTEM_MONTH: { type: "integer" as const },
  SYSTEM_YEAR: { type: "integer" as const },

  // Float fields
  RATIO: { type: "float" as const },
  RATIO_LIMIT: { type: "float" as const },
  MAX_RATIO: { type: "float" as const },
  UPLOADED_OVER_SIZE: { type: "float" as const },
  PROGRESS: { type: "percentage" as const },
  AVAILABILITY: { type: "float" as const },
  POPULARITY: { type: "float" as const },

  // Speed fields (bytes/s)
  DL_SPEED: { type: "speed" as const },
  UP_SPEED: { type: "speed" as const },
  DL_LIMIT: { type: "speed" as const },
  UP_LIMIT: { type: "speed" as const },

  // Count fields
  NUM_SEEDS: { type: "integer" as const },
  NUM_LEECHS: { type: "integer" as const },
  NUM_COMPLETE: { type: "integer" as const },
  NUM_INCOMPLETE: { type: "integer" as const },
  TRACKERS_COUNT: { type: "integer" as const },
  PRIORITY: { type: "integer" as const },
  GROUP_SIZE: { type: "integer" as const },

  // Boolean fields
  PRIVATE: { type: "boolean" as const },
  AUTO_MANAGED: { type: "boolean" as const },
  FIRST_LAST_PIECE_PRIO: { type: "boolean" as const },
  FORCE_START: { type: "boolean" as const },
  SEQUENTIAL_DOWNLOAD: { type: "boolean" as const },
  SUPER_SEEDING: { type: "boolean" as const },
  IS_UNREGISTERED: { type: "boolean" as const },
  HAS_MISSING_FILES: { type: "boolean" as const },
  HAS_SKIPPED_FILES: { type: "boolean" as const },
  IS_GROUPED: { type: "boolean" as const },
  EXISTS_ON_OTHER_INSTANCE: { type: "boolean" as const },
  SEEDING_ON_OTHER_INSTANCE: { type: "boolean" as const },
  EXISTS_ON_SAME_INSTANCE: { type: "boolean" as const },
  SEEDING_ON_SAME_INSTANCE: { type: "boolean" as const },
  CROSS_SEED_TAGS: { type: "string" as const },
  SEASON_PACK_STATUS: { type: "seasonPackStatus" as const },
  SEASON_PACK_STATUS_ANY_INSTANCE: { type: "seasonPackStatus" as const },

  // Enum-like fields
  HARDLINK_SCOPE: { type: "hardlinkScope" as const },
  HARDLINK_SCOPE_CROSS: { type: "hardlinkScope" as const },
} as const;

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
