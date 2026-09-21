/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it } from "vitest"

import i18n, { changeLanguage } from "@/i18n"
import deAutomations from "@/i18n/locales/de/automations.json"
import type { TFunction } from "i18next"
import * as constants from "./constants"
import {
  CAPABILITY_REASONS,
  CONDITION_FIELDS,
  FIELD_GROUPS,
  getCapabilityReason,
  getFieldGroupLabel,
  getFieldLabel
} from "./constants"

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

// The English labels in constants.ts are only defaultValues, so a missing key renders
// English in every locale without failing any other check.
describe("query-builder translation keys", () => {
  it("every label the helpers look up has an English automations key", () => {
    const requested = new Set<string>()
    const recordingT = ((key: string, options?: { defaultValue?: string }) => {
      requested.add(key)
      return options?.defaultValue ?? key
    }) as TFunction

    for (const field of Object.keys(CONDITION_FIELDS)) {
      getFieldLabel(field, recordingT)
    }
    for (const group of FIELD_GROUPS) {
      getFieldGroupLabel(group.label, recordingT)
    }
    for (const capability of Object.keys(CAPABILITY_REASONS) as (keyof typeof CAPABILITY_REASONS)[]) {
      getCapabilityReason(capability, recordingT)
    }
    const translatedHelpers = Object.entries(constants).flatMap(([name, value]) =>
      name.startsWith("getTranslated") && typeof value === "function" ? [value as (...args: never[]) => unknown] : []
    )
    // Fails when a module change hides the helpers, instead of passing with nothing checked.
    expect(translatedHelpers.length).toBeGreaterThanOrEqual(5)
    for (const helper of translatedHelpers) {
      if (helper.length === 2) {
        for (const field of Object.keys(CONDITION_FIELDS)) {
          (helper as (field: string, t: TFunction) => unknown)(field, recordingT)
        }
      } else {
        (helper as (t: TFunction) => unknown)(recordingT)
      }
    }

    const missing = [...requested].filter((key) => !i18n.exists(key, { ns: "automations", lng: "en" }))
    expect(missing).toEqual([])
    expect(requested.size).toBeGreaterThan(Object.keys(CONDITION_FIELDS).length)
  })
})
