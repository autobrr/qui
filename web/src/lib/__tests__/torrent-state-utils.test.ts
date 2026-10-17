/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import i18n, { changeLanguage } from "@/i18n"
import { getStateLabel } from "@/lib/torrent-state-utils"
import { afterEach, describe, expect, it } from "vitest"

const t = i18n.getFixedT(null, "torrents")

afterEach(async () => {
  await changeLanguage("en")
})

describe("getStateLabel", () => {
  it("renders a known state from the locale file", () => {
    expect(getStateLabel("uploading", t)).toBe("Seeding")
    expect(getStateLabel("stoppedDL", t)).toBe("Stopped")
    expect(getStateLabel("forcedDL", t)).toBe("(F) Downloading")
    expect(getStateLabel("forcedMetaDL", t)).toBe("(F) Fetching Metadata")
  })

  it("disguises forcedMetaDL like metaDL under the spreadsheet theme", () => {
    document.documentElement.setAttribute("data-theme", "spreadsheet")
    try {
      expect(getStateLabel("metaDL", t)).toBe("Fetching Info")
      expect(getStateLabel("forcedMetaDL", t)).toBe("(F) Fetching Info")
    } finally {
      document.documentElement.removeAttribute("data-theme")
    }
  })

  it("treats qBittorrent's own unknown state as a label", () => {
    expect(getStateLabel("unknown", t)).toBe("Unknown")
  })

  // Listed, not derived from the JSON: a derived list loses a state the moment the file does.
  const QBITTORRENT_STATES = [
    "downloading", "metaDL", "forcedMetaDL", "allocating", "stalledDL", "queuedDL", "checkingDL", "forcedDL",
    "uploading", "stalledUP", "queuedUP", "checkingUP", "forcedUP",
    "pausedDL", "pausedUP", "stoppedDL", "stoppedUP",
    "error", "missingFiles", "checkingResumeData", "moving", "unknown",
  ]

  it("resolves every state qBittorrent documents", () => {
    const unresolved = QBITTORRENT_STATES.filter((state) => {
      const label = getStateLabel(state, t)
      return label === `stateLabels.${state}` || label === "" || label === "Unrecognized"
    })

    expect(unresolved).toEqual([])
  })

  it.each(["common:nav.dashboard", "tableColumns.unregistered", "a.b", ""])(
    "does not let %j reach the key path", (state) => {
      expect(getStateLabel(state, t)).toBe("Unrecognized")
    }
  )

  // Letters only, so these pass the separator check; i18next would resolve them to inherited functions.
  it.each(["constructor", "toString", "valueOf", "hasOwnProperty"])(
    "does not let the inherited name %j resolve to a function", (state) => {
      expect(getStateLabel(state, t)).toBe("Unrecognized")
    }
  )

  it("names an undocumented state through the fallback, without the raw value", () => {
    const label = getStateLabel("someFutureState", t)

    expect(label).toBe("Unrecognized")
    expect(label).not.toContain("someFutureState")
  })

  it("translates the fallback, not just the known states", async () => {
    await changeLanguage("de")

    expect(getStateLabel("someFutureState", t)).toBe("Nicht erkannt")
  })
})
