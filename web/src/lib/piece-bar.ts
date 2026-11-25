/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

const PIECE_DOWNLOADING = 1
const PIECE_FINISHED = 2

export interface PieceBarColumn {
  finished: number
  downloading: number
}

const FLOAT_TOLERANCE = 1e-9

function toShare(amount: number, ratio: number): number {
  return Math.min(amount / ratio, 1)
}

export function computePieceBarColumns(pieceStates: readonly number[], width: number): PieceBarColumn[] {
  const columnCount = Math.min(Math.floor(width), pieceStates.length)
  if (columnCount <= 0) return []

  const ratio = pieceStates.length / columnCount
  const columns: PieceBarColumn[] = []

  for (let x = 0; x < columnCount; x++) {
    const from = x * ratio
    const to = (x + 1) * ratio
    let finished = 0
    let downloading = 0

    for (let piece = Math.floor(from); piece < Math.ceil(to); piece++) {
      const overlap = Math.min(piece + 1, to) - Math.max(piece, from)
      const state = pieceStates[piece]
      if (state === PIECE_FINISHED) finished += overlap
      else if (state === PIECE_DOWNLOADING) downloading += overlap
    }

    columns.push({ finished: toShare(finished, ratio), downloading: toShare(downloading, ratio) })
  }

  return columns
}

export interface PieceBarFill {
  finishedPixels: number
  downloadingPixels: number
}

export function computePieceBarFill({ finished, downloading }: PieceBarColumn, height: number): PieceBarFill {
  const toPixels = (share: number) => (share > FLOAT_TOLERANCE ? Math.max(1, Math.round(share * height)) : 0)
  const emptyPixels = finished + downloading < 1 - FLOAT_TOLERANCE ? 1 : 0
  const finishedMinimum = finished > FLOAT_TOLERANCE ? 1 : 0

  const downloadingPixels = Math.min(toPixels(downloading), height - emptyPixels - finishedMinimum)
  const finishedPixels =
    emptyPixels === 0 ? height - downloadingPixels : Math.min(toPixels(finished), height - emptyPixels - downloadingPixels)

  return { finishedPixels, downloadingPixels }
}
