/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { CrossSeedGazelleLookup, CrossSeedGazelleLookupReason } from "@/types"

// i18n-namespace: torrents

const statusKeys: Partial<Record<CrossSeedGazelleLookup["status"], string>> = {
  will_ask: "crossSeedDialog.gazelle.willAsk",
  match: "crossSeedDialog.gazelle.match",
  no_match: "crossSeedDialog.gazelle.noMatch",
}

const notAskedKeys: Partial<Record<CrossSeedGazelleLookupReason, string>> = {
  disabled: "crossSeedDialog.gazelle.notAsked.disabled",
  no_api_key: "crossSeedDialog.gazelle.notAsked.noApiKey",
  cooldown: "crossSeedDialog.gazelle.notAsked.cooldown",
  not_selected: "crossSeedDialog.gazelle.notAsked.notSelected",
  source_site: "crossSeedDialog.gazelle.notAsked.sourceSite",
  already_seeded: "crossSeedDialog.gazelle.notAsked.alreadySeeded",
  not_music: "crossSeedDialog.gazelle.notAsked.notMusic",
}

const failedKeys: Partial<Record<CrossSeedGazelleLookupReason, string>> = {
  key_rejected: "crossSeedDialog.gazelle.failed.keyRejected",
  timed_out: "crossSeedDialog.gazelle.failed.timedOut",
}

// The reasons that the user fixes in the RED/OPS card of the settings.
export const gazelleSettingsReasons = new Set<CrossSeedGazelleLookupReason | undefined>(["disabled", "no_api_key"])

/** gazelleLookupTextKey returns the key of the text that describes one RED/OPS entry. */
export function gazelleLookupTextKey(lookup: CrossSeedGazelleLookup): string {
  if (lookup.status === "not_asked") {
    return (lookup.reason && notAskedKeys[lookup.reason]) ?? "crossSeedDialog.gazelle.notAsked.other"
  }
  if (lookup.status === "failed") {
    return (lookup.reason && failedKeys[lookup.reason]) ?? "crossSeedDialog.gazelle.failed.other"
  }
  return statusKeys[lookup.status] ?? "crossSeedDialog.gazelle.willAsk"
}

/** gazelleReportLines is the RED/OPS section of the plain-text search report. */
export function gazelleReportLines(lookups: CrossSeedGazelleLookup[]): string[] {
  if (lookups.length === 0) {
    return []
  }
  return [
    "RED/OPS:",
    ...lookups.map(lookup => {
      const reason = lookup.reason ? ` (${lookup.reason})` : ""
      const detail = lookup.detail ? ` - ${lookup.detail}` : ""
      return `  ${lookup.name}: ${lookup.status}${reason}${detail}`
    }),
  ]
}
