/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { readFileSync } from "node:fs"
import { resolve } from "node:path"

import {
  ACTION_RUN_ORDER,
  allowanceInputOf,
  freeSpaceSourceOf,
  fromImportFormat,
  generateUniqueName,
  getTrackerMatchMode,
  getTrackerTokens,
  firstActionCondition,
  hasMixedActionConditions,
  loadWorkflowNames,
  parseImportJSON,
  toCopyInput,
  toDuplicateInput,
  toEditInput,
  toExportFormat,
  toExportJSON,
  type WorkflowExport,
  type WorkflowImport
} from "@/lib/workflow-utils"
import type { ActionConditions, Automation, RuleCondition } from "@/types"
import { QueryClient } from "@tanstack/react-query"
import { afterEach, describe, expect, it, vi } from "vitest"

const listAutomations = vi.hoisted(() => vi.fn())

vi.mock("@/lib/api", () => ({ api: { listAutomations } }))

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

  it("drops sortOrder so the backend appends the rule", () => {
    const result = fromImportFormat(baseExport({ sortOrder: 3 }), [])
    expect(JSON.parse(JSON.stringify(result))).not.toHaveProperty("sortOrder")
  })

  it("uniquifies the name against existing names", () => {
    expect(fromImportFormat(baseExport({ name: "My workflow" }), ["My workflow"]).name).toBe("My workflow (copy)")
    expect(fromImportFormat(baseExport({ name: "My workflow" }), ["MY WORKFLOW", "My workflow (copy)"]).name).toBe("My workflow (copy 2)")
  })

  it("keeps the name when the instance has no rule with that name", () => {
    expect(fromImportFormat(baseExport({ name: "My workflow" }), ["Other"]).name).toBe("My workflow")
  })

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

describe("toDuplicateInput", () => {
  it("round-trips through export -> import and produces a unique disabled copy", () => {
    const original = makeAutomation({ name: "Source", enabled: true, dryRun: true })
    const result = toDuplicateInput(original, ["Source"])
    expect(result.name).toBe("Source (copy)")
    expect(result.enabled).toBe(false)
    expect(result.dryRun).toBe(true)
  })

  it("adds a copy suffix when no existing name is known", () => {
    expect(toDuplicateInput(makeAutomation({ name: "Source" }), []).name).toBe("Source (copy)")
  })
})

describe("toCopyInput", () => {
  it("keeps the name when the target instance has no rule with that name", () => {
    const result = toCopyInput(makeAutomation({ name: "Source", enabled: true }), ["Other"])
    expect(result.name).toBe("Source")
    expect(result.enabled).toBe(false)
    expect(result.sortOrder).toBeUndefined()
  })

  it("adds a copy suffix when the target instance has the name in another case", () => {
    expect(toCopyInput(makeAutomation({ name: "Source" }), ["source"]).name).toBe("Source (copy)")
    expect(toCopyInput(makeAutomation({ name: "Source" }), ["SOURCE", "Source (copy)"]).name).toBe("Source (copy 2)")
  })
})

describe("loadWorkflowNames", () => {
  afterEach(() => {
    listAutomations.mockReset()
  })

  it("reads the rule list from the server when it is not loaded", async () => {
    listAutomations.mockResolvedValueOnce([makeAutomation({ name: "Source" })])
    expect(await loadWorkflowNames(new QueryClient(), 2)).toEqual(["Source"])
    expect(listAutomations).toHaveBeenCalledWith(2)
  })

  it("uses the loaded rule list", async () => {
    const queryClient = new QueryClient()
    queryClient.setQueryData(["automations", 2], [makeAutomation({ name: "Loaded" })])
    expect(await loadWorkflowNames(queryClient, 2)).toEqual(["Loaded"])
    expect(listAutomations).not.toHaveBeenCalled()
  })

  it("returns no names when the server sends null for an instance with no rules", async () => {
    listAutomations.mockResolvedValueOnce(null)
    expect(await loadWorkflowNames(new QueryClient(), 2)).toEqual([])
  })

  it("rejects when the rule list cannot be read", async () => {
    listAutomations.mockRejectedValueOnce(new Error("list failed"))
    await expect(loadWorkflowNames(new QueryClient(), 2)).rejects.toThrow("list failed")
  })
})

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

})

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

})

describe("toExportJSON", () => {
  it("produces a 2-space indented JSON string", () => {
    const data: WorkflowExport = {
      name: "x",
      trackerPattern: "*",
      conditions,
    }
    expect(toExportJSON(data)).toBe("{\n  \"name\": \"x\",\n  \"trackerPattern\": \"*\",\n  \"conditions\": {\n    \"schemaVersion\": \"1\"\n  }\n}")
  })
})

describe("ACTION_RUN_ORDER", () => {
  it("matches the backend's actionRunOrder", () => {
    const source = readFileSync(resolve(import.meta.dirname, "../../../../internal/services/automations/service.go"), "utf8")

    const slice = source.match(/^var actionRunOrder = \[\]string\{\n([^}]*)\}/m)
    expect(slice).not.toBeNull()
    const goKeys = [...slice![1].matchAll(/^\t"(\w+)",$/gm)].map(m => m[1])

    expect(goKeys.length).toBeGreaterThan(0)
    expect([...ACTION_RUN_ORDER]).toEqual(goKeys)
  })
})

