/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE: a stand-in for the transfer route of #3997, which does not exist yet.
// Every number is made up from the speed that an instance had at its first sight.
// The real Dashboard reads one transfer query for each instance (24 hours, bucket
// hour, grouped by Tracker group) and adds the instances together. That one answer
// feeds the chart and the tracker list.

import type { History, Row, TrackerRow } from "./shared"

const HOUR = 3_600_000
const HOURS = 24
const SERIES = 5

export interface DayHistory {
  /** The start of each hour, oldest first. */
  hours: number[]
  /** The bytes of each hour, for each instance. */
  byInstance: Map<number, History>
  uploaded: number
  downloaded: number
  crossSeedUploaded: number
  /** The hour that carries the bytes of a Recording gap, and the length of the gap in hours. */
  gap: { index: number; hours: number } | null
  /** The largest Tracker groups. They and `otherUploaded` add up to `uploaded`. */
  trackers: Array<{ domain: string; name: string; uploaded: number }>
  otherUploaded: number
  otherCount: number
}

/** A stable number from 0 to 1 for a pair of numbers, so that the bars keep still between renders. */
const noise = (a: number, b: number) => {
  const x = Math.sin(a * 127.1 + b * 311.7) * 43758.5453
  return x - Math.floor(x)
}

// The live speeds drift, so each instance keeps the speeds of its first sight.
const firstSpeeds = new Map<string, { ul: number; dl: number }>()

export const sumOf = (values: number[] | undefined) => (values ?? []).reduce((a, b) => a + b, 0)

export function dayHistory(statsData: Row[], trackerRows: TrackerRow[], fixture: boolean): DayHistory {
  const end = Math.floor(Date.now() / HOUR)
  const hours = Array.from({ length: HOURS }, (_, i) => (end - HOURS + i) * HOUR)
  const gap = fixture ? { index: 9, hours: 1 } : null
  const byInstance = new Map<number, History>()

  for (const d of statsData) {
    const id = d.instance.id
    const key = `${fixture}:${id}`
    if (!firstSpeeds.has(key) && d.stats?.totalUploadSpeed) {
      firstSpeeds.set(key, { ul: d.stats.totalUploadSpeed, dl: d.stats.totalDownloadSpeed || 0 })
    }
    const first = firstSpeeds.get(key) ?? { ul: 0, dl: 0 }
    const ul: number[] = []
    const dl: number[] = []
    hours.forEach((start, i) => {
      const hour = start / HOUR
      // Qui did not run in the hours of the gap, so their bytes land on the hour after it.
      const weight = !gap || i > gap.index || i < gap.index - gap.hours ? 1 : i === gap.index ? gap.hours + 1 : 0
      const evening = 0.75 + 0.4 * Math.sin(((new Date(start).getHours() - 14) / 24) * 2 * Math.PI)
      ul.push(first.ul * 3600 * evening * (0.6 + 0.8 * noise(id, hour)) * weight)
      // A download runs in a few hours of the day only, also on a client that downloads nothing now.
      const busy = noise(id + 3, hour) > 0.75 ? 0.4 : 0
      const burst = noise(id + 7, hour) > 0.8 ? 1.2 * noise(id + 5, hour) : 0
      dl.push((first.dl * busy + first.ul * burst) * 3600 * weight)
    })
    byInstance.set(id, { ul, dl })
  }

  const total = (key: keyof History) => [...byInstance.values()].reduce((sum, h) => sum + sumOf(h[key]), 0)
  const uploaded = total("ul")

  // The share of a tracker in the day follows its share of the lifetime upload.
  const weights = trackerRows.map((t, i) => t.uploaded * (0.5 + noise(i, Math.floor(end / 24))))
  const weightSum = sumOf(weights) || 1
  const groups = trackerRows
    .map((t, i) => ({ domain: t.domain, name: t.name, uploaded: (weights[i] / weightSum) * uploaded }))
    .sort((a, b) => b.uploaded - a.uploaded)

  return {
    hours, byInstance, uploaded, gap,
    downloaded: total("dl"),
    crossSeedUploaded: uploaded * 0.38,
    trackers: groups.slice(0, SERIES),
    otherUploaded: sumOf(groups.slice(SERIES).map(t => t.uploaded)),
    otherCount: Math.max(0, groups.length - SERIES),
  }
}
