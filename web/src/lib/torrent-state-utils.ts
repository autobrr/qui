/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TFunction } from "i18next"

export function getStateLabel(state: string, t: TFunction): string {
  if (!/^[a-zA-Z]+$/.test(state) || state in Object.prototype) return t("stateLabelFallback")

  return t([`stateLabels.${state}`, "stateLabelFallback"])
}

export function formatReannounceIn(state: string, seconds: number | undefined, t: TFunction): string {
  if (seconds === undefined || state === "stoppedDL" || state === "stoppedUP") return "—"

  if (seconds < 0) return "-"

  if (seconds === 0) return t("tableColumns.now")

  const minutes = Math.floor(seconds / 60)
  const hours = Math.floor(minutes / 60)

  if (minutes < 1) {
    return "< 1m"
  }

  if (hours < 1) {
    return `${minutes}m`
  }

  const remainingMinutes = minutes % 60
  if (remainingMinutes === 0) {
    return `${hours}h`
  }

  return `${hours}h ${remainingMinutes}m`
}
