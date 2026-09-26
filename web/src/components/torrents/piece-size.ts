/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { formatBytes } from "@/lib/utils"

export const TorrentPieceSize = {
  Auto: "0",
  KiB16: "16384",
  KiB32: "32768",
  KiB64: "65536",
  KiB128: "131072",
  KiB256: "262144",
  KiB512: "524288",
  MiB1: "1048576",
  MiB2: "2097152",
  MiB4: "4194304",
  MiB8: "8388608",
  MiB16: "16777216",
  MiB32: "33554432",
  MiB64: "67108864",
  MiB128: "134217728",
} as const

export type TorrentPieceSizeValue = (typeof TorrentPieceSize)[keyof typeof TorrentPieceSize]

// Each value is already the piece size in bytes, so the label is derived rather than repeated.
// Built on call, not frozen at import: the unit is localized and a module-level array would
// resolve it before i18next loads a language.
export const getPieceSizeOptions = (): { value: TorrentPieceSizeValue; label: string }[] =>
  Object.values(TorrentPieceSize).map((value) => ({
    value,
    label: value === TorrentPieceSize.Auto ? "Auto (recommended)" : formatBytes(Number(value)),
  }))

export type PieceSizeOption = ReturnType<typeof getPieceSizeOptions>[number]
