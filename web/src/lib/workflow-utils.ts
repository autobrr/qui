/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import { BYTES_PER_UNIT } from "@/lib/unit-format"
import type { Automation, AutomationInput, ActionConditions, FreeSpaceSource, RuleCondition, SortingConfig } from "@/types"
import type { QueryClient } from "@tanstack/react-query"

// i18n-namespace: instances

export type TrackerMatchMode = "include" | "exclude" | "mixed"

export type ActionType = "speedLimits" | "shareLimits" | "pause" | "resume" | "recheck" | "reannounce" | "autoManagement" | "delete" | "tag" | "category" | "move" | "externalProgram" | "exportToInstance" | "transferToInstance"

export const ACTION_LABEL_KEYS: Record<ActionType, string> = {
  speedLimits: "preferences.workflowDialog.actions.speedLimits",
  shareLimits: "preferences.workflowDialog.actions.shareLimits",
  pause: "preferences.workflowDialog.actions.pause",
  resume: "preferences.workflowDialog.actions.resume",
  recheck: "preferences.workflowDialog.actions.recheck",
  reannounce: "preferences.workflowDialog.actions.reannounce",
  autoManagement: "preferences.workflowDialog.actions.autoManagement",
  delete: "preferences.workflowDialog.actions.delete",
  tag: "preferences.workflowDialog.actions.tag",
  category: "preferences.workflowDialog.actions.category",
  move: "preferences.workflowDialog.actions.move",
  externalProgram: "preferences.workflowDialog.actions.externalProgram",
  exportToInstance: "preferences.workflowDialog.actions.exportToInstance",
  transferToInstance: "preferences.workflowDialog.actions.transferToInstance",
}

// The backend reads the same way: the tags list wins over the legacy single tag.
export function getRuleTagActions(rule: Automation) {
  if (rule.conditions?.tags && rule.conditions.tags.length > 0) {
    return rule.conditions.tags
  }
  if (rule.conditions?.tag) {
    return [rule.conditions.tag]
  }
  return []
}

/** Returns the actions that a saved workflow has turned on, in the order of the action picker. */
export function getRuleActions(rule: Automation): ActionType[] {
  return (Object.keys(ACTION_LABEL_KEYS) as ActionType[]).filter(action => {
    if (action === "tag") {
      return getRuleTagActions(rule).some(tag => tag.enabled)
    }
    if (action === "autoManagement") {
      // enabled is the value that the action sets, so a saved action with enabled false still runs.
      return rule.conditions?.autoManagement != null
    }
    return Boolean(rule.conditions?.[action]?.enabled)
  })
}

/** The actions of a saved workflow that are switched off, each as stored. tags holds only the entries that are switched off. */
export type SwitchedOffActions = Omit<ActionConditions, "schemaVersion" | "grouping" | "tag" | "autoManagement">

/** Returns the actions that a saved workflow has switched off, as stored. autoManagement never counts: getRuleActions always runs it. */
export function getSwitchedOffActions(rule: Automation): SwitchedOffActions {
  const running = getRuleActions(rule)
  const actions = (Object.keys(ACTION_LABEL_KEYS) as ActionType[])
    .filter(action => action !== "tag" && !running.includes(action) && rule.conditions?.[action] != null)
    .map(action => [action, rule.conditions[action]])
  const switchedOff: SwitchedOffActions = Object.fromEntries(actions)
  const tags = getRuleTagActions(rule).filter(tag => !tag.enabled)
  if (tags.length > 0) {
    switchedOff.tags = tags
  }
  return switchedOff
}

/** Returns the condition of each action that a saved workflow has turned on, null for an action without one. */
function runningActionConditions(rule: Automation): (RuleCondition | null)[] {
  return getRuleActions(rule).flatMap(action => {
    const entries = action === "tag" ? getRuleTagActions(rule).filter(tag => tag.enabled) : [rule.conditions[action]]
    return entries.map(entry => entry?.condition ?? null)
  })
}

/** Returns the condition that the rule editor shows: the first condition of an action that the workflow has turned on. */
export function firstActionCondition(rule: Automation): RuleCondition | null {
  return runningActionConditions(rule).find(condition => condition != null) ?? null
}

/** Reports whether a save from the rule editor changes the condition of an action that the rule turns on. A save writes the shown condition onto every action. */
export function hasMixedActionConditions(rule: Automation): boolean {
  const shown = JSON.stringify(firstActionCondition(rule))
  return runningActionConditions(rule).some(condition => JSON.stringify(condition) !== shown)
}

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

