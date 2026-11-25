/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export const RELEASE_MAP_DAYS = 364

const DAY_SECONDS = 86_400
const RELEASE_TAG = /^v\d+\.\d+\.\d+$/

export function releaseDays(tags: string, now: number): number[] {
  const today = Math.floor(now / DAY_SECONDS)
  return tags
    .split("\n")
    .map((line) => line.trim().split(" "))
    .filter(([name]) => RELEASE_TAG.test(name))
    .map(([, created]) => RELEASE_MAP_DAYS - 1 - (today - Math.floor(Number(created) / DAY_SECONDS)))
    .filter((day) => day >= 0 && day < RELEASE_MAP_DAYS)
    .sort((a, b) => a - b)
}
