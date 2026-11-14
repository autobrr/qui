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

/** A selected instance: the time of its first recording, and whether qui records it now. */
export interface LibraryInstance {
  firstRecordedAt: string
  recording: boolean
}

/**
 * Builds one point for each UTC day from the first stored day of the period to the end of the period, with the
 * instances added together. An instance counts on each day from the day of its first recording: to the end of the
 * period when qui records it now, else to its last stored day. A day on which a counted instance has no size, or no
 * instance counts, is null, and so is a Unique size that a counted instance lacks, so that a missing instance does not
 * draw a drop.
 */
export function buildLibraryPoints(rowsByInstance: TransferHistoryLibraryRow[][], period: StatisticsPeriod, instances: LibraryInstance[]): LibraryPoint[] {
  const days = new Map<number, TransferHistoryLibraryRow[]>()
  const lastStored: (number | undefined)[] = []
  rowsByInstance.forEach((rows, index) => {
    for (const row of rows) {
      if (row.totalSize === undefined) {
        continue
      }
      const day = Date.parse(row.day)
      days.set(day, [...days.get(day) ?? [], row])
      lastStored[index] = Math.max(lastStored[index] ?? day, day)
    }
  })
  if (days.size === 0) {
    return []
  }
  const firstDays = instances.map(instance => utcDayStart(new Date(instance.firstRecordedAt)))
  const points: LibraryPoint[] = []
  const lastDay = utcDayStart(new Date(period.to.getTime() - 1))
  for (let day = Math.min(...days.keys()); day <= lastDay; day += DAY) {
    const rows = days.get(day) ?? []
    const counted = instances.filter((instance, index) => firstDays[index] <= day && (instance.recording || day <= (lastStored[index] ?? -Infinity))).length
    const sum = (value: (row: TransferHistoryLibraryRow) => number | undefined) =>
      counted > 0 && rows.length === counted && rows.every(row => value(row) !== undefined) ? rows.reduce((total, row) => total + value(row)!, 0) : null
    points.push({ start: localDateOfUtc(day), torrents: sum(row => row.torrents), totalSize: sum(row => row.totalSize), uniqueSize: sum(row => row.uniqueSize) })
  }
  return points
}

/** Returns the newest point of the period that has a Unique size. A point with a Unique size also has a total size. */
export function newestUniqueSize(points: LibraryPoint[]): (LibraryPoint & { uniqueSize: number; totalSize: number }) | undefined {
  return [...points].reverse().find((point): point is LibraryPoint & { uniqueSize: number; totalSize: number } => point.uniqueSize !== null && point.totalSize !== null)
}
