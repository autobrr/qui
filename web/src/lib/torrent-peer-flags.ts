/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TorrentPeer } from "@/types"

interface PeerFlagDetail {
  flag: string
  description?: string
}

function parseFlagDescriptionsMap(flagsDescription?: string): Record<string, string> {
  if (!flagsDescription) return {}

  return flagsDescription.split("\n").reduce<Record<string, string>>((acc, line) => {
    const trimmedLine = line.trim()
    if (!trimmedLine) return acc

    const equalsIndex = trimmedLine.indexOf("=")
    if (equalsIndex === -1) return acc

    const flag = trimmedLine.slice(0, equalsIndex).trim()
    const description = trimmedLine.slice(equalsIndex + 1).trim()

    if (!flag) return acc

    // Use the first character as the flag symbol (matches qBittorrent output)
    const symbol = flag[0]
    if (!symbol) return acc

    acc[symbol] = description
    return acc
  }, {})
}

export function getPeerFlagDetails(flags?: TorrentPeer["flags"], flagsDescription?: TorrentPeer["flags_desc"]): PeerFlagDetail[] {
  if (!flags) return []

  const parsedDescriptions = parseFlagDescriptionsMap(flagsDescription)

  const normalizedFlags = flags.replace(/\s+/g, "")

  return Array.from(normalizedFlags).map((flag) => ({
    flag,
    description: parsedDescriptions[flag],
  }))
}