describe("freeSpaceSourceOf", () => {
  const TiB = 1024 ** 4

  it("turns the typed allowance into bytes", () => {
    expect(freeSpaceSourceOf({ type: "allowance", path: " /home/user ", allowance: "1.5", allowanceUnit: TiB }))
      .toEqual({ type: "allowance", path: "/home/user", allowanceBytes: 1649267441664 })
  })

  it("keeps the path source as it is", () => {
    expect(freeSpaceSourceOf({ type: "path", path: "/mnt/data", allowance: "1.5", allowanceUnit: TiB }))
      .toEqual({ type: "path", path: "/mnt/data" })
    expect(freeSpaceSourceOf({ type: "qbitPath", path: "D:\\downloads", allowance: "", allowanceUnit: TiB }))
      .toEqual({ type: "qbitPath", path: "D:\\downloads" })
  })

  it("sends no source without a path, without an allowance, and for the default", () => {
    expect(freeSpaceSourceOf({ type: "allowance", path: "", allowance: "1.5", allowanceUnit: TiB })).toBeUndefined()
    expect(freeSpaceSourceOf({ type: "allowance", path: "/home/user", allowance: "", allowanceUnit: TiB })).toBeUndefined()
    expect(freeSpaceSourceOf({ type: "allowance", path: "/home/user", allowance: "0", allowanceUnit: TiB })).toBeUndefined()
    expect(freeSpaceSourceOf({ type: "qbittorrent", path: "/mnt/data", allowance: "", allowanceUnit: TiB })).toBeUndefined()
  })
})

describe("allowanceInputOf", () => {
  it("shows a saved allowance as the figure that the user typed", () => {
    expect(allowanceInputOf(1499733860286)).toEqual({ allowance: "1.364", allowanceUnit: 1024 ** 4 })
    expect(allowanceInputOf(1397 * 1024 ** 3)).toEqual({ allowance: "1397", allowanceUnit: 1024 ** 3 })
    expect(allowanceInputOf(500 * 1024 ** 3)).toEqual({ allowance: "500", allowanceUnit: 1024 ** 3 })
    expect(allowanceInputOf(750 * 1024 ** 2)).toEqual({ allowance: "750", allowanceUnit: 1024 ** 2 })
    expect(allowanceInputOf(1500000000000)).toEqual({ allowance: "1.5", allowanceUnit: 1000 ** 4 })
    expect(allowanceInputOf(500 * 1000 ** 3)).toEqual({ allowance: "500", allowanceUnit: 1000 ** 3 })
  })

  it("keeps the bytes of an allowance that no short figure holds", () => {
    const input = allowanceInputOf(1500000000001)
    expect(input).toEqual({ allowance: "1430511.4746103287", allowanceUnit: 1024 ** 2 })
    expect(freeSpaceSourceOf({ type: "allowance", path: "/home/user", ...input }))
      .toEqual({ type: "allowance", path: "/home/user", allowanceBytes: 1500000000001 })
  })
})

describe("hasMixedActionConditions", () => {
  const packed: RuleCondition = { field: "NAME", operator: "CONTAINS", value: "packed" }
  const unpacked: RuleCondition = { field: "NAME", operator: "CONTAINS", value: "unpacked" }
  const tag = (tags: string[], condition?: RuleCondition, enabled = true) => ({ enabled, tags, mode: "full" as const, condition })

  it.each([
    { name: "two tag actions with different conditions", mixed: true, conditions: { schemaVersion: "1", tags: [tag(["packed"], packed), tag(["unpacked"], unpacked)] } },
    { name: "a condition on category and none on pause", mixed: true, conditions: { schemaVersion: "1", category: { enabled: true, category: "sorted", condition: packed }, pause: { enabled: true } } },
    { name: "the same condition on every action", mixed: false, conditions: { schemaVersion: "1", tags: [tag(["packed"], packed)], pause: { enabled: true, condition: packed } } },
    { name: "a different condition on a disabled action", mixed: false, conditions: { schemaVersion: "1", tags: [tag(["packed"], packed), tag(["unpacked"], unpacked, false)] } },
    { name: "another condition on a disabled action that comes first", mixed: false, conditions: { schemaVersion: "1", speedLimits: { enabled: false, condition: unpacked }, pause: { enabled: true, condition: packed } } },
    { name: "auto management set to off with no condition", mixed: true, conditions: { schemaVersion: "1", pause: { enabled: true, condition: packed }, autoManagement: { enabled: false } } },
  ])("returns $mixed for $name", ({ mixed, conditions }) => {
    expect(hasMixedActionConditions(makeAutomation({ conditions }))).toBe(mixed)
  })
})

describe("firstActionCondition", () => {
  const packed: RuleCondition = { field: "NAME", operator: "CONTAINS", value: "packed" }
  const unpacked: RuleCondition = { field: "NAME", operator: "CONTAINS", value: "unpacked" }

  it.each([
    { name: "skips a switched-off action", conditions: { schemaVersion: "1", speedLimits: { enabled: false, condition: unpacked }, pause: { enabled: true, condition: packed } } },
    { name: "skips a switched-off tag action", conditions: { schemaVersion: "1", tags: [{ enabled: false, tags: ["a"], mode: "full" as const, condition: unpacked }, { enabled: true, tags: ["b"], mode: "full" as const, condition: packed }] } },
    { name: "skips a running action without a condition", conditions: { schemaVersion: "1", pause: { enabled: true }, category: { enabled: true, category: "sorted", condition: packed } } },
  ])("$name", ({ conditions }) => {
    expect(firstActionCondition(makeAutomation({ conditions }))).toEqual(packed)
  })

  it("returns null when no running action has a condition", () => {
    const conditions = { schemaVersion: "1", speedLimits: { enabled: false, condition: unpacked }, pause: { enabled: true } }
    expect(firstActionCondition(makeAutomation({ conditions }))).toBeNull()
  })
})
