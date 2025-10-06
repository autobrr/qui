/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent } from "@/types"

export const NUMERIC_COLUMNS = [
  "size",
  "total_size",
  "progress",
  "num_seeds",
  "num_complete",
  "num_leechs",
  "num_incomplete",
  "dlspeed",
  "upspeed",
  "eta",
  "ratio",
  "ratio_limit",
  "dl_limit",
  "up_limit",
  "downloaded",
  "uploaded",
  "downloaded_session",
  "uploaded_session",
  "amount_left",
  "time_active",
  "seeding_time",
  "completed",
  "availability",
  "reannounce",
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

export const DATE_COLUMNS = [
  "added_on",
  "completion_on",
  "seen_complete",
  "last_activity",
] as const satisfies readonly (keyof Torrent)[]

export const DURATION_COLUMNS = [
  "eta",
  "time_active",
  "seeding_time",
  "reannounce",
] as const satisfies readonly (keyof Torrent)[]

export const BOOLEAN_COLUMNS = [
  "private",
] as const satisfies readonly (keyof Torrent)[]

export type ColumnType = "number" | "size" | "date" | "duration" | "boolean" | "string"

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
