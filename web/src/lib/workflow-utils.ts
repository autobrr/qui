/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Automation, AutomationInput, ActionConditions, FreeSpaceSource, SortingConfig } from "@/types"

export type TrackerMatchMode = "include" | "exclude" | "mixed"

/**
 * Export format for workflows. This is the clipboard JSON format.
 * - Omits id, instanceId, sortOrder, enabled
 * - Omits intervalSeconds when it equals default 900
 */
export interface WorkflowExport {
  name: string
  trackerPattern: string
  conditions: ActionConditions
  freeSpaceSource?: FreeSpaceSource
  sortingConfig?: SortingConfig
  intervalSeconds?: number
  dryRun?: boolean
  notify?: boolean
}

/** Parsed import JSON, with every key as written. The backend checks the keys and lists each problem. */
export type WorkflowImport = WorkflowExport & Record<string, unknown>

const DEFAULT_INTERVAL_SECONDS = 900

/**
 * Normalizes an Automation to export format.
 * Strips internal fields (id, instanceId, sortOrder, enabled) and
 * omits intervalSeconds when it equals the default 900.
 */
export function toExportFormat(workflow: Automation): WorkflowExport {
  const exported: WorkflowExport = {
    name: workflow.name,
    trackerPattern: workflow.trackerPattern,
    conditions: workflow.conditions,
    sortingConfig: workflow.sortingConfig,
  }

  if (workflow.freeSpaceSource) {
    exported.freeSpaceSource = workflow.freeSpaceSource
  }

  // Only include intervalSeconds if it differs from default
  if (workflow.intervalSeconds && workflow.intervalSeconds !== DEFAULT_INTERVAL_SECONDS) {
    exported.intervalSeconds = workflow.intervalSeconds
  }

  if (workflow.dryRun) {
    exported.dryRun = true
  }

  if (!workflow.notify) {
    exported.notify = false
  }

  return exported
}

export function getTrackerTokens(source: { trackerPattern?: string }): string[] {
  const tokens: string[] = []
  const seen = new Set<string>()
  for (const part of (source.trackerPattern ?? "").split(/[|,;]/)) {
    const token = part.trim()
    if (!token) continue
    const key = token.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    tokens.push(token)
  }
  return tokens
}

export function getTrackerMatchMode(tokens: string[]): TrackerMatchMode {
  if (tokens.length === 0) return "include"
  const hasExclude = tokens.some((token) => token.startsWith("!"))
  const hasInclude = tokens.some((token) => !token.startsWith("!"))
  if (hasExclude && hasInclude) return "mixed"
  return hasExclude ? "exclude" : "include"
}

/**
 * Create payload for import: the JSON as written, except that
 * - enabled is forced to false
 * - sortOrder is omitted, so the backend appends the rule
 * - name gets "(copy)" suffix via generateUniqueName
 */
export function fromImportFormat(
  data: WorkflowImport,
  existingNames: string[]
): AutomationInput {
  return {
    ...data,
    name: generateUniqueName(data.name, existingNames),
    enabled: false, // Always start disabled
    sortOrder: undefined,
  }
}

/** Update payload for "Edit as JSON": the JSON as written, except that the rule keeps enabled and sortOrder. */
export function toEditInput(rule: Automation, data: WorkflowImport): AutomationInput {
  return {
    ...data,
    enabled: rule.enabled,
    sortOrder: rule.sortOrder,
  }
}

/**
 * Creates an AutomationInput for duplicating a workflow.
 * Similar to fromImportFormat but takes an existing Automation.
 */
export function toDuplicateInput(
  workflow: Automation,
  existingNames: string[]
): AutomationInput {
  return fromImportFormat({ ...toExportFormat(workflow) }, existingNames)
}

/**
 * Generates a unique name by appending "(copy)", "(copy 2)", etc.
 * @param baseName The original name to make unique
 * @param existingNames List of names already in use
 * @returns A unique name with copy suffix
 */
export function generateUniqueName(baseName: string, existingNames: string[]): string {
  // Strip existing copy suffix from base name to get clean base
  const cleanBase = baseName.replace(/\s*\(copy(?:\s*\d+)?\)\s*$/, "").trim()

  const nameSet = new Set(existingNames.map(n => n.toLowerCase()))

  // Try "(copy)" first
  const firstAttempt = `${cleanBase} (copy)`
  if (!nameSet.has(firstAttempt.toLowerCase())) {
    return firstAttempt
  }

  // Try "(copy 2)", "(copy 3)", etc.
  let counter = 2
  while (counter < 1000) { // Safety limit
    const attempt = `${cleanBase} (copy ${counter})`
    if (!nameSet.has(attempt.toLowerCase())) {
      return attempt
    }
    counter++
  }

  // Fallback: append timestamp
  return `${cleanBase} (copy ${Date.now()})`
}

const IMPORT_ERROR_KEYS = "preferences.workflowsOverview.importDialog.errors"

/** Validates import JSON; the error is an `instances` i18n key for the caller to translate. */
export function parseImportJSON(jsonString: string): { data: WorkflowImport; error: null } | { data: null; error: string } {
  let parsed: unknown
  try {
    parsed = JSON.parse(jsonString)
  } catch {
    return { data: null, error: `${IMPORT_ERROR_KEYS}.invalidJson` }
  }

  if (typeof parsed !== "object" || parsed === null) {
    return { data: null, error: `${IMPORT_ERROR_KEYS}.notObject` }
  }

  const obj = parsed as Record<string, unknown>

  // Validate required fields
  if (typeof obj.name !== "string" || obj.name.trim() === "") {
    return { data: null, error: `${IMPORT_ERROR_KEYS}.missingName` }
  }

  if (typeof obj.conditions !== "object" || obj.conditions === null) {
    return { data: null, error: `${IMPORT_ERROR_KEYS}.missingConditions` }
  }

  // The backend rejects a rule with no tracker, so a missing tracker is not checked here.
  return { data: obj as WorkflowImport, error: null }
}

/**
 * Serializes workflow export data to a formatted JSON string.
 */
export function toExportJSON(data: WorkflowExport): string {
  return JSON.stringify(data, null, 2)
}
