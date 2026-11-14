/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { localDateOfUtc, utcDayStart, type StatisticsPeriod } from "@/lib/statistics"
import type { TransferHistoryLibraryRow } from "@/types"

const DAY = 86_400_000

/** The library of the selected instances on one UTC day. A value is null when the day has no stored value for it. */
export interface LibraryPoint {
  /** The local midnight of the date of the UTC day, so that a label shows the date of the stored day in each timezone. */
  start: number
  torrents: number | null
  totalSize: number | null
  uniqueSize: number | null
}

/**
 * Builds one point for each UTC day from the first stored day of the period to the end of the period, with the
 * instances added together. recording tells for each instance whether qui records it now. An instance counts on each
 * day from its first stored day: to the end of the period when qui records it, else to its last stored day. A day on
 * which a counted instance has no size is null, and so is a Unique size that a counted instance lacks, so that a
 * missing instance does not draw a drop.
 */
export function buildLibraryPoints(rowsByInstance: TransferHistoryLibraryRow[][], period: StatisticsPeriod, recording: boolean[]): LibraryPoint[] {
  const days = new Map<number, TransferHistoryLibraryRow[]>()
  const spans: { first: number; last: number }[] = []
  rowsByInstance.forEach((rows, index) => {
    for (const row of rows) {
      if (row.totalSize === undefined) {
        continue
      }
      const day = Date.parse(row.day)
      days.set(day, [...days.get(day) ?? [], row])
      const span = spans[index] ??= { first: day, last: day }
      span.first = Math.min(span.first, day)
      span.last = Math.max(span.last, day)
    }
  })
  if (days.size === 0) {
    return []
  }
  const points: LibraryPoint[] = []
  const lastDay = utcDayStart(new Date(period.to.getTime() - 1))
  for (let day = Math.min(...days.keys()); day <= lastDay; day += DAY) {
    const rows = days.get(day) ?? []
    const counted = spans.filter((span, index) => span && span.first <= day && (recording[index] || day <= span.last)).length
    const sum = (value: (row: TransferHistoryLibraryRow) => number | undefined) =>
      rows.length === counted && rows.every(row => value(row) !== undefined) ? rows.reduce((total, row) => total + value(row)!, 0) : null
    points.push({ start: localDateOfUtc(day), torrents: sum(row => row.torrents), totalSize: sum(row => row.totalSize), uniqueSize: sum(row => row.uniqueSize) })
  }
  return points
}

/** Returns the newest point of the period that has a Unique size. A point with a Unique size also has a total size. */
export function newestUniqueSize(points: LibraryPoint[]): (LibraryPoint & { uniqueSize: number; totalSize: number }) | undefined {
  return [...points].reverse().find((point): point is LibraryPoint & { uniqueSize: number; totalSize: number } => point.uniqueSize !== null && point.totalSize !== null)
}
