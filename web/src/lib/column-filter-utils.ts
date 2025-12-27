/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ColumnType, DurationUnit, FilterOperation, SizeUnit, SpeedUnit } from "@/lib/column-constants"
import {
  BOOLEAN_COLUMNS,
  BOOLEAN_OPERATIONS,
  DATE_COLUMNS,
  DATE_OPERATIONS,
  DURATION_COLUMNS,
  ENUM_COLUMNS,
  NUMERIC_COLUMNS,
  NUMERIC_OPERATIONS,
  PERCENTAGE_COLUMNS,
  SIZE_COLUMNS,
  SPEED_COLUMNS,
  STRING_OPERATIONS
} from "@/lib/column-constants"
import type { CrossInstanceTorrent, Torrent, TorznabSearchResult } from "@/types"

export interface ColumnFilter {
  columnId: string
  operation: FilterOperation
  value: string
  value2?: string
  sizeUnit?: SizeUnit
  sizeUnit2?: SizeUnit
  speedUnit?: SpeedUnit
  speedUnit2?: SpeedUnit
  durationUnit?: DurationUnit
  durationUnit2?: DurationUnit
  caseSensitive?: boolean
}

const COLUMN_TO_QB_FIELD: Partial<Record<keyof (Torrent & CrossInstanceTorrent), string>> = {
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
  instanceName: "InstanceName", // Cross-seed filtering instance column
}

// Remap column IDs for filtering to use total counts instead of connected counts
// This matches the sorting behavior in TorrentTableOptimized.tsx (lines 972-976)
const FILTER_COLUMN_REMAP: Record<string, string> = {
  num_seeds: "num_complete",    // Filter by total seeds, not connected
  num_leechs: "num_incomplete", // Filter by total peers, not connected
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

const COLUMN_TYPE_MAP: Map<string, ColumnType> = new Map([
  ...SIZE_COLUMNS.map(col => [col, "size" as ColumnType] as const),
  ...SPEED_COLUMNS.map(col => [col, "speed" as ColumnType] as const),
  ...DURATION_COLUMNS.map(col => [col, "duration" as ColumnType] as const),
  ...PERCENTAGE_COLUMNS.map(col => [col, "percentage" as ColumnType] as const),
  ...NUMERIC_COLUMNS.map(col => [col, "number" as ColumnType] as const),
  ...DATE_COLUMNS.map(col => [col, "date" as ColumnType] as const),
  ...BOOLEAN_COLUMNS.map(col => [col, "boolean" as ColumnType] as const),
  ...ENUM_COLUMNS.map(col => [col, "enum" as ColumnType] as const),
])

function escapeExprValue(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/"/g, "\\\"")
}

