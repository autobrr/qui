import { resolveTrackerHealthSupport } from "@/lib/tracker-health-support"
import { describe, expect, it } from "vitest"

describe("resolveTrackerHealthSupport", () => {
  it("uses response support for unified views", () => {
    expect(resolveTrackerHealthSupport({
      isUnifiedView: true,
      capabilitySupport: false,
      responseSupport: true,
    })).toBe(true)
  })

  it("preserves explicit false response support for unified views", () => {
    expect(resolveTrackerHealthSupport({
      isUnifiedView: true,
      capabilitySupport: true,
      responseSupport: false,
    })).toBe(false)
  })

  it("uses capability support for concrete instance views", () => {
    expect(resolveTrackerHealthSupport({
      isUnifiedView: false,
      capabilitySupport: true,
      responseSupport: false,
    })).toBe(true)
  })
})
