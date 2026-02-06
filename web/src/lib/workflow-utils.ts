/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Automation, AutomationInput, ActionConditions } from "@/types"

/**
 * Export format for workflows. This is the clipboard JSON format.
 * - Includes trackerDomains (primary) and derived trackerPattern
 * - Omits id, instanceId, sortOrder, enabled
 * - Omits intervalSeconds when it equals default 900
 */
export interface WorkflowExport {
  name: string
  trackerPattern: string
  trackerDomains: string[]
  conditions: ActionConditions
  intervalSeconds?: number
  dryRun?: boolean
}

const DEFAULT_INTERVAL_SECONDS = 900

/**
 * Normalizes an Automation to export format.
 * Strips internal fields (id, instanceId, sortOrder, enabled) and
 * omits intervalSeconds when it equals the default 900.
 */
export function toExportFormat(workflow: Automation): WorkflowExport {
  const trackerDomains = workflow.trackerDomains ?? []
  const trackerPattern = deriveTrackerPattern(trackerDomains, workflow.trackerPattern)

  const exported: WorkflowExport = {
    name: workflow.name,
    trackerPattern,
    trackerDomains,
    conditions: workflow.conditions,
  }

  // Only include intervalSeconds if it differs from default
  if (workflow.intervalSeconds && workflow.intervalSeconds !== DEFAULT_INTERVAL_SECONDS) {
    exported.intervalSeconds = workflow.intervalSeconds
  }

  if (workflow.dryRun) {
    exported.dryRun = true
  }

  return exported
}

/**
 * Derives the trackerPattern from trackerDomains.
 * If domains is empty and pattern is "*", returns "*".
 * Otherwise joins domains with comma.
 */
function deriveTrackerPattern(domains: string[], existingPattern?: string): string {
  if (domains.length === 0) {
    return existingPattern === "*" ? "*" : ""
  }
  return domains.join(",")
}

/**
 * Parses and normalizes import data to AutomationInput.
 * - trackerDomains is authoritative; trackerPattern is recomputed
 * - enabled is forced to false
 * - sortOrder is omitted (will be appended)
 * - name gets "(copy)" suffix via generateUniqueName
 */
export function fromImportFormat(
  data: WorkflowExport,
  existingNames: string[]
): AutomationInput {
  const trackerDomains = data.trackerDomains ?? []
  const trackerPattern = deriveTrackerPattern(trackerDomains, data.trackerPattern)

  const input: AutomationInput = {
    name: generateUniqueName(data.name, existingNames),
    trackerPattern,
    trackerDomains,
    conditions: data.conditions,
    enabled: false, // Always start disabled
    dryRun: data.dryRun ?? false,
  }

  // Include intervalSeconds if specified and differs from default
  if (data.intervalSeconds && data.intervalSeconds !== DEFAULT_INTERVAL_SECONDS) {
    input.intervalSeconds = data.intervalSeconds
  }

  return input
}

/**
 * Creates an AutomationInput for duplicating a workflow.
 * Similar to fromImportFormat but takes an existing Automation.
 */
export function toDuplicateInput(
  workflow: Automation,
  existingNames: string[]
): AutomationInput {
  const exportData = toExportFormat(workflow)
  return fromImportFormat(exportData, existingNames)
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

/**
 * Validates import JSON and returns either the parsed WorkflowExport or an error message.
 */
export function parseImportJSON(jsonString: string): { data: WorkflowExport; error: null } | { data: null; error: string } {
  let parsed: unknown
  try {
    parsed = JSON.parse(jsonString)
  } catch {
    return { data: null, error: "Invalid JSON format" }
  }

  if (typeof parsed !== "object" || parsed === null) {
    return { data: null, error: "Expected a JSON object" }
  }

  const obj = parsed as Record<string, unknown>

  // Validate required fields
  if (typeof obj.name !== "string" || obj.name.trim() === "") {
    return { data: null, error: "Missing or invalid 'name' field" }
  }

  if (typeof obj.conditions !== "object" || obj.conditions === null) {
    return { data: null, error: "Missing or invalid 'conditions' field" }
  }

  // Validate tracker fields
  const hasValidTrackerDomains = Array.isArray(obj.trackerDomains) &&
    obj.trackerDomains.every((el: unknown) => typeof el === "string")
  const hasValidTrackerPattern = typeof obj.trackerPattern === "string"

  if (!hasValidTrackerDomains && !hasValidTrackerPattern) {
    return { data: null, error: "Must specify either 'trackerDomains' (array of strings) or 'trackerPattern'" }
  }

  // Build the export data
  const data: WorkflowExport = {
    name: obj.name as string,
    trackerPattern: hasValidTrackerPattern ? (obj.trackerPattern as string) : "",
    trackerDomains: hasValidTrackerDomains ? (obj.trackerDomains as string[]) : [],
    conditions: obj.conditions as ActionConditions,
  }

  // Optional intervalSeconds
  if (typeof obj.intervalSeconds === "number" && obj.intervalSeconds >= 60) {
    data.intervalSeconds = obj.intervalSeconds
  }

  return { data, error: null }
}

/**
 * Serializes workflow export data to a formatted JSON string.
 */
export function toExportJSON(data: WorkflowExport): string {
  return JSON.stringify(data, null, 2)
}
