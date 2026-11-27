/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ViewMode } from "@/hooks/usePersistedCompactViewState"

export function viewModeRowHeight(desktopViewMode: ViewMode): number {
  switch (desktopViewMode) {
    case "compact": return 80
    case "dense": return 26
    default: return 40
  }
}
