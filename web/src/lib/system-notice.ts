/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { LinkProps } from "@tanstack/react-router"

import type { SystemNotice } from "@/types"

export type SystemNoticeRoute = Pick<LinkProps, "to" | "search">

// The page where the user fixes each kind of System notice. A kind with no
// entry shows no action button. Its label is systemNotice.kinds.<kind>.action.
export const systemNoticeRoutes: Partial<Record<string, SystemNoticeRoute>> = {
  redacted_key_rejected: { to: "/settings", search: { tab: "indexers" } },
  orpheus_key_rejected: { to: "/settings", search: { tab: "indexers" } },
  low_disk: { to: "/settings", search: { tab: "statistics" } },
}

const freshMs = 10 * 60 * 1000

export function isFreshSystemNotice(openedAt: string, now: number): boolean {
  return now - Date.parse(openedAt) < freshMs
}

type Severity = SystemNotice["severity"]

// The dot of an error is red, and the dot of a warning is yellow.
export const systemNoticeDotColor: Record<Severity, { fill: string; ring: string }> = {
  error: { fill: "bg-destructive", ring: "ring-destructive/20" },
  warning: { fill: "bg-yellow-500", ring: "ring-yellow-500/20" },
}

/** The severity of the dot on a menu button: red when one of the notices is an error. */
export function topSystemNoticeSeverity(notices: SystemNotice[]): Severity {
  return notices.some((notice) => notice.severity === "error") ? "error" : "warning"
}
