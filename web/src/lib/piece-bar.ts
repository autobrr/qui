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

function toShare(amount: number, ratio: number): number {
  return Math.min(Math.round((amount / ratio) * 100) / 100, 1)
}

/**
 * Splits the pieces over at most `width` columns, as the qBittorrent pieces bar does.
 * Each column covers piece count / column count pieces, and a piece that falls across
 * two columns counts in each by its overlap. Returns, for each column, the share of
 * finished and of downloading pieces, from 0 to 1 in steps of 0.01. With fewer pieces
 * than `width`, it returns one column for each piece.
 */
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
