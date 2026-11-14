/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TransferHistoryLibraryRow } from "@/types"

const DAY = 24 * 3_600_000

export const EVENTS = ["added", "completed", "deleted"] as const

export type EventKind = (typeof EVENTS)[number]

/** The events of one UTC day. start is the local midnight of the date of that day, so that its label shows the stored date. */
export type EventDay = { start: number } & Record<EventKind, number>

function utcDay(time: number): number {
  const date = new Date(time)
  return Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate())
}

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
  const last = utcDay(period.to.getTime() - 1)
  for (let day = utcDay(start.getTime()); day <= last; day += DAY) {
    const date = new Date(day)
    days.set(day, { start: new Date(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()).getTime(), added: 0, completed: 0, deleted: 0 })
  }
  for (const rows of rowsByInstance) {
    for (const row of rows) {
      const day = days.get(utcDay(Date.parse(row.day)))
      if (day) {
        day.added += row.added
        day.completed += row.completed
        day.deleted += row.deleted
      }
    }
  }
  return [...days.values()]
}
