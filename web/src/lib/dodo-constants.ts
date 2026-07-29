/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Production checkout URL (hardcoded canonical Dodo buy URL).
// Test checkout URL can be provided via Vite env for local/dev builds:
// `VITE_DODO_TEST_CHECKOUT_URL="https://test.checkout.dodopayments.com/buy/<product_id>?quantity=1"`
export const DODO_CHECKOUT_URL =
  import.meta.env.DEV && import.meta.env.VITE_DODO_TEST_CHECKOUT_URL? import.meta.env.VITE_DODO_TEST_CHECKOUT_URL: "https://checkout.dodopayments.com/buy/pdt_0NWpVDU3kVcVuB10ycNiQ?quantity=1"
export const DODO_PORTAL_URL = "https://customer.dodopayments.com"

// Supporter subscription. Deliberately keyless: it issues no license key and grants
// only the qui-patron Discord role. A keyed product here would grant premium themes,
// since license activation cannot tell Dodo products apart.
export const DODO_SUPPORTER_MONTHLY_URL = "https://checkout.dodopayments.com/buy/pdt_0NkFecY5CvwgHGP6TgDIZ?quantity=1"
export const DODO_SUPPORTER_MONTHLY_PRICE = "$5"
export const DODO_SUPPORTER_YEARLY_URL = "https://checkout.dodopayments.com/buy/pdt_0NkFeqcJL7YVFgj8p4vYg?quantity=1"
export const DODO_SUPPORTER_YEARLY_PRICE = "$50"
