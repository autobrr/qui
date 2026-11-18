/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { withBasePath } from "@/lib/base-url"
import { DODO_CHECKOUT_URL } from "@/lib/dodo-constants"

/** The checkout of the theme license. After the payment, it returns to Settings, under Themes. */
export function themeLicenseCheckoutUrl(): string {
  const returnPath = withBasePath("settings?tab=themes&checkout=success")
  const returnUrl = new URL(returnPath, window.location.origin).toString()
  try {
    const checkoutUrl = new URL(DODO_CHECKOUT_URL)
    checkoutUrl.searchParams.set("redirect_url", returnUrl)
    return checkoutUrl.toString()
  } catch {
    const separator = DODO_CHECKOUT_URL.includes("?") ? "&" : "?"
    return `${DODO_CHECKOUT_URL}${separator}redirect_url=${encodeURIComponent(returnUrl)}`
  }
}
