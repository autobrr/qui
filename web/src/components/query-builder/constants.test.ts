/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it } from "vitest"

import i18n, { changeLanguage } from "@/i18n"
import deAutomations from "@/i18n/locales/de/automations.json"
import { getCapabilityReason } from "./constants"

afterEach(async () => {
  await changeLanguage("en")
})

describe("getCapabilityReason", () => {
  it("renders the automations translation, not the English constant", async () => {
    await changeLanguage("de")

    expect(getCapabilityReason("trackerHealth", i18n.t)).toBe(deAutomations.queryBuilder.capabilityReasons.trackerHealth)
    expect(getCapabilityReason("localFilesystemAccess", i18n.t)).toBe(deAutomations.queryBuilder.capabilityReasons.localFilesystemAccess)
  })
})
