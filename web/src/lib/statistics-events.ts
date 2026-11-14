/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { localDateOfUtc, utcDayStart } from "@/lib/statistics"
import type { TransferHistoryLibraryRow } from "@/types"

const DAY = 24 * 3_600_000

export const EVENTS = ["added", "completed", "deleted"] as const

export type EventKind = (typeof EVENTS)[number]

/** The events of one UTC day. start is the local midnight of the date of that day, so that its label shows the stored date. */
export type EventDay = { start: number } & Record<EventKind, number>

/**
 * Adds the library rows of the instances into one day for each UTC day that the period overlaps, because qui counts the
 * events for each UTC day. A period with no start begins at the first recording.
 */
export function buildEventDays(rowsByInstance: TransferHistoryLibraryRow[][], period: { from?: Date; to: Date }, since: Date | undefined): EventDay[] | undefined {
  const start = period.from ?? since
  if (!start) {
    return undefined
  }
  const days = new Map<number, EventDay>()
  const last = utcDayStart(new Date(period.to.getTime() - 1))
  for (let day = utcDayStart(start); day <= last; day += DAY) {
    days.set(day, { start: localDateOfUtc(day), added: 0, completed: 0, deleted: 0 })
  }
  for (const rows of rowsByInstance) {
    for (const row of rows) {
      const day = days.get(utcDayStart(new Date(row.day)))
      if (day) {
        day.added += row.added
        day.completed += row.completed
        day.deleted += row.deleted
      }
    }
  }
  return [...days.values()]
}
