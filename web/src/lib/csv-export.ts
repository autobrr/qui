/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/**
 * Column definition for CSV export.
 */
export interface CsvColumn<T> {
  header: string
  accessor: (row: T) => string | number | boolean | null | undefined
}

/**
 * Escape a value for CSV format (RFC 4180).
 * Wraps in quotes if contains comma, newline, or double-quote.
 */
function escapeCsvValue(value: string | number | boolean | null | undefined): string {
  if (value == null) return ""
  const str = String(value)
  if (str.includes(",") || str.includes("\n") || str.includes("\r") || str.includes("\"")) {
    return `"${str.replace(/"/g, "\"\"")}"`
  }
  return str
}

/**
 * Convert rows to CSV string.
 */
export function toCsv<T>(rows: T[], columns: CsvColumn<T>[]): string {
  const headerLine = columns.map(c => escapeCsvValue(c.header)).join(",")
  const dataLines = rows.map(row =>
    columns.map(col => escapeCsvValue(col.accessor(row))).join(",")
  )
  return [headerLine, ...dataLines].join("\n")
}

/**
 * Trigger a file download in the browser.
 */
export function downloadBlob(content: string, filename: string, mimeType = "text/csv;charset=utf-8"): void {
  const blob = new Blob([content], { type: mimeType })
  const url = URL.createObjectURL(blob)
  const link = document.createElement("a")
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}
