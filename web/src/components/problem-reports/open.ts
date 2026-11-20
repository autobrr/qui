/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncExternalStore } from "react"

import type { ProblemReportStartedFrom } from "@/types"

/** The view of the dialog: the report form, or Sent reports. */
export type ProblemReportView = "compose" | "sent"

/** The starting point and the view stay after a close, so the dialog keeps its content while it fades out. */
export interface ProblemReportOpenState {
  open: boolean
  startedFrom: ProblemReportStartedFrom
  view: ProblemReportView
}

let state: ProblemReportOpenState = { open: false, startedFrom: "menu", view: "compose" }
const listeners = new Set<() => void>()

function setState(next: ProblemReportOpenState) {
  state = next
  for (const listener of listeners) listener()
}

/** Opens the one report dialog of the app, from any entry point. */
export function openProblemReport(startedFrom: ProblemReportStartedFrom) {
  setState({ open: true, startedFrom, view: "compose" })
}

/** Switches the open dialog between the report form and Sent reports. The form keeps its text. */
export function showProblemReportView(view: ProblemReportView) {
  setState({ ...state, view })
}

export function closeProblemReport() {
  setState({ ...state, open: false })
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function useProblemReportOpenState(): ProblemReportOpenState {
  return useSyncExternalStore(subscribe, () => state)
}
