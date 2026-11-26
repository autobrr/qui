/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { EDITING, workflowSaveFlowReducer, type WorkflowSaveFlow, type WorkflowSaveFlowEvent } from "@/lib/workflow-save-flow"
import type { AutomationPreviewResult } from "@/types"

function run(events: WorkflowSaveFlowEvent[], state: WorkflowSaveFlow = EDITING): WorkflowSaveFlow {
  return events.reduce(workflowSaveFlowReducer, state)
}

function result(names: string[], totalMatches: number): AutomationPreviewResult {
  return { totalMatches, examples: names.map(name => ({ name })) } as AutomationPreviewResult
}

const page1 = result(["a", "b"], 3)
const page2 = result(["c"], 3)

const askingDryRun = run([{ type: "dryRunAsked" }])
const loading = run([{ type: "previewStarted" }])
const loaded = run([{ type: "previewStarted" }, { type: "previewLoaded", result: page1 }])
const failed = run([{ type: "previewStarted" }, { type: "previewFailed", error: "HTTP 504" }])

describe("workflowSaveFlowReducer", () => {
  it("asks about dry run", () => {
    expect(askingDryRun).toEqual({ step: "askingDryRun" })
  })

  it("shows the preview as loading, then its result", () => {
    expect(loading).toEqual({ step: "previewing", preview: { status: "loading" } })
    expect(loaded).toEqual({ step: "previewing", preview: { status: "loaded", result: page1 } })
  })

  it("stays in the preview with the error when the preview fails", () => {
    expect(failed).toEqual({ step: "previewing", preview: { status: "failed", error: "HTTP 504" } })
  })

  it("forgets an earlier failure when a new preview starts", () => {
    expect(workflowSaveFlowReducer(failed, { type: "previewStarted" })).toEqual(loading)
  })

  it.each([
    ["the dry-run question", askingDryRun],
    ["a loading preview", loading],
    ["a loaded preview", loaded],
    ["a failed preview", failed],
  ])("goes back to editing when the user closes %s", (_name, state) => {
    expect(workflowSaveFlowReducer(state, { type: "closed" })).toEqual(EDITING)
  })

  it("adds the next page to the examples and takes its total", () => {
    const next = workflowSaveFlowReducer(loaded, { type: "morePreviewLoaded", result: result(["c"], 4) })

    expect(next).toEqual({
      step: "previewing",
      preview: { status: "loaded", result: { totalMatches: 4, examples: [{ name: "a" }, { name: "b" }, { name: "c" }] } },
    })
  })

  it.each<[string, WorkflowSaveFlow, WorkflowSaveFlowEvent]>([
    ["a preview result after the user closed the preview", EDITING, { type: "previewLoaded", result: page1 }],
    ["a preview failure after the user closed the preview", EDITING, { type: "previewFailed", error: "HTTP 504" }],
    ["a next page after the user closed the preview", EDITING, { type: "morePreviewLoaded", result: page2 }],
    ["a next page while the preview loads", loading, { type: "morePreviewLoaded", result: page2 }],
  ])("ignores %s", (_name, state, event) => {
    expect(workflowSaveFlowReducer(state, event)).toBe(state)
  })
})
