/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// i18n-namespace: crossseed

/** What the run controls of a section show. A blocked state carries the reason for the disabled button, and a running state can name the active run. */
export type RunState =
  | { kind: "ready" }
  | { kind: "blocked"; reason: string }
  | { kind: "running"; note?: string }
  | { kind: "cancelling" }

type Translate = (key: string, options?: Record<string, unknown>) => string

export interface RssRunInput {
  hasEnabledIndexers: boolean
  hasTargets: boolean
  running: boolean
  cancelPending: boolean
  startPending: boolean
  cooldown: { active: boolean; minutes: number; remaining: string }
}

/** A run in progress wins over every reason, so Cancel stays reachable. */
export function rssRunState(input: RssRunInput, t: Translate): RunState {
  if (input.running) return input.cancelPending ? { kind: "cancelling" } : { kind: "running" }
  if (!input.hasEnabledIndexers) return { kind: "blocked", reason: t("automation.requiresTorznabIndexer") }
  if (!input.hasTargets) return { kind: "blocked", reason: t("toast.selectAtLeastOneInstance") }
  if (input.cooldown.active) {
    return { kind: "blocked", reason: t("automation.runDisabledCooldown", { minutes: input.cooldown.minutes, remaining: input.cooldown.remaining }) }
  }
  if (input.startPending) return { kind: "blocked", reason: t("run.starting") }
  return { kind: "ready" }
}

export interface LibraryRunInput {
  hasInstance: boolean
  startPending: boolean
  running: boolean
  cancelPending: boolean
  /** Torznab is off for the scan and Gazelle is not set up. */
  needsGazelle: boolean
  /** An enabled Torznab indexer or a saved Gazelle setup exists. */
  hasSearchSource: boolean
  /** The active run is a scheduled run. */
  scheduledRun: boolean
}

/** A scan in progress wins over every reason, so Cancel stays reachable. */
export function libraryRunState(input: LibraryRunInput, t: Translate): RunState {
  if (input.running) {
    if (input.cancelPending) return { kind: "cancelling" }
    return input.scheduledRun ? { kind: "running", note: t("scan.schedule.scheduledRunActive") } : { kind: "running" }
  }
  if (!input.hasInstance) return { kind: "blocked", reason: t("toast.selectInstanceToRun") }
  if (input.startPending) return { kind: "blocked", reason: t("run.starting") }
  if (input.needsGazelle) return { kind: "blocked", reason: t("toast.enableGazelleDescription") }
  if (!input.hasSearchSource) return { kind: "blocked", reason: t("toast.configureTorznabOrGazelle") }
  return { kind: "ready" }
}
