/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { SystemNotice } from "@/types"

export const systemNoticeStyles = {
  error: {
    box: "border-red-200 bg-red-50 dark:border-red-800 dark:bg-red-950/50",
    icon: "text-red-600 dark:text-red-400",
    strong: "text-red-800 dark:text-red-200",
    body: "text-red-700 dark:text-red-300",
    dot: "bg-red-500",
  },
  warning: {
    box: "border-yellow-200 bg-yellow-50 dark:border-yellow-800 dark:bg-yellow-950/50",
    icon: "text-yellow-600 dark:text-yellow-400",
    strong: "text-yellow-800 dark:text-yellow-200",
    body: "text-yellow-700 dark:text-yellow-300",
    dot: "bg-yellow-500",
  },
} as const

/** The dot color for a set of open notices: red when one of them is an error. */
export function systemNoticeDot(notices: SystemNotice[]): string {
  return notices.some((notice) => notice.severity === "error") ? systemNoticeStyles.error.dot : systemNoticeStyles.warning.dot
}
