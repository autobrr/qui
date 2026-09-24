/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it } from "vitest"

import i18n, { changeLanguage } from "@/i18n"
import deAutomations from "@/i18n/locales/de/automations.json"
import type { TFunction } from "i18next"
import * as constants from "./constants"

afterEach(async () => {
  await changeLanguage("en")
})

describe("getCapabilityReason", () => {
  it("renders the automations translation, not the English constant", async () => {
    await changeLanguage("de")

    expect(constants.getCapabilityReason("trackerHealth", i18n.t)).toBe(deAutomations.queryBuilder.capabilityReasons.trackerHealth)
    expect(constants.getCapabilityReason("localFilesystemAccess", i18n.t)).toBe(deAutomations.queryBuilder.capabilityReasons.localFilesystemAccess)
  })
})

// The English labels in constants.ts are only defaultValues, so a missing key renders
// English in every locale without failing any other check.
describe("query-builder translation keys", () => {
  it("every label the helpers look up has an English automations key", () => {
    const requested = new Set<string>()
    const recordingT = ((key: string, options?: { defaultValue?: string }) => {
      requested.add(key)
      return options?.defaultValue ?? key
    }) as TFunction

    const fields = Object.keys(constants.CONDITION_FIELDS)
    for (const field of fields) {
      constants.getFieldLabel(field, recordingT)
    }
    for (const group of constants.FIELD_GROUPS) {
      constants.getFieldGroupLabel(group.label, recordingT)
    }
    for (const capability of Object.keys(constants.CAPABILITY_REASONS) as (keyof typeof constants.CAPABILITY_REASONS)[]) {
      constants.getCapabilityReason(capability, recordingT)
    }

    const translatedHelpers = Object.entries(constants).flatMap(([name, value]) =>
      name.startsWith("getTranslated") && typeof value === "function" ? [[name, value] as const] : []
    )
    // Fails when a module change hides the helpers, instead of passing with nothing checked.
    expect(translatedHelpers.length).toBeGreaterThanOrEqual(5)
    for (const [name, helper] of translatedHelpers) {
      // A future per-field helper throws here rather than being skipped quietly.
      if (name.endsWith("ForField")) {
        for (const field of fields) {
          (helper as (field: string, t: TFunction) => unknown)(field, recordingT)
        }
      } else {
        (helper as (t: TFunction) => unknown)(recordingT)
      }
    }

    const missing = [...requested].filter((key) => !i18n.exists(key, { ns: "automations", lng: "en" }))
    expect(missing).toEqual([])
    expect(requested.size).toBeGreaterThan(fields.length)
  })
})
