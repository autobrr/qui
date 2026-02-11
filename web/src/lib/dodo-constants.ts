/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Production checkout URL (hardcoded canonical Dodo buy URL).
// Test checkout URL can be provided via Vite env for local/dev builds:
// `VITE_DODO_TEST_CHECKOUT_URL="https://test.checkout.dodopayments.com/buy/<product_id>?quantity=1"`
export const DODO_CHECKOUT_URL =
  import.meta.env.DEV && import.meta.env.VITE_DODO_TEST_CHECKOUT_URL
    ? import.meta.env.VITE_DODO_TEST_CHECKOUT_URL
    : "https://checkout.dodopayments.com/buy/pdt_0NWpVDU3kVcVuB10ycNiQ?quantity=1"
export const DODO_PORTAL_URL = "https://customer.dodopayments.com"
