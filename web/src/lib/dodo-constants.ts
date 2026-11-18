/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Production checkout URL (hardcoded canonical Dodo buy URL).
// Test checkout URL can be provided via Vite env for local/dev builds:
// `VITE_DODO_TEST_CHECKOUT_URL="https://test.checkout.dodopayments.com/buy/<product_id>?quantity=1"`
export const DODO_CHECKOUT_URL =
  import.meta.env.DEV && import.meta.env.VITE_DODO_TEST_CHECKOUT_URL? import.meta.env.VITE_DODO_TEST_CHECKOUT_URL: "https://checkout.dodopayments.com/buy/pdt_0NWpVDU3kVcVuB10ycNiQ?quantity=1"
export const LICENSE_PORTAL_URL = "https://licenses.getqui.com"

// The yearly support. Each product issues a supporter key. internal/services/license lists the same product ids.
export const DODO_SUPPORTER_PLANS = [
  { id: "24", price: "$24", monthlyPrice: "$2", url: "https://checkout.dodopayments.com/buy/pdt_0NsEXpOuhHMsJwbdagrM9?quantity=1" },
  { id: "48", price: "$48", monthlyPrice: "$4", url: "https://checkout.dodopayments.com/buy/pdt_0NsEXpSFgcjPNVB6CI98V?quantity=1" },
] as const
/** The product name of a supporter key in the license list. A theme license has another name. */
export const SUPPORTER_PRODUCT_NAME = "supporter"
export const DODO_CUSTOMER_PORTAL_URL = "https://customer.dodopayments.com/login/bus_0NWnkOye8uJm3odwDQQIO"
export const THEME_LICENSE_MIN_PRICE = "$9.99"
