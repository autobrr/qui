/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { LinkProps } from "@tanstack/react-router"

import type { SystemNotice } from "@/types"

export type SystemNoticeRoute = Pick<LinkProps, "to" | "search">

export const systemNoticeRoutes: Partial<Record<string, SystemNoticeRoute>> = {
  redacted_key_rejected: { to: "/settings", search: { tab: "indexers" } },
  orpheus_key_rejected: { to: "/settings", search: { tab: "indexers" } },
  low_disk: { to: "/settings", search: { tab: "statistics" } },
  library_scan_schedule_blocked: { to: "/cross-seed", search: { tab: "library" } },
}

const freshMs = 10 * 60 * 1000

export function isFreshSystemNotice(openedAt: string, now: number): boolean {
  return now - Date.parse(openedAt) < freshMs
}

type Severity = SystemNotice["severity"]

export const systemNoticeDotColor: Record<Severity, { fill: string; ring: string }> = {
  error: { fill: "bg-destructive", ring: "ring-destructive/20" },
  warning: { fill: "bg-yellow-500", ring: "ring-yellow-500/20" },
}

export function topSystemNoticeSeverity(notices: SystemNotice[]): Severity {
  return notices.some((notice) => notice.severity === "error") ? "error" : "warning"
}
