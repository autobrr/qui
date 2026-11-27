/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export function parseTypedTags(text: string): string[] {
  return [...new Set(text.split(",").map(part => part.trim()).filter(Boolean))]
}
