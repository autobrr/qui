/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  fromImportFormat,
  generateUniqueName,
  getTrackerMatchMode,
  getTrackerTokens,
  parseImportJSON,
  toDuplicateInput,
  toExportFormat,
  toExportJSON,
  type WorkflowExport
} from "@/lib/workflow-utils"
import type { ActionConditions, Automation } from "@/types"
import { describe, expect, it } from "vitest"

const conditions: ActionConditions = { schemaVersion: "1" }

function makeAutomation(overrides: Partial<Automation> = {}): Automation {
  return {
    id: 1,
    instanceId: 1,
    name: "My workflow",
    trackerPattern: "",
    trackerDomains: [],
    conditions,
    enabled: true,
    dryRun: false,
    notify: true,
    sortOrder: 0,
    ...overrides,
  }
}

// Intent: turn an internal Automation row into the clipboard JSON shape.
// Strips id/instanceId/sortOrder/enabled (internal state that shouldn't
// travel between deployments) and drops intervalSeconds when it equals the
// default 900 (smaller, less surprising JSON for the common case).
describe("toExportFormat", () => {
  it("omits id, instanceId, sortOrder, enabled", () => {
    const result = toExportFormat(makeAutomation({ id: 42, instanceId: 99, sortOrder: 7, enabled: true }))
    expect(result).not.toHaveProperty("id")
    expect(result).not.toHaveProperty("instanceId")
    expect(result).not.toHaveProperty("sortOrder")
    expect(result).not.toHaveProperty("enabled")
  })

  it("derives trackerPattern from trackerDomains when domains are present", () => {
    const result = toExportFormat(makeAutomation({ trackerDomains: ["a.com", "b.org"], trackerPattern: "ignored" }))
    expect(result.trackerPattern).toBe("a.com,b.org")
    expect(result.trackerDomains).toEqual(["a.com", "b.org"])
  })

  it("preserves the '*' wildcard pattern when domains are empty", () => {
    const result = toExportFormat(makeAutomation({ trackerDomains: [], trackerPattern: "*" }))
    expect(result.trackerPattern).toBe("*")
  })

  it("preserves trackerPattern when domains are empty", () => {
    expect(toExportFormat(makeAutomation({ trackerDomains: [], trackerPattern: "foo,!bar" })).trackerPattern).toBe("foo,!bar")
  })

  it("omits intervalSeconds when it equals the 900 default", () => {
    expect(toExportFormat(makeAutomation({ intervalSeconds: 900 }))).not.toHaveProperty("intervalSeconds")
  })

  it("includes intervalSeconds when it differs from the default", () => {
    expect(toExportFormat(makeAutomation({ intervalSeconds: 60 })).intervalSeconds).toBe(60)
  })

  it("emits dryRun: true only when set; omits the field otherwise", () => {
    expect(toExportFormat(makeAutomation({ dryRun: true })).dryRun).toBe(true)
    expect(toExportFormat(makeAutomation({ dryRun: false }))).not.toHaveProperty("dryRun")
  })

  it("emits notify: false only when explicitly disabled; omits the field when true", () => {
    expect(toExportFormat(makeAutomation({ notify: false })).notify).toBe(false)
    expect(toExportFormat(makeAutomation({ notify: true }))).not.toHaveProperty("notify")
  })
})

