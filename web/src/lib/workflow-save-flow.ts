/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { AutomationPreviewResult } from "@/types"

export type WorkflowPreview =
  | { status: "loading" }
  | { status: "loaded"; result: AutomationPreviewResult }
  | { status: "failed"; error: string }

export type WorkflowSaveFlow =
  | { step: "editing" }
  | { step: "askingDryRun" }
  | { step: "previewing"; preview: WorkflowPreview }

export type WorkflowSaveFlowEvent =
  | { type: "dryRunAsked" }
  | { type: "previewStarted" }
  // The first page, or the first page of another preview view.
  | { type: "previewLoaded"; result: AutomationPreviewResult }
  | { type: "previewFailed"; error: string }
  | { type: "morePreviewLoaded"; result: AutomationPreviewResult }
  // The user cancelled or confirmed, or the editor closed.
  | { type: "closed" }

export const EDITING: WorkflowSaveFlow = { step: "editing" }

export function workflowSaveFlowReducer(state: WorkflowSaveFlow, event: WorkflowSaveFlowEvent): WorkflowSaveFlow {
  switch (event.type) {
    case "dryRunAsked":
      return { step: "askingDryRun" }
    case "previewStarted":
      return { step: "previewing", preview: { status: "loading" } }
    case "previewLoaded":
      if (state.step !== "previewing") return state
      return { step: "previewing", preview: { status: "loaded", result: event.result } }
    case "previewFailed":
      if (state.step !== "previewing") return state
      return { step: "previewing", preview: { status: "failed", error: event.error } }
    case "morePreviewLoaded": {
      if (state.step !== "previewing" || state.preview.status !== "loaded") return state
      const { result } = state.preview
      return {
        step: "previewing",
        preview: {
          status: "loaded",
          result: { ...result, examples: [...result.examples, ...event.result.examples], totalMatches: event.result.totalMatches },
        },
      }
    }
    case "closed":
      return EDITING
  }
}
