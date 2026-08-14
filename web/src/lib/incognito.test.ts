/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it } from "vitest"
import { getIncognitoTags, getLinuxCategory, getLinuxIsoName, getLinuxSavePath, getLinuxTrackerDomain } from "./incognito"
import { SPREADSHEET_THEME_ID } from "./spreadsheet-disguise"

afterEach(() => {
  document.documentElement.removeAttribute("data-theme")
})

const HASH = "abcdef1234567890abcdef1234567890abcdef12"

describe("incognito flavors", () => {
  it("serves linux vocabulary by default", () => {
    expect(getLinuxIsoName(HASH)).toMatch(/\.iso$/)
    expect(getLinuxTrackerDomain(HASH)).not.toMatch(/corp\.internal$/)
  })

  it("serves spreadsheet vocabulary when the theme is active", () => {
    document.documentElement.setAttribute("data-theme", SPREADSHEET_THEME_ID)
    expect(getLinuxIsoName(HASH)).toMatch(/\.(xlsx|csv|pdf|docx|vsdx)$/)
    expect(getLinuxTrackerDomain(HASH)).toMatch(/corp\.internal$/)
    expect(getLinuxSavePath(HASH)).toMatch(/^\/shares\//)
    expect(getIncognitoTags(true)).toContain("pending review")
  })

  it("is deterministic per hash within a flavor", () => {
    document.documentElement.setAttribute("data-theme", SPREADSHEET_THEME_ID)
    expect(getLinuxIsoName(HASH)).toBe(getLinuxIsoName(HASH))
    expect(getLinuxCategory(HASH)).toBe(getLinuxCategory(HASH))
  })
})
