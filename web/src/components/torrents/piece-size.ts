/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

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

export const pieceSizeOptions = [
  { value: TorrentPieceSize.Auto, label: "Auto (recommended)" },
  { value: TorrentPieceSize.KiB16, label: "16 KiB" },
  { value: TorrentPieceSize.KiB32, label: "32 KiB" },
  { value: TorrentPieceSize.KiB64, label: "64 KiB" },
  { value: TorrentPieceSize.KiB128, label: "128 KiB" },
  { value: TorrentPieceSize.KiB256, label: "256 KiB" },
  { value: TorrentPieceSize.KiB512, label: "512 KiB" },
  { value: TorrentPieceSize.MiB1, label: "1 MiB" },
  { value: TorrentPieceSize.MiB2, label: "2 MiB" },
  { value: TorrentPieceSize.MiB4, label: "4 MiB" },
  { value: TorrentPieceSize.MiB8, label: "8 MiB" },
  { value: TorrentPieceSize.MiB16, label: "16 MiB" },
  { value: TorrentPieceSize.MiB32, label: "32 MiB" },
  { value: TorrentPieceSize.MiB64, label: "64 MiB" },
  { value: TorrentPieceSize.MiB128, label: "128 MiB" },
] as const

export type PieceSizeOption = (typeof pieceSizeOptions)[number]
