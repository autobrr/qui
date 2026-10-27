/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it } from "vitest"

import i18n, { changeLanguage } from "@/i18n"
import deAutomations from "@/i18n/locales/de/automations.json"
import { createInstance, type TFunction } from "i18next"
import { FIELD_REQUIREMENTS, getCapabilityReason } from "./constants"
import * as constants from "./constants"

afterEach(async () => {
  await changeLanguage("en")
})

describe("getCapabilityReason", () => {
  it("renders the automations translation, not the English constant", async () => {
    await changeLanguage("de")

    expect(getCapabilityReason("trackerHealth", i18n.t)).toBe(deAutomations.queryBuilder.capabilityReasons.trackerHealth)
    // An instance without Read has no filesystem access, local or remote.
    expect(getCapabilityReason("read", i18n.t)).toBe(deAutomations.queryBuilder.capabilityReasons.localFilesystemAccess)
    // Trusted identity needs local access, so a remote instance can never get it.
    expect(getCapabilityReason("trustedIdentity", i18n.t)).toBe(deAutomations.queryBuilder.capabilityReasons.localFilesystemAccess)
    expect(getCapabilityReason("trustedIdentity", i18n.t, true)).toBe(deAutomations.queryBuilder.capabilityReasons.remoteInstance)
  })
})

describe("FIELD_REQUIREMENTS", () => {
  it("maps each requirement in the Go table to a capability", () => {
    expect(FIELD_REQUIREMENTS).toEqual({
      IS_UNREGISTERED: "trackerHealth",
      TRACKER_STATUS: "trackerHealth",
      TRACKER_MESSAGE: "trackerHealth",
      HAS_MISSING_FILES: "read",
      HARDLINK_SCOPE: "trustedIdentity",
      HARDLINK_SCOPE_CROSS: "trustedIdentity",
    })
  })
})

// The English labels in constants.ts are only defaultValues, so a missing key renders
// English in every locale without failing any other check.
describe("query-builder translation keys", () => {
  it("every label the helpers look up has an English key", () => {
    const requested = new Set<string>()
    const recordingT = ((key: string) => {
      requested.add(key)
      return key
    }) as TFunction

    const fields = Object.keys(constants.CONDITION_FIELDS)
    for (const field of fields) {
      constants.getFieldLabel(field, recordingT)
    }
    for (const group of constants.FIELD_GROUPS) {
      constants.getFieldGroupLabel(group.label, recordingT)
    }
    const capabilities = new Set([...Object.values(constants.FIELD_REQUIREMENTS), ...Object.values(constants.STATE_VALUE_REQUIREMENTS)])
    for (const capability of capabilities) {
      constants.getCapabilityReason(capability, recordingT)
      constants.getCapabilityReason(capability, recordingT, true)
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

describe("getFieldLabel", () => {
  it("has no fallback of its own when the key is missing", async () => {
    const fields = Object.keys(constants.CONDITION_FIELDS)
    const empty = createInstance()
    await empty.init({ lng: "en", resources: {} })
    expect(fields.map((field) => constants.getFieldLabel(field, empty.t))).toEqual(fields.map((field) => `queryBuilder.fields.${field}`))
  })

  it("renders the locale string", async () => {
    const withKey = createInstance()
    await withKey.init({ lng: "en", resources: { en: { translation: { queryBuilder: { fields: { SAVE_PATH: "probe-string" } } } } } })
    expect(constants.getFieldLabel("SAVE_PATH", withKey.t)).toBe("probe-string")
  })
})

describe("getFieldType", () => {
  it.each([
    ["HARDLINK_SCOPE", "hardlinkScope"],
    ["STATE", "state"],
    ["SAVE_PATH", "string"],
    ["SIZE", "bytes"],
    ["ADDED_ON", "duration"],
    ["DL_SPEED", "speed"],
    ["PROGRESS", "percentage"],
    ["RATIO", "float"],
    ["NOT_A_FIELD", "string"],
    ["__proto__", "string"],
    ["constructor", "string"],
    ["toString", "string"],
  ])("returns the type of %s, and string for an id with no entry", (field, type) => {
    expect(constants.getFieldType(field)).toBe(type)
  })
})

describe("getTranslatedOperatorsForField", () => {
  const labels = (field: string) => constants.getTranslatedOperatorsForField(field, i18n.getFixedT("en", "automations")).map((op) => op.label)

  it("shows symbols for a numeric field, words for a string field, and is or is not for a value list", () => {
    expect(labels("SIZE")).toEqual(["=", "!=", ">", ">=", "<", "<=", "between"])
    expect(labels("NAME")).toEqual(["equals", "not equals", "contains", "not contains", "starts with", "ends with", "matches regex", "exists in", "similar exists in"])
    expect(labels("STATE")).toEqual(["is", "is not"])
    expect(labels("PRIVATE")).toEqual(["is", "is not"])
    expect(labels("NOT_A_FIELD")).toEqual(labels("SAVE_PATH"))
  })
})

describe("getTranslatedHardlinkScopes", () => {
  it("leaves out the \"both\" scope", () => {
    expect(constants.getTranslatedHardlinkScopes(i18n.getFixedT("en", "automations")).map((scope) => scope.value)).toEqual(["none", "torrents_only", "inside_qbittorrent", "outside_qbittorrent"])
  })
})
