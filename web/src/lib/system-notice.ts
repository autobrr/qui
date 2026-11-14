/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { LinkProps } from "@tanstack/react-router"

export type SystemNoticeRoute = Pick<LinkProps, "to" | "search">

// The page where the user fixes each kind of System notice. A kind with no
// entry shows no action button. Its label is systemNotice.kinds.<kind>.action.
export const systemNoticeRoutes: Partial<Record<string, SystemNoticeRoute>> = {}

const freshMs = 10 * 60 * 1000

export function isFreshSystemNotice(openedAt: string, now: number): boolean {
  return now - Date.parse(openedAt) < freshMs
}