// The order one automation run applies actions in; a test holds it equal to the backend's actionRunOrder.
export const ACTION_RUN_ORDER = [
  "speedLimits",
  "shareLimits",
  "pause",
  "resume",
  "recheck",
  "reannounce",
  "autoManagement",
  "tag",
  "category",
  "move",
  "externalProgram",
  "exportToInstance",
  "delete",
  "transferToInstance",
] as const satisfies readonly ActionType[]

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

/** Returns the rule names of an instance. Reads the server when the rule list is not loaded, and rejects when that read fails. */
export async function loadWorkflowNames(queryClient: QueryClient, instanceId: number): Promise<string[]> {
  const rules = await queryClient.ensureQueryData({
    queryKey: ["automations", instanceId],
    queryFn: () => api.listAutomations(instanceId),
  })
  return (rules ?? []).map(rule => rule.name)
}

/**
 * Create payload for import: the JSON as written, except that
 * - enabled is forced to false
 * - sortOrder is omitted, so the backend appends the rule
 * - a name that the instance already has gets a copy suffix via generateUniqueName
 */
export function fromImportFormat(
  data: WorkflowImport,
  existingNames: string[]
): AutomationInput {
  const nameTaken = existingNames.some(name => name.toLowerCase() === data.name.toLowerCase())
  return {
    ...data,
    name: nameTaken ? generateUniqueName(data.name, existingNames) : data.name,
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

/** Create payload for "Duplicate" on the same instance: an import of the exported rule that always gets a copy suffix. */
export function toDuplicateInput(
  workflow: Automation,
  existingNames: string[]
): AutomationInput {
  return {
    ...fromImportFormat({ ...toExportFormat(workflow) }, existingNames),
    name: generateUniqueName(workflow.name, existingNames),
  }
}

/** Create payload for "Copy to instance": an import of the exported rule on the target instance. */
export function toCopyInput(
  workflow: Automation,
  targetNames: string[]
): AutomationInput {
  return fromImportFormat({ ...toExportFormat(workflow) }, targetNames)
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

/** The Free space source fields of the workflow dialog. `allowanceUnit` is a byte multiplier. */
export interface FreeSpaceSourceInput {
  type: FreeSpaceSource["type"]
  path: string
  allowance: string
  allowanceUnit: number
}

const ALLOWANCE_DECIMALS = 3

/** The units of a plan that counts in steps of 1000, as most providers do. */
export const ALLOWANCE_DECIMAL_UNITS = { MB: 1000 ** 2, GB: 1000 ** 3, TB: 1000 ** 4 } as const

function allowanceBytesOf(allowance: string, unit: number): number {
  return Math.round((parseFloat(allowance) || 0) * unit)
}

/** The `freeSpaceSource` of a rule payload. Undefined means the default source, also for a source that is not complete. */
export function freeSpaceSourceOf(input: FreeSpaceSourceInput): FreeSpaceSource | undefined {
  const path = input.path.trim()
  if (input.type === "qbittorrent" || path === "") return undefined
  if (input.type === "path" || input.type === "qbitPath") return { type: input.type, path }
  const allowanceBytes = allowanceBytesOf(input.allowance, input.allowanceUnit)
  return allowanceBytes > 0 ? { type: "allowance", path, allowanceBytes } : undefined
}

/** The figure and unit that show a saved allowance without a change to its bytes, so a save of an untouched rule keeps the figure. */
export function allowanceInputOf(allowanceBytes: number): Pick<FreeSpaceSourceInput, "allowance" | "allowanceUnit"> {
  const { TB, GB, MB } = ALLOWANCE_DECIMAL_UNITS
  for (const allowanceUnit of [BYTES_PER_UNIT.TiB, BYTES_PER_UNIT.GiB, BYTES_PER_UNIT.MiB, TB, GB, MB]) {
    const allowance = String(Number((allowanceBytes / allowanceUnit).toFixed(ALLOWANCE_DECIMALS)))
    // A figure below 1 reads better in the next unit down: 500 GB, not 0.5 TB.
    if (Number(allowance) >= 1 && allowanceBytesOf(allowance, allowanceUnit) === allowanceBytes) {
      return { allowance, allowanceUnit }
    }
  }
  // No short figure holds these bytes, for example a value set through the API.
  // A division by a power of two is exact, so the full MiB figure is.
  return { allowance: String(allowanceBytes / BYTES_PER_UNIT.MiB), allowanceUnit: BYTES_PER_UNIT.MiB }
}
