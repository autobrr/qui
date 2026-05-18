/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Torrent } from "@/types"

export const LIMIT_USE_GLOBAL = -2
export const LIMIT_UNLIMITED = -1

export type TorrentLimitSnapshot = Pick<
  Torrent,
  | "ratio_limit"
  | "seeding_time_limit"
  | "inactive_seeding_time_limit"
  | "max_ratio"
  | "max_seeding_time"
  | "max_inactive_seeding_time"
  | "dl_limit"
  | "up_limit"
  | "share_limit_action"
  | "share_limits_mode"
>

export function checkFieldConsistency(
  torrents: TorrentLimitSnapshot[] | undefined,
  getter: (t: TorrentLimitSnapshot) => number | undefined
): { isMixed: boolean; commonValue: number | undefined } {
  if (!torrents || torrents.length === 0) {
    return { isMixed: false, commonValue: undefined }
  }
  const firstValue = getter(torrents[0])
  const allSame = torrents.every(t => getter(t) === firstValue)
  return { isMixed: !allSame, commonValue: allSame ? firstValue : undefined }
}

/** Qt share-limit enum; when torrents disagree, value is "default" placeholder and isMixed is true. */
export function shareLimitEnumFieldFromTorrents(
  torrents: TorrentLimitSnapshot[] | undefined,
  pick: (t: TorrentLimitSnapshot) => string | undefined
): { value: string; isMixed: boolean } {
  if (!torrents?.length) return { value: "default", isMixed: false }
  const vals = torrents.map((t) => {
    const v = (pick(t) ?? "").trim()
    return v === "" || v === "Default" ? "default" : v
  })
  const first = vals[0]!
  const isMixed = !vals.every((x) => x === first)
  return { value: isMixed ? "default" : first, isMixed }
}
