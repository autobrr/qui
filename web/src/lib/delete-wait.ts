/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { AutomationPreviewResult, RuleCondition } from "@/types"

export const DEFAULT_DELETE_WAIT_SECONDS = 7 * 24 * 3600
export const MAX_DELETE_WAIT_SECONDS = 365 * 24 * 3600

export function conditionReadsVerdict(condition: RuleCondition | null | undefined): boolean {
  if (!condition) return false
  if (condition.field === "IS_UNREGISTERED") return true
  if (condition.field === "STATE" && condition.value?.trim().toLowerCase() === "unregistered") return true
  return condition.conditions?.some(conditionReadsVerdict) ?? false
}

export function previewRowCount(preview: Pick<AutomationPreviewResult, "totalMatches" | "waitingCount">): number {
  return preview.totalMatches + (preview.waitingCount ?? 0)
}
