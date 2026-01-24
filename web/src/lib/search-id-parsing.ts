/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

const IMDB_REGEX = /tt(\d{7,})/i
const TVDB_REGEX = /(?:the)?tvdb(?:id)?\s*[:#=]?\s*(\d{5,})/i

export function extractImdbId(rawQuery: string): string | null {
  if (!rawQuery) {
    return null
  }

  const match = rawQuery.match(IMDB_REGEX)
  if (!match) {
    return null
  }

  return `tt${match[1]}`
}

export function extractTvdbId(rawQuery: string): string | null {
  if (!rawQuery) {
    return null
  }

  const match = rawQuery.match(TVDB_REGEX)
  if (!match) {
    return null
  }

  return match[1]
}
