/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnFilter, DurationUnit, FilterOperation, SizeUnit } from "@/components/torrents/ColumnFilterPopover"
import { DATE_COLUMNS, DURATION_COLUMNS, SIZE_COLUMNS } from "@/lib/column-constants"
import type { Torrent } from "@/types"

const COLUMN_TO_QB_FIELD: Partial<Record<keyof Torrent, string>> = {
  name: "Name",
  size: "Size",
  total_size: "TotalSize",
  progress: "Progress",
  state: "State",
  num_seeds: "NumSeeds",
  num_complete: "NumComplete",
  num_leechs: "NumLeechs",
  num_incomplete: "NumIncomplete",
  dlspeed: "DlSpeed",
  upspeed: "UpSpeed",
  eta: "ETA",
  time_active: "TimeActive",
  seeding_time: "SeedingTime",
  ratio: "Ratio",
  ratio_limit: "RatioLimit",
  popularity: "Popularity",
  category: "Category",
  tags: "Tags",
  added_on: "AddedOn",
  completion_on: "CompletionOn",
  seen_complete: "SeenComplete",
  last_activity: "LastActivity",
  tracker: "Tracker",
  dl_limit: "DlLimit",
  up_limit: "UpLimit",
  downloaded: "Downloaded",
  uploaded: "Uploaded",
  downloaded_session: "DownloadedSession",
  uploaded_session: "UploadedSession",
  amount_left: "AmountLeft",
  completed: "Completed",
  save_path: "SavePath",
  availability: "Availability",
  infohash_v1: "InfohashV1",
  infohash_v2: "InfohashV2",
  reannounce: "Reannounce",
  private: "Private",
  priority: "Priority",
}

const OPERATION_TO_EXPR: Record<FilterOperation, string> = {
  eq: "==",
  ne: "!=",
  gt: ">",
  ge: ">=",
  lt: "<",
  le: "<=",
  between: "between",
  contains: "contains",
  notContains: "not contains",
  startsWith: "startsWith",
  endsWith: "endsWith",
}

function escapeExprValue(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/"/g, "\\\"")
}

function convertSizeToBytes(value: number, unit: SizeUnit): number {
  const k = 1024
  const unitMultipliers: Record<SizeUnit, number> = {
    B: 1,
    KiB: k,
    MiB: k ** 2,
    GiB: k ** 3,
    TiB: k ** 4,
  }
  return Math.floor(value * unitMultipliers[unit])
}

function convertDateToTimestamp(dateStr: string): number {
  const date = new Date(dateStr)
  return Math.floor(date.getTime() / 1000)
}

function convertDurationToSeconds(value: number, unit: DurationUnit): number {
  const unitMultipliers: Record<DurationUnit, number> = {
    seconds: 1,
    minutes: 60,
    hours: 3600,
    days: 86400,
  }
  return Math.floor(value * unitMultipliers[unit])
}

/**
 * Converts a column filter to qBittorrent expr format
 *
 * Examples:
 * - { columnId: "ratio", operation: "gt", value: "2" } => "Ratio > 2"
 * - { columnId: "name", operation: "contains", value: "linux" } => "Name contains \"linux\""
 * - { columnId: "state", operation: "eq", value: "downloading" } => "State == \"downloading\""
 * - { columnId: "size", operation: "gt", value: "10", sizeUnit: "GiB" } => "Size > 10737418240"
 * - { columnId: "added_on", operation: "gt", value: "2024-01-01" } => "AddedOn > 1704067200"
 */
