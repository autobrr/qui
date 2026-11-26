/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

const SIGN_IN_TOKENS = new Set(["username", "current-password", "new-password", "one-time-code"])

const IGNORE = {
  "data-1p-ignore": "true",
  "data-bwignore": "true",
  "data-lpignore": "true",
  "data-protonpass-ignore": "true",
  "data-form-type": "other",
} as const

export function passwordManagerAttributes(autoComplete?: string): Partial<typeof IGNORE> {
  const tokens = autoComplete?.toLowerCase().split(/\s+/) ?? []
  return tokens.some((token) => SIGN_IN_TOKENS.has(token)) ? {} : IGNORE
}
