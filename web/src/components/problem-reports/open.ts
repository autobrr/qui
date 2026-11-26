/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncExternalStore } from "react"

import type { ProblemReportStartedFrom } from "@/types"

export type ProblemReportView = "compose" | "sent"

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

export function openProblemReport(startedFrom: ProblemReportStartedFrom) {
  setState({ open: true, startedFrom, view: "compose" })
}

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
