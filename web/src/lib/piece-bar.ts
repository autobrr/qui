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

/**
 * Splits the pieces over at most `width` columns, as the qBittorrent pieces bar does.
 * Each column covers piece count / column count pieces, and a piece that falls across
 * two columns counts in each by its overlap. Returns, for each column, the share of
 * finished and of downloading pieces, from 0 to 1. With fewer pieces than `width`, it
 * returns one column for each piece.
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

export interface PieceBarFill {
  finishedPixels: number
  downloadingPixels: number
}

/**
 * Returns the height in pixels of the finished part and of the downloading part of one
 * column, for a bar of `height` pixels (3 or more). The two parts leave 1 pixel or more
 * free when the column also holds missing pieces, and they fill the height when it holds
 * none. Each part that the column holds is 1 pixel or more.
 */
export function computePieceBarFill({ finished, downloading }: PieceBarColumn, height: number): PieceBarFill {
  const toPixels = (share: number) => (share > FLOAT_TOLERANCE ? Math.max(1, Math.round(share * height)) : 0)
  const emptyPixels = finished + downloading < 1 - FLOAT_TOLERANCE ? 1 : 0
  const finishedMinimum = finished > FLOAT_TOLERANCE ? 1 : 0

  const downloadingPixels = Math.min(toPixels(downloading), height - emptyPixels - finishedMinimum)
  const finishedPixels =
    emptyPixels === 0 ? height - downloadingPixels : Math.min(toPixels(finished), height - emptyPixels - downloadingPixels)

  return { finishedPixels, downloadingPixels }
}
