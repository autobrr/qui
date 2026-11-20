/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncExternalStore } from "react"

import type { ProblemReportStartedFrom } from "@/types"

/** The starting point stays after a close, so the dialog keeps its content while it fades out. */
export interface ProblemReportOpenState {
  open: boolean
  startedFrom: ProblemReportStartedFrom
}

let state: ProblemReportOpenState = { open: false, startedFrom: "menu" }
const listeners = new Set<() => void>()

function setState(next: ProblemReportOpenState) {
  state = next
  for (const listener of listeners) listener()
}

/** Opens the one report dialog of the app, from any entry point. */
export function openProblemReport(startedFrom: ProblemReportStartedFrom) {
  setState({ open: true, startedFrom })
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
