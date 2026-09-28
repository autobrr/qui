/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import { afterEach, describe, expect, it } from "vitest"

import i18n, { changeLanguage } from "@/i18n"
import deAutomations from "@/i18n/locales/de/automations.json"
import { CONTENT_TYPE_VALUES, getCapabilityReason } from "./constants"

afterEach(async () => {
  await changeLanguage("en")
})

// The drop-down's values are a copy of releases.ContentTypes, so read the Go
// source and fail when the two lists drift.
describe("CONTENT_TYPE_VALUES", () => {
  it("matches releases.ContentTypes in pkg/releases/content_type.go", () => {
    const source = readFileSync(resolve(import.meta.dirname, "../../../../pkg/releases/content_type.go"), "utf8")

    const constants = new Map(
      [...source.matchAll(/^\t(ContentType\w+)\s+ContentType\s*=\s*"([^"]+)"$/gm)].map(m => [m[1], m[2]])
    )
    const slice = source.match(/var ContentTypes = \[\]ContentType\{([^}]*)\}/)
    expect(slice).not.toBeNull()

    const goValues = [...slice![1].matchAll(/^\t(ContentType\w+),$/gm)].map(m => {
      const value = constants.get(m[1])
      expect(value, `${m[1]} has no string constant`).toBeDefined()
      return value
    })

    expect(goValues.length).toBeGreaterThan(0)
    expect(CONTENT_TYPE_VALUES.map(contentType => contentType.value)).toEqual(goValues)
  })
})

describe("getCapabilityReason", () => {
  it("renders the automations translation, not the English constant", async () => {
    await changeLanguage("de")

    expect(getCapabilityReason("trackerHealth", i18n.t)).toBe(deAutomations.queryBuilder.capabilityReasons.trackerHealth)
    expect(getCapabilityReason("localFilesystemAccess", i18n.t)).toBe(deAutomations.queryBuilder.capabilityReasons.localFilesystemAccess)
  })
})
