// Field definitions with metadata for the query builder UI
export const CONDITION_FIELDS = {
  // String fields
  NAME: { label: "Name", type: "string" as const, description: "Torrent name" },
  HASH: { label: "Hash", type: "string" as const, description: "Torrent info hash" },
  CATEGORY: { label: "Category", type: "string" as const, description: "Torrent category" },
  TAGS: { label: "Tags", type: "string" as const, description: "Comma-separated tags" },
  SAVE_PATH: { label: "Save Path", type: "string" as const, description: "Download location" },
  CONTENT_PATH: { label: "Content Path", type: "string" as const, description: "Content location" },
  STATE: { label: "State", type: "state" as const, description: "Torrent state" },
  TRACKER: { label: "Tracker", type: "string" as const, description: "Primary tracker URL" },
  COMMENT: { label: "Comment", type: "string" as const, description: "Torrent comment" },

  // Size fields (bytes)
  SIZE: { label: "Size", type: "bytes" as const, description: "Selected file size" },
  TOTAL_SIZE: { label: "Total Size", type: "bytes" as const, description: "Total torrent size" },
  DOWNLOADED: { label: "Downloaded", type: "bytes" as const, description: "Total downloaded" },
  UPLOADED: { label: "Uploaded", type: "bytes" as const, description: "Total uploaded" },
  AMOUNT_LEFT: { label: "Amount Left", type: "bytes" as const, description: "Remaining to download" },

  // Duration fields (seconds)
  SEEDING_TIME: { label: "Seeding Time", type: "duration" as const, description: "Time spent seeding" },
  TIME_ACTIVE: { label: "Time Active", type: "duration" as const, description: "Total active time" },

  // Timestamp fields (unix)
  ADDED_ON: { label: "Added On", type: "timestamp" as const, description: "When torrent was added" },
  COMPLETION_ON: { label: "Completed On", type: "timestamp" as const, description: "When download completed" },
  LAST_ACTIVITY: { label: "Last Activity", type: "timestamp" as const, description: "Last activity timestamp" },

  // Float fields
  RATIO: { label: "Ratio", type: "float" as const, description: "Upload/download ratio" },
  PROGRESS: { label: "Progress", type: "float" as const, description: "Download progress (0-1)" },
  AVAILABILITY: { label: "Availability", type: "float" as const, description: "Distributed copies" },

  // Speed fields (bytes/s)
  DL_SPEED: { label: "Download Speed", type: "speed" as const, description: "Current download speed" },
  UP_SPEED: { label: "Upload Speed", type: "speed" as const, description: "Current upload speed" },

  // Count fields
  NUM_SEEDS: { label: "Seeds", type: "integer" as const, description: "Connected seeds" },
  NUM_LEECHS: { label: "Leechers", type: "integer" as const, description: "Connected leechers" },
  NUM_COMPLETE: { label: "Complete", type: "integer" as const, description: "Seeds in swarm" },
  NUM_INCOMPLETE: { label: "Incomplete", type: "integer" as const, description: "Leechers in swarm" },
  TRACKERS_COUNT: { label: "Trackers", type: "integer" as const, description: "Number of trackers" },

  // Boolean fields
  PRIVATE: { label: "Private", type: "boolean" as const, description: "Private tracker torrent" },
  IS_UNREGISTERED: { label: "Unregistered", type: "boolean" as const, description: "Tracker reports torrent as unregistered" },
} as const;

export type FieldType = "string" | "state" | "bytes" | "duration" | "timestamp" | "float" | "speed" | "integer" | "boolean";

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
  timestamp: [
    { value: "EQUAL", label: "=" },
    { value: "NOT_EQUAL", label: "!=" },
    { value: "GREATER_THAN", label: "after" },
    { value: "GREATER_THAN_OR_EQUAL", label: "on or after" },
    { value: "LESS_THAN", label: "before" },
    { value: "LESS_THAN_OR_EQUAL", label: "on or before" },
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
};

// qBittorrent torrent states
export const TORRENT_STATES = [
  { value: "error", label: "Error" },
  { value: "missingFiles", label: "Missing Files" },
  { value: "uploading", label: "Uploading" },
  { value: "pausedUP", label: "Paused (Seeding)" },
  { value: "queuedUP", label: "Queued (Seeding)" },
  { value: "stalledUP", label: "Stalled (Seeding)" },
  { value: "checkingUP", label: "Checking (Seeding)" },
  { value: "forcedUP", label: "Forced Upload" },
  { value: "allocating", label: "Allocating" },
  { value: "downloading", label: "Downloading" },
  { value: "metaDL", label: "Downloading Metadata" },
  { value: "pausedDL", label: "Paused (Downloading)" },
  { value: "queuedDL", label: "Queued (Downloading)" },
  { value: "stalledDL", label: "Stalled (Downloading)" },
  { value: "checkingDL", label: "Checking (Downloading)" },
  { value: "forcedDL", label: "Forced Download" },
  { value: "checkingResumeData", label: "Checking Resume Data" },
  { value: "moving", label: "Moving" },
  { value: "unknown", label: "Unknown" },
];

// Delete mode options
export const DELETE_MODES = [
  { value: "delete", label: "Remove from client" },
  { value: "deleteWithFiles", label: "Remove with files" },
  { value: "deleteWithFilesPreserveCrossSeeds", label: "Remove with files (preserve cross-seeds)" },
];

// Field groups for organized selection
export const FIELD_GROUPS = [
  {
    label: "Identity",
    fields: ["NAME", "HASH", "CATEGORY", "TAGS", "STATE"],
  },
  {
    label: "Paths",
    fields: ["SAVE_PATH", "CONTENT_PATH"],
  },
  {
    label: "Size",
    fields: ["SIZE", "TOTAL_SIZE", "DOWNLOADED", "UPLOADED", "AMOUNT_LEFT"],
  },
  {
    label: "Time",
    fields: ["SEEDING_TIME", "TIME_ACTIVE", "ADDED_ON", "COMPLETION_ON", "LAST_ACTIVITY"],
  },
  {
    label: "Progress",
    fields: ["RATIO", "PROGRESS", "AVAILABILITY"],
  },
  {
    label: "Speed",
    fields: ["DL_SPEED", "UP_SPEED"],
  },
  {
    label: "Peers",
    fields: ["NUM_SEEDS", "NUM_LEECHS", "NUM_COMPLETE", "NUM_INCOMPLETE"],
  },
  {
    label: "Tracker",
    fields: ["TRACKER", "TRACKERS_COUNT", "PRIVATE", "IS_UNREGISTERED", "COMMENT"],
  },
];

// Helper to get field type
export function getFieldType(field: string): FieldType {
  const fieldDef = CONDITION_FIELDS[field as keyof typeof CONDITION_FIELDS];
  return fieldDef?.type ?? "string";
}

// Helper to get operators for a field
export function getOperatorsForField(field: string) {
  const type = getFieldType(field);
  return OPERATORS_BY_TYPE[type];
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