export function columnFilterToExpr(filter: ColumnFilter): string | null {
  const fieldName = COLUMN_TO_QB_FIELD[filter.columnId as keyof Torrent]

  if (!fieldName) {
    console.warn(`Unknown column ID: ${filter.columnId}`)
    return null
  }

  const operator = OPERATION_TO_EXPR[filter.operation]

  if (!operator) {
    console.warn(`Unknown operation: ${filter.operation}`)
    return null
  }

  if (filter.operation === "between") {
    if (!filter.value2) {
      console.warn(`Between operation requires value2 for column ${filter.columnId}`)
      return null
    }

    if (SIZE_COLUMNS.includes(filter.columnId as typeof SIZE_COLUMNS[number]) && filter.sizeUnit) {
      const numericValue1 = Number(filter.value)
      const numericValue2 = Number(filter.value2)
      if (isNaN(numericValue1) || isNaN(numericValue2)) {
        console.warn(`Invalid numeric values for size column ${filter.columnId}`)
        return null
      }
      const bytesValue1 = convertSizeToBytes(numericValue1, filter.sizeUnit)
      const bytesValue2 = convertSizeToBytes(numericValue2, filter.sizeUnit2 || filter.sizeUnit)
      return `(${fieldName} >= ${bytesValue1} && ${fieldName} <= ${bytesValue2})`
    }

    if (DURATION_COLUMNS.includes(filter.columnId as typeof DURATION_COLUMNS[number]) && filter.durationUnit) {
      const numericValue1 = Number(filter.value)
      const numericValue2 = Number(filter.value2)
      if (isNaN(numericValue1) || isNaN(numericValue2)) {
        console.warn(`Invalid numeric values for duration column ${filter.columnId}`)
        return null
      }
      const secondsValue1 = convertDurationToSeconds(numericValue1, filter.durationUnit)
      const secondsValue2 = convertDurationToSeconds(numericValue2, filter.durationUnit2 || filter.durationUnit)
      return `(${fieldName} >= ${secondsValue1} && ${fieldName} <= ${secondsValue2})`
    }

    if (DATE_COLUMNS.includes(filter.columnId as typeof DATE_COLUMNS[number])) {
      const timestamp1 = convertDateToTimestamp(filter.value)
      const timestamp2 = convertDateToTimestamp(filter.value2)
      if (isNaN(timestamp1) || isNaN(timestamp2)) {
        console.warn(`Invalid date values for date column ${filter.columnId}`)
        return null
      }
      return `(${fieldName} >= ${timestamp1} && ${fieldName} <= ${timestamp2})`
    }

    const numericValue1 = Number(filter.value)
    const numericValue2 = Number(filter.value2)
    if (isNaN(numericValue1) || isNaN(numericValue2)) {
      console.warn(`Invalid numeric values for column ${filter.columnId}`)
      return null
    }
    return `(${fieldName} >= ${numericValue1} && ${fieldName} <= ${numericValue2})`
  }

  if (SIZE_COLUMNS.includes(filter.columnId as typeof SIZE_COLUMNS[number]) && filter.sizeUnit) {
    const numericValue = Number(filter.value)
    if (isNaN(numericValue)) {
      console.warn(`Invalid numeric value for size column ${filter.columnId}: ${filter.value}`)
      return null
    }
    const bytesValue = convertSizeToBytes(numericValue, filter.sizeUnit)
    return `${fieldName} ${operator} ${bytesValue}`
  }

  if (DURATION_COLUMNS.includes(filter.columnId as typeof DURATION_COLUMNS[number]) && filter.durationUnit) {
    const numericValue = Number(filter.value)
    if (isNaN(numericValue)) {
      console.warn(`Invalid numeric value for duration column ${filter.columnId}: ${filter.value}`)
      return null
    }
    const secondsValue = convertDurationToSeconds(numericValue, filter.durationUnit)
    return `${fieldName} ${operator} ${secondsValue}`
  }

  if (DATE_COLUMNS.includes(filter.columnId as typeof DATE_COLUMNS[number])) {
    const timestamp = convertDateToTimestamp(filter.value)
    if (isNaN(timestamp)) {
      console.warn(`Invalid date value for date column ${filter.columnId}: ${filter.value}`)
      return null
    }
    return `${fieldName} ${operator} ${timestamp}`
  }

  const needsQuotes = isNaN(Number(filter.value)) ||
    filter.columnId === "state" ||
    filter.columnId === "category" ||
    filter.columnId === "tags" ||
    filter.columnId === "name" ||
    filter.columnId === "tracker" ||
    filter.columnId === "save_path" ||
    filter.columnId === "infohash_v1" ||
    filter.columnId === "infohash_v2"

  let escapedValue = filter.value

  if (needsQuotes) {
    escapedValue = escapeExprValue(filter.value)
    return `${fieldName} ${operator} "${escapedValue}"`
  } else {
    return `${fieldName} ${operator} ${filter.value}`
  }
}

/**
 * Converts multiple column filters to a combined expr string
 * Multiple filters are combined with AND logic
 *
 * Example:
 * [
 *   { columnId: "ratio", operation: "gt", value: "2" },
 *   { columnId: "state", operation: "eq", value: "downloading" }
 * ]
 * => "Ratio > 2 && State == \"downloading\""
 */
export function columnFiltersToExpr(filters: ColumnFilter[], operator: string = "and"): string | null {
  if (!filters || filters.length === 0) {
    return null
  }

  const exprParts = filters
    .map(columnFilterToExpr)
    .filter((expr): expr is string => expr !== null)

  if (exprParts.length === 0) {
    return null
  }

  return exprParts.join(` ${operator} `)
}

export function getColumnType(columnId: string): "number" | "string" | "date" {
  const numericColumns = [
    "size", "total_size", "progress", "num_seeds", "num_complete",
    "num_leechs", "num_incomplete", "dlspeed", "upspeed", "eta",
    "ratio", "ratio_limit", "dl_limit", "up_limit", "downloaded",
    "uploaded", "downloaded_session", "uploaded_session", "amount_left",
    "time_active", "seeding_time", "completed", "availability",
    "reannounce", "priority", "popularity",
  ]

  const dateColumns = [
    "added_on", "completion_on", "seen_complete", "last_activity",
  ]

  if (numericColumns.includes(columnId)) {
    return "number"
  }

  if (dateColumns.includes(columnId)) {
    return "date"
  }

  return "string"
}
