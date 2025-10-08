/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import i18n from "@/i18n"
import { POLAR_PORTAL_URL } from "./polar-constants"

export function getLicenseErrorMessage(error: Error | null): string {
  if (!error) return ""

  const errorMessage = error.message.toLowerCase()

  if (errorMessage.includes("expired")) {
    return i18n.t("lib.licenseErrors.expired")
  } else if (errorMessage.includes("no longer active") || errorMessage.includes("not active")) {
    return i18n.t("lib.licenseErrors.noLongerActive")
  } else if (errorMessage.includes("not valid") || errorMessage.includes("invalid")) {
    return i18n.t("lib.licenseErrors.invalid")
  } else if (errorMessage.includes("not found") || errorMessage.includes("404")) {
    return i18n.t("lib.licenseErrors.notFound")
  } else if (errorMessage.includes("does not match required conditions")) {
    return i18n.t("lib.licenseErrors.conditionsNotMet")
  } else if (errorMessage.includes("does not match")) {
    return i18n.t("lib.licenseErrors.doesNotMatch")
  } else if (errorMessage.includes("activation limit exceeded")) {
    return i18n.t("lib.licenseErrors.activationLimitExceeded", { polarPortalUrl: POLAR_PORTAL_URL })
  } else if (errorMessage.includes("limit") && errorMessage.includes("reached")) {
    return i18n.t("lib.licenseErrors.limitReached")
  } else if (errorMessage.includes("usage")) {
    return i18n.t("lib.licenseErrors.usageLimitExceeded")
  } else if (errorMessage.includes("too many requests") || errorMessage.includes("429")) {
    return i18n.t("lib.licenseErrors.tooManyRequests")
  } else if (errorMessage.includes("rate limit")) {
    return i18n.t("lib.licenseErrors.rateLimit")
  } else {
    return i18n.t("lib.licenseErrors.default")
  }
}