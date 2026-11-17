/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/**
 * Splits typed tag text into tag names the way qBittorrent reads it: commas separate tags.
 * Names keep their case, because qBittorrent treats "Keep" and "keep" as two tags.
 * The result holds each name once, in typed order.
 */
export function parseTypedTags(text: string): string[] {
  return [...new Set(text.split(",").map(part => part.trim()).filter(Boolean))]
}
