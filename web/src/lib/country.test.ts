/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { getCountryCode } from "./country"

describe("getCountryCode", () => {
  it("resolves valid 2-letter country codes", () => {
    expect(getCountryCode("BR")).toBe("br")
    expect(getCountryCode("us")).toBe("us")
    expect(getCountryCode("DE", "Germany")).toBe("de")
  })

  it("resolves from country name when country_code is empty or missing", () => {
    expect(getCountryCode("", "Brazil")).toBe("br")
    expect(getCountryCode(undefined, "Brasil")).toBe("br")
    expect(getCountryCode("", "United States")).toBe("us")
    expect(getCountryCode(undefined, "Deutschland")).toBe("de")
  })

  it("resolves from 3-letter ISO codes", () => {
    expect(getCountryCode("BRA")).toBe("br")
    expect(getCountryCode("USA")).toBe("us")
    expect(getCountryCode("DEU")).toBe("de")
  })

  it("handles country name passed as country_code parameter", () => {
    expect(getCountryCode("Brazil", "Brazil")).toBe("br")
    expect(getCountryCode("United States", "")).toBe("us")
  })

  it("returns undefined when no country data is resolvable", () => {
    expect(getCountryCode()).toBeUndefined()
    expect(getCountryCode("", "")).toBeUndefined()
    expect(getCountryCode("UnknownLand", "")).toBeUndefined()
  })
})
