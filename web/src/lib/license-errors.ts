/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Returns a settings namespace key for the license error.
export function getLicenseErrorKey(error: Error | null): string | null {
  if (!error) return null

  const errorMessage = error.message.toLowerCase()

  if (errorMessage.includes("expired")) {
    return "themes.license.errors.expired"
  } else if (errorMessage.includes("no longer active") || errorMessage.includes("not active")) {
    return "themes.license.errors.notActive"
  } else if (errorMessage.includes("not valid") || errorMessage.includes("invalid")) {
    return "themes.license.errors.invalid"
  } else if (errorMessage.includes("not found") || errorMessage.includes("404")) {
    return "themes.license.errors.notFound"
  } else if (errorMessage.includes("does not match required conditions")) {
    return "themes.license.errors.databaseCopied"
  } else if (errorMessage.includes("does not match")) {
    return "themes.license.errors.conditionsMismatch"
  } else if (errorMessage.includes("activation limit exceeded")) {
    return "themes.license.errors.activationLimit"
  } else if (errorMessage.includes("limit") && errorMessage.includes("reached")) {
    return "themes.license.errors.limitReached"
  } else if (errorMessage.includes("usage")) {
    return "themes.license.errors.usageLimit"
  } else if (errorMessage.includes("timeout") || errorMessage.includes("network") || errorMessage.includes("temporar")) {
    return "themes.license.errors.unreachable"
  } else if (errorMessage.includes("too many requests") || errorMessage.includes("429")) {
    return "themes.license.errors.tooManyAttempts"
  } else if (errorMessage.includes("rate limit")) {
    return "themes.license.errors.rateLimited"
  } else {
    return "themes.license.errors.generic"
  }
}
