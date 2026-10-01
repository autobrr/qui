/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent } from "@/types"

export const NUMERIC_COLUMNS = [
  // "size",
  // "total_size",
  // "progress",
  "num_seeds",
  "num_complete",
  "num_leechs",
  "num_incomplete",
  // "eta",
  "ratio",
  "ratio_limit",
  // "downloaded",
  // "uploaded",
  // "downloaded_session",
  // "uploaded_session",
  // "amount_left",
  // "time_active",
  // "seeding_time",
  // "completed",
  "availability",
  // "reannounce",
  "priority",
  "popularity",
] as const satisfies readonly (keyof Torrent)[]

export const SIZE_COLUMNS = [
  "size",
  "total_size",
  "downloaded",
  "uploaded",
  "downloaded_session",
  "uploaded_session",
  "amount_left",
  "completed",
] as const satisfies readonly (keyof Torrent)[]

export const SPEED_COLUMNS = [
  "dlspeed",
  "upspeed",
  "dl_limit",
  "up_limit",
] as const satisfies readonly (keyof Torrent)[]

export const DURATION_COLUMNS = [
  "eta",
  "time_active",
  "seeding_time",
  "reannounce",
] as const satisfies readonly (keyof Torrent)[]

export const PERCENTAGE_COLUMNS = [
  "progress",
] as const satisfies readonly (keyof Torrent)[]

export const DATE_COLUMNS = [
  "added_on",
  "completion_on",
  "seen_complete",
  "last_activity",
] as const satisfies readonly (keyof Torrent)[]

export const BOOLEAN_COLUMNS = [
  "private",
] as const satisfies readonly (keyof Torrent)[]

export const ENUM_COLUMNS = [
  "state",
] as const satisfies readonly (keyof Torrent)[]


export type ColumnType =
  "number"
  | "size"
  | "speed"
  | "duration"
  | "percentage"
  | "date"
  | "boolean"
  | "enum"
  | "string"

export type FilterOperation =
  | "eq" // equals
  | "ne" // not equals
  | "gt" // greater than
  | "ge" // greater than or equal
  | "lt" // less than
  | "le" // less than or equal
  | "between"
  | "contains"
  | "notContains"
  | "startsWith"
  | "endsWith"

export type SizeUnit =
  "B"
  | "KiB"
  | "MiB"
  | "GiB"
  | "TiB"

export type SpeedUnit =
  "B/s"
  | "KiB/s"
  | "MiB/s"
  | "GiB/s"
  | "TiB/s"

export type DurationUnit =
  "seconds"
  | "minutes"
  | "hours"
  | "days"

// labelKey is a key in the torrents namespace.
export const NUMERIC_OPERATIONS: { value: FilterOperation; labelKey: string }[] = [
  { value: "eq", labelKey: "columnFilter.operations.equalTo" },
  { value: "ne", labelKey: "columnFilter.operations.notEqualTo" },
  { value: "gt", labelKey: "columnFilter.operations.greaterThan" },
  { value: "ge", labelKey: "columnFilter.operations.greaterThanOrEqual" },
  { value: "lt", labelKey: "columnFilter.operations.lessThan" },
  { value: "le", labelKey: "columnFilter.operations.lessThanOrEqual" },
  { value: "between", labelKey: "columnFilter.operations.between" },
]

export const STRING_OPERATIONS: { value: FilterOperation; labelKey: string }[] = [
  { value: "eq", labelKey: "columnFilter.operations.equals" },
  { value: "ne", labelKey: "columnFilter.operations.notEquals" },
  { value: "contains", labelKey: "columnFilter.operations.contains" },
  { value: "notContains", labelKey: "columnFilter.operations.doesNotContain" },
  { value: "startsWith", labelKey: "columnFilter.operations.startsWith" },
  { value: "endsWith", labelKey: "columnFilter.operations.endsWith" },
]

export const DATE_OPERATIONS: { value: FilterOperation; labelKey: string }[] = [
  { value: "eq", labelKey: "columnFilter.operations.on" },
  { value: "gt", labelKey: "columnFilter.operations.after" },
  { value: "lt", labelKey: "columnFilter.operations.before" },
  { value: "between", labelKey: "columnFilter.operations.between" },
]

export const BOOLEAN_OPERATIONS: { value: FilterOperation; labelKey: string }[] = [
  { value: "eq", labelKey: "columnFilter.operations.is" },
  { value: "ne", labelKey: "columnFilter.operations.isNot" },
]
