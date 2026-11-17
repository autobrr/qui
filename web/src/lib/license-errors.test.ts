/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import settings from "@/i18n/locales/en/settings.json"
import { getLicenseErrorKey } from "./license-errors"

const cases: Array<[string, string]> = [
  ["failed to activate license: license activation limit exceeded: License key activation limit reached", "themes.license.errors.activationLimit"],
  ["license key has expired", "themes.license.errors.expired"],
  ["license does not match required conditions", "themes.license.errors.databaseCopied"],
  ["license key does not match", "themes.license.errors.conditionsMismatch"],
  ["rate limit hit", "themes.license.errors.rateLimited"],
  ["Failed to delete license", "themes.license.errors.generic"],
]

function resolve(key: string): unknown {
  return key.split(".").reduce<unknown>((node, part) => (node as Record<string, unknown> | undefined)?.[part], settings)
}

describe("getLicenseErrorKey", () => {
  it("returns null without an error", () => {
    expect(getLicenseErrorKey(null)).toBeNull()
  })

  it.each(cases)("maps %j to %s", (message, key) => {
    expect(getLicenseErrorKey(new Error(message))).toBe(key)
    expect(typeof resolve(key)).toBe("string")
  })
})
