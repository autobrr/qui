/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TFunction } from "i18next"

export function getStateLabel(state: string, t: TFunction): string {
  // Backend input in a key path: "." or ":" resolves a fragment and an inherited name resolves a function.
  if (!/^[a-zA-Z]+$/.test(state) || state in Object.prototype) return t("stateLabelFallback")

  // One t() call: i18next returns the first key it finds, so a miss costs no second pass through the post-processors.
  // The fallback sits outside stateLabels because "unknown" is itself a qBittorrent state.
  return t([`stateLabels.${state}`, "stateLabelFallback"])
}
