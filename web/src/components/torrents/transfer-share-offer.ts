/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { isDemo } from "@/lib/demo"
import type { Torrent } from "@/types"

const maxShareTorrents = 100

export function transferShareOffer(torrents: Torrent[], count: number, instanceIds: number[] | undefined): { transfer: boolean; share: boolean } {
  const share = !isDemo && count > 0 && count <= maxShareTorrents && torrents.length === count && instanceIds?.length === 1 && !torrents.some((torrent) => torrent.qui_helper)
  return { transfer: share && count === 1, share }
}
