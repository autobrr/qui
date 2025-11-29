/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { POLAR_PORTAL_URL } from "./polar-constants"

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
  } else if (errorMessage.includes("does not match required conditions") || errorMessage.includes("condition mismatch")) {
    return `License fingerprint mismatch detected. This usually happens when the device ID file was deleted. Try clicking "Refresh" to auto-recover, or deactivate old devices at ${POLAR_PORTAL_URL}`
  } else if (errorMessage.includes("does not match")) {
    return "License key does not match required conditions."
  } else if (errorMessage.includes("re-activation failed") && errorMessage.includes("activation limit")) {
    return `License recovery failed: all activation slots are in use. Please deactivate an unused device at ${POLAR_PORTAL_URL} and try again.`
  } else if (errorMessage.includes("activation limit exceeded") || errorMessage.includes("activation limit")) {
    return `License activation limit has been reached. Please deactivate an unused device at ${POLAR_PORTAL_URL}`
  } else if (errorMessage.includes("limit") && errorMessage.includes("reached")) {
    return "License activation limit has been reached."
  } else if (errorMessage.includes("usage")) {
    return "License usage limit exceeded."
  } else if (errorMessage.includes("too many requests") || errorMessage.includes("429")) {
    return "Too many attempts. Please wait a moment and try again."
  } else if (errorMessage.includes("rate limit")) {
    return "Please wait before trying again."
  } else if (errorMessage.includes("offline") && errorMessage.includes("grace")) {
    return "License validation failed and offline grace period has expired. Please ensure you have internet connectivity."
  } else {
    return "Failed to validate license key. Please try again."
  }
}