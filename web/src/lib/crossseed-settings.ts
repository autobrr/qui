/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { CrossSeedAutomationSettingsPatch } from "@/types/crossseed"

/** Builds the settings fields shared by pooled completion and its byte budget. */
export function buildPooledCompletionPatch(
  pooledPartialCompletionEnabled: boolean,
  autoResumeMaxDownloadMb: number
): Pick<CrossSeedAutomationSettingsPatch, "pooledPartialCompletionEnabled" | "autoResumeMaxDownloadMb"> {
  return { pooledPartialCompletionEnabled, autoResumeMaxDownloadMb }
}
