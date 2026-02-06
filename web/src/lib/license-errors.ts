/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export function getLicenseErrorMessage(error: Error | null): string {
  if (!error) return ""

  const errorMessage = error.message.toLowerCase()

  if (errorMessage.includes("expired")) {
    return "Your license key has expired."
  } else if (errorMessage.includes("no longer active") || errorMessage.includes("not active")) {
    return "This license key is no longer active."
  } else if (errorMessage.includes("not valid") || errorMessage.includes("invalid")) {
    return "This license key is invalid."
  } else if (errorMessage.includes("not found") || errorMessage.includes("404")) {
    return "The license key you entered is not valid."
  } else if (errorMessage.includes("does not match required conditions")) {
    return "This license was activated on a different machine. The database appears to have been copied."
  } else if (errorMessage.includes("does not match")) {
    return "License key does not match required conditions."
  } else if (errorMessage.includes("activation limit exceeded")) {
    return "License activation limit has been reached. Deactivate it from the other machine in qui."
  } else if (errorMessage.includes("limit") && errorMessage.includes("reached")) {
    return "License activation limit has been reached."
  } else if (errorMessage.includes("usage")) {
    return "License usage limit exceeded."
  } else if (errorMessage.includes("timeout") || errorMessage.includes("network") || errorMessage.includes("temporar")) {
    return "Unable to reach the license service. Please try again shortly."
  } else if (errorMessage.includes("too many requests") || errorMessage.includes("429")) {
    return "Too many attempts. Please wait a moment and try again."
  } else if (errorMessage.includes("rate limit")) {
    return "Please wait before trying again."
  } else {
    return "Failed to validate license key. Please try again."
  }
}
