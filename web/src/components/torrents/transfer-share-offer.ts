/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { isDemo } from "@/lib/demo"
import type { Torrent } from "@/types"

// The most hashes that POST /api/shares takes (internal/api/handlers/shares.go).
const maxShareTorrents = 100

/**
 * Which of "Transfer to instance…" and "Share with someone…" the torrent menu
 * offers. Both need each torrent of the selection by name, on one instance, and
 * no helper torrent of a Transfer or a Share. A Transfer moves one torrent, and
 * one Share request takes at most 100 torrents. The demo offers neither,
 * because it serves no Transfer or Share request.
 */
export function transferShareOffer(torrents: Torrent[], count: number, instanceIds: number[] | undefined): { transfer: boolean; share: boolean } {
  const share = !isDemo && count > 0 && count <= maxShareTorrents && torrents.length === count && instanceIds?.length === 1 && !torrents.some((torrent) => torrent.qui_helper)
  return { transfer: share && count === 1, share }
}
