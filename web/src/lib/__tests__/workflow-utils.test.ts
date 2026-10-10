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
  toEditInput,
  toExportFormat,
  toExportJSON,
  type WorkflowExport,
  type WorkflowImport
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

  it("copies trackerPattern as is and has no trackerDomains", () => {
    for (const trackerPattern of ["a.com,b.org", "*", "foo,!bar"]) {
      const result = toExportFormat(makeAutomation({ trackerPattern }))
      expect(result.trackerPattern).toBe(trackerPattern)
      expect(result).not.toHaveProperty("trackerDomains")
    }
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

  it("carries freeSpaceSource through export and import; omits the key when unset", () => {
    const source = { type: "path" as const, path: "/data" }
    const exported = toExportFormat(makeAutomation({ freeSpaceSource: source }))
    expect(exported.freeSpaceSource).toEqual(source)
    const parsed = parseImportJSON(toExportJSON(exported))
    expect(parsed.data?.freeSpaceSource).toEqual(source)
    expect(fromImportFormat(parsed.data!, []).freeSpaceSource).toEqual(source)
    expect(toExportFormat(makeAutomation())).not.toHaveProperty("freeSpaceSource")
  })
})

// Intent: turn clipboard JSON back into an AutomationInput. Two safety
// invariants matter: imported automations always START disabled (so a paste
// can't immediately fire side-effects) and the imported name is uniquified
// so it can't clobber an existing automation by accident.
describe("fromImportFormat", () => {
  const baseExport = (overrides: Partial<WorkflowImport> = {}): WorkflowImport => ({
    name: "Imported",
    trackerPattern: "a.com",
    conditions,
    ...overrides,
  })

  it("forces enabled=false on every import", () => {
    expect(fromImportFormat(baseExport(), []).enabled).toBe(false)
    expect(fromImportFormat(baseExport({ enabled: true }), []).enabled).toBe(false)
  })

  // A rule pasted from an API response carries its old position; the import must still go to the end.
  it("drops sortOrder so the backend appends the rule", () => {
    const result = fromImportFormat(baseExport({ sortOrder: 3 }), [])
    expect(JSON.parse(JSON.stringify(result))).not.toHaveProperty("sortOrder")
  })

  it("uniquifies the name against existing names", () => {
    expect(fromImportFormat(baseExport({ name: "My workflow" }), ["My workflow"]).name).toBe("My workflow (copy)")
  })

  // The backend resolves the two fields: a non-empty trackerPattern wins.
  it("passes trackerPattern and trackerDomains through unchanged", () => {
    const result = fromImportFormat(baseExport({ trackerPattern: "a.com", trackerDomains: ["b.com"] }), [])
    expect(result.trackerPattern).toBe("a.com")
    expect(result).toHaveProperty("trackerDomains", ["b.com"])
  })

  it("preserves pattern-only tracker imports", () => {
    const result = fromImportFormat(baseExport({ trackerPattern: "a.com,!b.com" }), [])
    expect(result.trackerPattern).toBe("a.com,!b.com")
    expect(result).not.toHaveProperty("trackerDomains")
  })

  // The backend checks every key and lists each problem, so unknown keys must reach it.
  it("passes unknown top-level keys through to the backend", () => {
    const result = fromImportFormat(baseExport({ enabeld: true, extra: { a: 1 } }), [])
    expect(result).toHaveProperty("enabeld", true)
    expect(result).toHaveProperty("extra", { a: 1 })
  })

  it("leaves omitted dryRun and notify to the backend defaults", () => {
    const result = fromImportFormat(baseExport(), [])
    expect(result).not.toHaveProperty("dryRun")
    expect(result).not.toHaveProperty("notify")
  })

  it("preserves explicit dryRun and notify from the import", () => {
    const result = fromImportFormat(baseExport({ dryRun: true, notify: false }), [])
    expect(result.dryRun).toBe(true)
    expect(result.notify).toBe(false)
  })

  it("passes intervalSeconds as written", () => {
    expect(fromImportFormat(baseExport({ intervalSeconds: 900 }), []).intervalSeconds).toBe(900)
    expect(fromImportFormat(baseExport({ intervalSeconds: 30 }), []).intervalSeconds).toBe(30)
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

// Intent: "Edit as JSON" sends the JSON as written and keeps the state the
// JSON never carries: enabled and sortOrder. An omitted optional key resets to
// its backend default, the same as an import would.
describe("toEditInput", () => {
  it("keeps enabled and sortOrder from the rule and takes every other key from the JSON", () => {
    const rule = makeAutomation({ id: 5, enabled: true, sortOrder: 3, dryRun: true, intervalSeconds: 60, name: "Old" })
    const result = toEditInput(rule, { name: "New", trackerPattern: "", trackerDomains: ["a.com"], conditions, enabled: false, sortOrder: 9 })
    expect(result).toEqual({
      name: "New",
      enabled: true,
      sortOrder: 3,
      trackerPattern: "",
      trackerDomains: ["a.com"],
      conditions,
    })
    expect(result).not.toHaveProperty("id")
  })

  it("passes unknown top-level keys through to the backend", () => {
    const result = toEditInput(makeAutomation(), { name: "x", trackerPattern: "*", conditions, notfy: false })
    expect(result).toHaveProperty("notfy", false)
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
    expect(result.error).toBe("preferences.workflowsOverview.importDialog.errors.invalidJson")
  })

  it("rejects non-object root values", () => {
    expect(parseImportJSON("123").error).toBe("preferences.workflowsOverview.importDialog.errors.notObject")
    expect(parseImportJSON("null").error).toBe("preferences.workflowsOverview.importDialog.errors.notObject")
    // Arrays pass the typeof === "object" check, then fail on missing 'name'.
    // Pinning this behavior so future readers know arrays aren't a special case.
    expect(parseImportJSON("[]").error).toBe("preferences.workflowsOverview.importDialog.errors.missingName")
  })

  it("rejects missing/empty name", () => {
    expect(parseImportJSON(JSON.stringify({ conditions: { schemaVersion: "1" } })).error).toBe(
      "preferences.workflowsOverview.importDialog.errors.missingName"
    )
    expect(parseImportJSON(JSON.stringify({ name: "   ", conditions: { schemaVersion: "1" } })).error).toBe(
      "preferences.workflowsOverview.importDialog.errors.missingName"
    )
  })

  it("rejects missing conditions", () => {
    expect(parseImportJSON(JSON.stringify({ name: "x" })).error).toBe(
      "preferences.workflowsOverview.importDialog.errors.missingConditions"
    )
  })

  it("keeps every key as written, unknown keys too", () => {
    const input = {
      name: "x",
      conditions: { schemaVersion: "1" },
      trackerDomains: ["b.com", 1],
      intervalSeconds: 30,
      enabeld: true,
    }
    expect(parseImportJSON(JSON.stringify(input)).data).toEqual(input)
  })

  it("leaves a missing tracker to the backend", () => {
    const result = parseImportJSON(JSON.stringify({ name: "x", conditions: { schemaVersion: "1" } }))
    expect(result.error).toBeNull()
    expect(result.data).not.toHaveProperty("trackerPattern")
  })

  it("keeps dryRun: true from the export shape", () => {
    const result = parseImportJSON(JSON.stringify({
      name: "x",
      conditions: { schemaVersion: "1" },
      dryRun: true,
    }))
    expect(result.data?.dryRun).toBe(true)
  })

  it("includes notify only when it's an explicit boolean", () => {
    const withNotify = parseImportJSON(JSON.stringify({
      name: "x",
      conditions: { schemaVersion: "1" },
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
      conditions,
    }
    expect(toExportJSON(data)).toBe(JSON.stringify(data, null, 2))
    expect(toExportJSON(data)).toContain("\n  \"name\": \"x\"")
  })
})