// Intent: turn clipboard JSON back into an AutomationInput. Two safety
// invariants matter: imported automations always START disabled (so a paste
// can't immediately fire side-effects) and the imported name is uniquified
// so it can't clobber an existing automation by accident.
describe("fromImportFormat", () => {
  const baseExport = (overrides: Partial<WorkflowExport> = {}): WorkflowExport => ({
    name: "Imported",
    trackerPattern: "",
    trackerDomains: ["a.com"],
    conditions,
    ...overrides,
  })

  it("forces enabled=false on every import", () => {
    expect(fromImportFormat(baseExport(), []).enabled).toBe(false)
  })

  it("uniquifies the name against existing names", () => {
    expect(fromImportFormat(baseExport({ name: "My workflow" }), ["My workflow"]).name).toBe("My workflow (copy)")
  })

  it("rebuilds trackerPattern from trackerDomains (domains are authoritative)", () => {
    expect(
      fromImportFormat(baseExport({ trackerDomains: ["a.com", "b.com"], trackerPattern: "stale" }), []).trackerPattern
    ).toBe("a.com,b.com")
  })

  it("preserves pattern-only tracker imports", () => {
    const result = fromImportFormat(baseExport({ trackerDomains: [], trackerPattern: "a.com,!b.com" }), [])
    expect(result.trackerPattern).toBe("a.com,!b.com")
    expect(result.trackerDomains).toEqual([])
  })

  it("defaults dryRun to false and notify to true when omitted", () => {
    const result = fromImportFormat(baseExport(), [])
    expect(result.dryRun).toBe(false)
    expect(result.notify).toBe(true)
  })

  it("preserves explicit dryRun and notify from the import", () => {
    const result = fromImportFormat(baseExport({ dryRun: true, notify: false }), [])
    expect(result.dryRun).toBe(true)
    expect(result.notify).toBe(false)
  })

  it("includes intervalSeconds only when present and not equal to default", () => {
    expect(fromImportFormat(baseExport({ intervalSeconds: 900 }), [])).not.toHaveProperty("intervalSeconds")
    expect(fromImportFormat(baseExport({ intervalSeconds: 60 }), []).intervalSeconds).toBe(60)
  })
})

describe("getTrackerTokens", () => {
  it("preserves per-token negation from trackerPattern", () => {
    expect(getTrackerTokens({ trackerPattern: "a.com,!b.com;c.com" })).toEqual(["a.com", "!b.com", "c.com"])
  })

  it("classifies mixed tracker tokens", () => {
    expect(getTrackerMatchMode(["a.com", "!b.com"])).toBe("mixed")
    expect(getTrackerMatchMode(["!a.com", "!b.com"])).toBe("exclude")
    expect(getTrackerMatchMode(["a.com", "b.com"])).toBe("include")
  })
})

// Intent: duplicate within the same instance — equivalent to export+import
// round-tripping. Guarantees the duplicate gets a fresh name and ships
// disabled, same as a clipboard import.
describe("toDuplicateInput", () => {
  it("round-trips through export -> import and produces a unique disabled copy", () => {
    const original = makeAutomation({ name: "Source", enabled: true, dryRun: true })
    const result = toDuplicateInput(original, ["Source"])
    expect(result.name).toBe("Source (copy)")
    expect(result.enabled).toBe(false)
    expect(result.dryRun).toBe(true)
  })
})

// Intent: copy-name generator used by import and duplicate. Must handle
// re-duplicates (don't grow "(copy) (copy)"), avoid collisions
// case-insensitively, and never produce a name that collides with an
// existing one when used as advertised. Catches anyone who breaks the
// stripping regex or the case-insensitive lookup.
describe("generateUniqueName", () => {
  it("appends '(copy)' on first duplication", () => {
    expect(generateUniqueName("Foo", [])).toBe("Foo (copy)")
  })

  it("collides case-insensitively with existing names", () => {
    expect(generateUniqueName("Foo", ["foo (copy)"])).toBe("Foo (copy 2)")
    expect(generateUniqueName("Foo", ["FOO (COPY)", "Foo (copy 2)"])).toBe("Foo (copy 3)")
  })

  it("strips an existing (copy) / (copy N) suffix before generating", () => {
    expect(generateUniqueName("Foo (copy)", ["Foo (copy)"])).toBe("Foo (copy 2)")
    expect(generateUniqueName("Foo (copy 5)", ["Foo (copy)"])).toBe("Foo (copy 2)")
  })

  it("trims whitespace exposed by stripping the suffix", () => {
    expect(generateUniqueName("Foo   (copy)", [])).toBe("Foo (copy)")
  })

  it("returns the first attempt when no collision exists", () => {
    expect(generateUniqueName("Foo", ["Bar", "Baz"])).toBe("Foo (copy)")
  })
})

