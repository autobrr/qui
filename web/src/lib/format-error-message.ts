/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Kept out of utils.ts: this imports i18n, and utils.ts reaches nearly every component through cn().
import i18n from "@/i18n"

export function formatErrorMessage(error: string | undefined): string {
  if (!error) return i18n.t("errors.unknown", { ns: "common" })

  const normalized = error.trim()
  if (!normalized) return i18n.t("errors.unknown", { ns: "common" })

  const cleaned = normalized.replace(/^(failed to create client: |failed to connect to qBittorrent instance: |connection failed: |error: )/i, "")
  if (!cleaned) return i18n.t("errors.unknown", { ns: "common" })

  return cleaned.charAt(0).toUpperCase() + cleaned.slice(1)
}
