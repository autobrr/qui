/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import i18n from "@/i18n"

export function formatErrorMessage(error: string | undefined): string {
  if (!error) return i18n.t("errors.unknown", { ns: "common" })

  const normalized = error.trim()
  if (!normalized) return i18n.t("errors.unknown", { ns: "common" })

  const cleaned = normalized.replace(/^(failed to create client: |failed to connect to qBittorrent instance: |connection failed: |error: )/i, "")

  return cleaned.charAt(0).toUpperCase() + cleaned.slice(1)
}
