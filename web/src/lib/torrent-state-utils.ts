/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TFunction } from "i18next"

export function getStateLabel(state: string, t: TFunction): string {
  // Backend input in a key path: a "." or ":" resolves a fragment instead of reporting it missing.
  // The fallback sits outside stateLabels because "unknown" is itself a qBittorrent state.
  if (!/^[a-zA-Z]+$/.test(state)) return t("stateLabelFallback")

  const key = `stateLabels.${state}`
  const label = t(key)

  return label === key ? t("stateLabelFallback") : label
}