export function convertSizeToBytes(value: number, unit: SizeUnit | SpeedUnit): number {
  const k = 1024
  const unitMultipliers: Record<SizeUnit | SpeedUnit, number> = {
    B: 1,
    KiB: k,
    MiB: k ** 2,
    GiB: k ** 3,
    TiB: k ** 4,
    "B/s": 1,
    "KiB/s": k,
    "MiB/s": k ** 2,
    "GiB/s": k ** 3,
    "TiB/s": k ** 4,
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
  // Apply column remapping for filtering (use total counts instead of connected)
  const effectiveColumnId = FILTER_COLUMN_REMAP[filter.columnId] ?? filter.columnId
  const fieldName = COLUMN_TO_QB_FIELD[effectiveColumnId as keyof Torrent]

  if (!fieldName) {
    console.warn(`Unknown column ID: ${filter.columnId}`)
    return null
  }

  const operator = OPERATION_TO_EXPR[filter.operation]

  if (!operator) {
    console.warn(`Unknown operation: ${filter.operation}`)
    return null
  }

  const columnType = getColumnType(effectiveColumnId)
  const isSizeColumn = columnType === "size"
  const isSpeedColumn = columnType === "speed"
  const isDurationColumn = columnType === "duration"
  const isDateColumn = columnType === "date"
  const isBooleanColumn = columnType === "boolean"
  const isProgressColumn = effectiveColumnId === "progress"

  if (filter.operation === "between") {
    if (!filter.value2) {
      console.warn(`Between operation requires value2 for column ${filter.columnId}`)
      return null
    }

    if (isSizeColumn && filter.sizeUnit) {
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

    if (isSpeedColumn && filter.speedUnit) {
      const numericValue1 = Number(filter.value)
      const numericValue2 = Number(filter.value2)
      if (isNaN(numericValue1) || isNaN(numericValue2)) {
        console.warn(`Invalid numeric values for speed column ${filter.columnId}`)
        return null
      }
      const bytesValue1 = convertSizeToBytes(numericValue1, filter.speedUnit)
      const bytesValue2 = convertSizeToBytes(numericValue2, filter.speedUnit2 || filter.speedUnit)
      return `(${fieldName} >= ${bytesValue1} && ${fieldName} <= ${bytesValue2})`
    }

    if (isDurationColumn && filter.durationUnit) {
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

    if (isDateColumn) {
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
    if (isProgressColumn) {
      const fractionValue1 = numericValue1 / 100
      const fractionValue2 = numericValue2 / 100
      return `(${fieldName} >= ${fractionValue1} && ${fieldName} <= ${fractionValue2})`
    }
    return `(${fieldName} >= ${numericValue1} && ${fieldName} <= ${numericValue2})`
  }

  if (isSizeColumn && filter.sizeUnit) {
    const numericValue = Number(filter.value)
    if (isNaN(numericValue)) {
      console.warn(`Invalid numeric value for size column ${filter.columnId}: ${filter.value}`)
      return null
    }
    const bytesValue = convertSizeToBytes(numericValue, filter.sizeUnit)
    return `${fieldName} ${operator} ${bytesValue}`
  }

  if (isSpeedColumn && filter.speedUnit) {
    const numericValue = Number(filter.value)
    if (isNaN(numericValue)) {
      console.warn(`Invalid numeric value for size column ${filter.columnId}: ${filter.value}`)
      return null
    }
    const bytesValue = convertSizeToBytes(numericValue, filter.speedUnit)
    return `${fieldName} ${operator} ${bytesValue}`
  }

  if (isDurationColumn && filter.durationUnit) {
    const numericValue = Number(filter.value)
    if (isNaN(numericValue)) {
      console.warn(`Invalid numeric value for duration column ${filter.columnId}: ${filter.value}`)
      return null
    }
    const secondsValue = convertDurationToSeconds(numericValue, filter.durationUnit)
    return `${fieldName} ${operator} ${secondsValue}`
  }

  if (isDateColumn) {
    const timestamp = convertDateToTimestamp(filter.value)
    if (isNaN(timestamp)) {
      console.warn(`Invalid date value for date column ${filter.columnId}: ${filter.value}`)
      return null
    }
    return `${fieldName} ${operator} ${timestamp}`
  }

  if (isBooleanColumn) {
    const boolValue = filter.value.toLowerCase() === "true"
    return `${fieldName} ${operator} ${boolValue}`
  }

  if (isProgressColumn) {
    const numericValue = Number(filter.value)
    if (isNaN(numericValue)) {
      console.warn(`Invalid numeric value for progress column ${filter.columnId}: ${filter.value}`)
      return null
    }
    const fractionValue = numericValue / 100
    return `${fieldName} ${operator} ${fractionValue}`
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

  const isStringColumn = columnType === "string"
  const useLowerCase = isStringColumn && filter.caseSensitive === false

  const needsStringCast = filter.columnId === "state"
  const effectiveFieldName = needsStringCast ? `string(${fieldName})` : fieldName

  let escapedValue = filter.value

  if (needsQuotes) {
    escapedValue = escapeExprValue(filter.value)
    if (useLowerCase) {
      return `lower(${effectiveFieldName}) ${operator} "${escapedValue.toLowerCase()}"`
    }
    return `${effectiveFieldName} ${operator} "${escapedValue}"`
  } else {
    return `${effectiveFieldName} ${operator} ${filter.value}`
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

export function getColumnType(columnId: string): ColumnType {
  return COLUMN_TYPE_MAP.get(columnId) || "string"
}

export function getDefaultOperation(columnType: ColumnType): FilterOperation {
  switch (columnType) {
    case "size":
    case "speed":
    case "duration":
    case "percentage":
    case "number":
    case "date":
      return "gt"
    case "enum":
    case "boolean":
      return "eq"
    default:
      return "contains"
  }
}

export function getOperations(columnType: ColumnType) {
  switch (columnType) {
    case "size":
    case "speed":
    case "duration":
    case "percentage":
    case "number":
      return NUMERIC_OPERATIONS
    case "date":
      return DATE_OPERATIONS
    case "enum":
    case "boolean":
      return BOOLEAN_OPERATIONS
    default:
      return STRING_OPERATIONS
  }
}

export function filterSearchResult(result: TorznabSearchResult, filter: ColumnFilter, categoryMap: Map<number, string>): boolean {
  const { columnId, operation, value, value2, caseSensitive } = filter

  let itemValue: string | number | boolean | Date | undefined

  switch (columnId) {
    case "title":
      itemValue = result.title
      break
    case "indexer":
      itemValue = result.indexer
      break
    case "size":
      itemValue = result.size
      break
    case "seeders":
      itemValue = result.seeders
      break
    case "category":
      itemValue = categoryMap.get(result.categoryId) || result.categoryName || ""
      break
    case "source":
      itemValue = result.source || ""
      break
    case "collection":
      itemValue = result.collection || ""
      break
    case "group":
      itemValue = result.group || ""
      break
    case "published":
      itemValue = result.publishDate
      break
    case "freeleech":
      itemValue = result.downloadVolumeFactor === 0
      break
    default:
      return true
  }

  if (itemValue === undefined || itemValue === null) {
    return false
  }

  if (columnId === "size" || columnId === "seeders") {
    const numValue = Number(itemValue)
    let compareValue1 = Number(value)
    let compareValue2 = value2 ? Number(value2) : 0

    if (columnId === "size" && filter.sizeUnit) {
      compareValue1 = convertSizeToBytes(compareValue1, filter.sizeUnit)
      if (value2 && filter.sizeUnit2) {
        compareValue2 = convertSizeToBytes(compareValue2, filter.sizeUnit2)
      } else if (value2) {
        compareValue2 = convertSizeToBytes(compareValue2, filter.sizeUnit)
      }
    }

    if (isNaN(compareValue1)) return true

    switch (operation) {
      case "eq": return numValue === compareValue1
      case "ne": return numValue !== compareValue1
      case "gt": return numValue > compareValue1
      case "ge": return numValue >= compareValue1
      case "lt": return numValue < compareValue1
      case "le": return numValue <= compareValue1
      case "between":
        if (isNaN(compareValue2)) return true
        return numValue >= compareValue1 && numValue <= compareValue2
      default: return true
    }
  }

  if (columnId === "published") {
    const dateValue = new Date(itemValue as string).getTime()
    const compareDate1 = new Date(value).getTime()

    if (isNaN(compareDate1)) return true

    switch (operation) {
      case "eq":
        const d1 = new Date(dateValue)
        const d2 = new Date(compareDate1)
        return d1.getFullYear() === d2.getFullYear() &&
          d1.getMonth() === d2.getMonth() &&
          d1.getDate() === d2.getDate()
      case "gt": return dateValue > compareDate1
      case "lt": return dateValue < compareDate1
      case "between":
        const compareDate2 = new Date(value2 || "").getTime()
        if (isNaN(compareDate2)) return true
        return dateValue >= compareDate1 && dateValue <= compareDate2
      default: return true
    }
  }

  if (columnId === "freeleech") {
    const factor = result.downloadVolumeFactor
    const selectedValues = value.split(",")

    if (selectedValues.length === 0 || (selectedValues.length === 1 && selectedValues[0] === "")) {
      return true
    }

    return selectedValues.some(val => {
      if (val === "true") return factor === 0
      if (val === "false") return factor === 1
      const numVal = Number(val)
      if (!isNaN(numVal)) {
        return factor === numVal
      }
      return false
    })
  }

  const strValue = String(itemValue)
  const strFilter = value

  if (value.includes(",") && (operation === "eq" || operation === "contains")) {
    const selectedValues = value.split(",")
    const a = caseSensitive ? strValue : strValue.toLowerCase()

    return selectedValues.some(val => {
      const b = caseSensitive ? val : val.toLowerCase()
      if (operation === "eq") return a === b
      if (operation === "contains") return a.includes(b)
      return false
    })
  }

  const a = caseSensitive ? strValue : strValue.toLowerCase()
  const b = caseSensitive ? strFilter : strFilter.toLowerCase()

  switch (operation) {
    case "eq": return a === b
    case "ne": return a !== b
    case "contains": return a.includes(b)
    case "notContains": return !a.includes(b)
    case "startsWith": return a.startsWith(b)
    case "endsWith": return a.endsWith(b)
    default: return true
  }
}