// Intent: validate clipboard JSON before letting it through. Returns
// {data, error} discriminated tuple. Catches anyone who weakens validation
// such that a malformed paste could create a half-formed automation.
describe("parseImportJSON", () => {
  const validJSON = JSON.stringify({
    name: "Test",
    trackerPattern: "*",
    trackerDomains: ["a.com"],
    conditions: { schemaVersion: "1" },
  })

  it("returns parsed data for a valid payload", () => {
    const result = parseImportJSON(validJSON)
    expect(result.error).toBeNull()
    expect(result.data?.name).toBe("Test")
    expect(result.data?.trackerDomains).toEqual(["a.com"])
  })

  it("rejects unparseable JSON", () => {
    const result = parseImportJSON("{not json")
    expect(result.data).toBeNull()
    expect(result.error).toBe("Invalid JSON format")
  })

  it("rejects non-object root values", () => {
    expect(parseImportJSON("123").error).toBe("Expected a JSON object")
    expect(parseImportJSON("null").error).toBe("Expected a JSON object")
    // Arrays pass the typeof === "object" check, then fail on missing 'name'.
    // Pinning this behavior so future readers know arrays aren't a special case.
    expect(parseImportJSON("[]").error).toBe("Missing or invalid 'name' field")
  })

  it("rejects missing/empty name", () => {
    expect(parseImportJSON(JSON.stringify({ conditions: { schemaVersion: "1" }, trackerDomains: [] })).error).toBe(
      "Missing or invalid 'name' field"
    )
    expect(parseImportJSON(JSON.stringify({ name: "   ", conditions: { schemaVersion: "1" }, trackerDomains: [] })).error).toBe(
      "Missing or invalid 'name' field"
    )
  })

  it("rejects missing conditions", () => {
    expect(parseImportJSON(JSON.stringify({ name: "x", trackerDomains: [] })).error).toBe(
      "Missing or invalid 'conditions' field"
    )
  })

  it("requires at least one of trackerDomains or trackerPattern", () => {
    expect(parseImportJSON(JSON.stringify({ name: "x", conditions: { schemaVersion: "1" } })).error).toBe(
      "Must specify either 'trackerDomains' (array of strings) or 'trackerPattern'"
    )
  })

  it("includes intervalSeconds only when it's a number >= 60", () => {
    const withInterval = parseImportJSON(JSON.stringify({
      name: "x",
      conditions: { schemaVersion: "1" },
      trackerDomains: [],
      intervalSeconds: 120,
    }))
    expect(withInterval.data?.intervalSeconds).toBe(120)

    const tooSmall = parseImportJSON(JSON.stringify({
      name: "x",
      conditions: { schemaVersion: "1" },
      trackerDomains: [],
      intervalSeconds: 30,
    }))
    expect(tooSmall.data?.intervalSeconds).toBeUndefined()
  })

  it("includes notify only when it's an explicit boolean", () => {
    const withNotify = parseImportJSON(JSON.stringify({
      name: "x",
      conditions: { schemaVersion: "1" },
      trackerDomains: [],
      notify: false,
    }))
    expect(withNotify.data?.notify).toBe(false)
  })
})

// Intent: thin pretty-print wrapper. Pinning the indent so future readers
// know the output is human-friendly clipboard JSON, not minified.
describe("toExportJSON", () => {
  it("produces a 2-space indented JSON string", () => {
    const data: WorkflowExport = {
      name: "x",
      trackerPattern: "*",
      trackerDomains: [],
      conditions,
    }
    expect(toExportJSON(data)).toBe(JSON.stringify(data, null, 2))
    expect(toExportJSON(data)).toContain("\n  \"name\": \"x\"")
  })
})
