/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { localDateOfUtc, utcDayStart, type StatisticsPeriod } from "@/lib/statistics"
import type { TransferHistoryLibraryRow } from "@/types"

const DAY = 86_400_000

export interface LibraryPoint {
  start: number
  torrents: number | null
  totalSize: number | null
  uniqueSize: number | null
}

export interface LibraryInstance {
  firstRecordedAt: string
  recording: boolean
}

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

export function newestUniqueSize(points: LibraryPoint[]): (LibraryPoint & { uniqueSize: number; totalSize: number }) | undefined {
  return [...points].reverse().find((point): point is LibraryPoint & { uniqueSize: number; totalSize: number } => point.uniqueSize !== null && point.totalSize !== null)
}
