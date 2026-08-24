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

export const NUMERIC_OPERATIONS: { value: FilterOperation; label: string }[] = [
  { value: "eq", label: "Equal to" },
  { value: "ne", label: "Not equal to" },
  { value: "gt", label: "Greater than" },
  { value: "ge", label: "Greater than or equal" },
  { value: "lt", label: "Less than" },
  { value: "le", label: "Less than or equal" },
  { value: "between", label: "Between" },
]

export const STRING_OPERATIONS: { value: FilterOperation; label: string }[] = [
  { value: "eq", label: "Equals" },
  { value: "ne", label: "Not equals" },
  { value: "contains", label: "Contains" },
  { value: "notContains", label: "Does not contain" },
  { value: "startsWith", label: "Starts with" },
  { value: "endsWith", label: "Ends with" },
]

export const DATE_OPERATIONS: { value: FilterOperation; label: string }[] = [
  { value: "eq", label: "On" },
  { value: "gt", label: "After" },
  { value: "lt", label: "Before" },
  { value: "between", label: "Between" },
]

export const BOOLEAN_OPERATIONS: { value: FilterOperation; label: string }[] = [
  { value: "eq", label: "Is" },
  { value: "ne", label: "Is not" },
]
