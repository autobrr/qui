/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncExternalStore } from "react"

import type { ProblemReportLogScope } from "@/types"

export interface ProblemReportAttempt {
  submissionId: string
  text: string
  logScope: ProblemReportLogScope
}

export interface ProblemReportDraft {
  text: string
  logScope: ProblemReportLogScope
  attempt?: ProblemReportAttempt
}

const DRAFT_KEY = "qui-problem-report-draft"
const EMPTY_DRAFT: ProblemReportDraft = { text: "", logScope: "all" }

let cache: ProblemReportDraft | undefined
const listeners = new Set<() => void>()

function readStored(): ProblemReportDraft {
  try {
    const raw = localStorage.getItem(DRAFT_KEY)
    return raw === null ? EMPTY_DRAFT : { ...EMPTY_DRAFT, ...(JSON.parse(raw) as Partial<ProblemReportDraft>) }
  } catch (error) {
    console.error("Failed to read the problem report draft:", error)
    return EMPTY_DRAFT
  }
}

function readProblemReportDraft(): ProblemReportDraft {
  cache ??= readStored()
  return cache
}

function writeDraft(draft: ProblemReportDraft) {
  const empty = draft.text === "" && draft.attempt === undefined && draft.logScope === EMPTY_DRAFT.logScope
  try {
    if (empty) localStorage.removeItem(DRAFT_KEY)
    else localStorage.setItem(DRAFT_KEY, JSON.stringify(draft))
  } catch (error) {
    console.error("Failed to store the problem report draft:", error)
  }
  cache = draft
  for (const listener of listeners) listener()
}

function attemptMatches(draft: ProblemReportDraft, attempt: ProblemReportAttempt): boolean {
  return draft.text === attempt.text && draft.logScope === attempt.logScope
}

export function isNotSent(draft: ProblemReportDraft): boolean {
  return draft.attempt !== undefined && draft.text.trim() !== ""
}

export function updateProblemReportDraft(change: Partial<Pick<ProblemReportDraft, "text" | "logScope">>) {
  writeDraft({ ...readProblemReportDraft(), ...change })
}

export function clearProblemReportDraft() {
  writeDraft(EMPTY_DRAFT)
}

export function startProblemReportDraftSend(newSubmissionId: () => string): ProblemReportAttempt {
  const draft = readProblemReportDraft()
  if (draft.attempt && attemptMatches(draft, draft.attempt)) return draft.attempt
  const attempt = { submissionId: newSubmissionId(), text: draft.text, logScope: draft.logScope }
  writeDraft({ ...draft, attempt })
  return attempt
}

export function finishProblemReportDraftSend(attempt: ProblemReportAttempt) {
  const draft = readProblemReportDraft()
  if (draft.attempt?.submissionId !== attempt.submissionId) return
  if (attemptMatches(draft, attempt)) clearProblemReportDraft()
  else writeDraft({ text: draft.text, logScope: draft.logScope })
}

window.addEventListener("storage", (event) => {
  if (event.key !== null && event.key !== DRAFT_KEY) return
  cache = undefined
  for (const listener of listeners) listener()
})

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function useProblemReportDraft(): ProblemReportDraft {
  return useSyncExternalStore(subscribe, readProblemReportDraft)
}

export function _resetProblemReportDraftForTests() {
  cache = undefined
}
