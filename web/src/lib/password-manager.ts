/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

const SIGN_IN_TOKENS = new Set(["username", "current-password", "new-password", "one-time-code"])

// LastPass reads only the value "true". The others only look for the attribute.
const IGNORE = {
  "data-1p-ignore": "true",
  "data-bwignore": "true",
  "data-lpignore": "true",
  "data-protonpass-ignore": "true",
  "data-form-type": "other",
} as const

/**
 * The attributes that keep 1Password, Bitwarden, LastPass, Proton Pass, and Dashlane off a text field. A field whose
 * autoComplete holds a sign-in token (username, current-password, new-password, one-time-code) gets none, so that
 * the password managers fill the sign-in forms of qui. Spread the result before the props of the caller.
 */
export function passwordManagerAttributes(autoComplete?: string): Partial<typeof IGNORE> {
  const tokens = autoComplete?.toLowerCase().split(/\s+/) ?? []
  return tokens.some((token) => SIGN_IN_TOKENS.has(token)) ? {} : IGNORE
}
