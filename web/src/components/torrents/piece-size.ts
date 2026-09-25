/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { formatValueWithUnit } from "@/lib/unit-format"

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

// Built on call, not frozen at import: the unit in each label is localized, and a
// module-level array would resolve it before i18next loads a language.
export const getPieceSizeOptions = (): { value: TorrentPieceSizeValue; label: string }[] => [
  { value: TorrentPieceSize.Auto, label: "Auto (recommended)" },
  { value: TorrentPieceSize.KiB16, label: formatValueWithUnit(16, "KiB", { fractionDigits: 0 }) },
  { value: TorrentPieceSize.KiB32, label: formatValueWithUnit(32, "KiB", { fractionDigits: 0 }) },
  { value: TorrentPieceSize.KiB64, label: formatValueWithUnit(64, "KiB", { fractionDigits: 0 }) },
  { value: TorrentPieceSize.KiB128, label: formatValueWithUnit(128, "KiB", { fractionDigits: 0 }) },
  { value: TorrentPieceSize.KiB256, label: formatValueWithUnit(256, "KiB", { fractionDigits: 0 }) },
  { value: TorrentPieceSize.KiB512, label: formatValueWithUnit(512, "KiB", { fractionDigits: 0 }) },
  { value: TorrentPieceSize.MiB1, label: formatValueWithUnit(1, "MiB", { fractionDigits: 0 }) },
  { value: TorrentPieceSize.MiB2, label: formatValueWithUnit(2, "MiB", { fractionDigits: 0 }) },
  { value: TorrentPieceSize.MiB4, label: formatValueWithUnit(4, "MiB", { fractionDigits: 0 }) },
  { value: TorrentPieceSize.MiB8, label: formatValueWithUnit(8, "MiB", { fractionDigits: 0 }) },
  { value: TorrentPieceSize.MiB16, label: formatValueWithUnit(16, "MiB", { fractionDigits: 0 }) },
  { value: TorrentPieceSize.MiB32, label: formatValueWithUnit(32, "MiB", { fractionDigits: 0 }) },
  { value: TorrentPieceSize.MiB64, label: formatValueWithUnit(64, "MiB", { fractionDigits: 0 }) },
  { value: TorrentPieceSize.MiB128, label: formatValueWithUnit(128, "MiB", { fractionDigits: 0 }) },
]

export type PieceSizeOption = ReturnType<typeof getPieceSizeOptions>[number]
