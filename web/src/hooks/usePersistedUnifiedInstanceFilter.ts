/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { encodeUnifiedInstanceIds, parseUnifiedInstanceIds } from "@/lib/instances"
import { useClientSetting } from "@/lib/client-settings"

const NO_IDS: readonly number[] = []

// An empty selection stores "" (the cleared sentinel); parse never sees it.
const parseIds = (raw: string): readonly number[] => parseUnifiedInstanceIds(raw)
const serializeIds = (ids: readonly number[]): string => encodeUnifiedInstanceIds(ids) ?? ""

export function usePersistedUnifiedInstanceFilter(): [
  readonly number[],
  (ids: readonly number[]) => void
] {
  return useClientSetting<readonly number[]>("qui-unified-instance-filter", {
    defaultValue: NO_IDS,
    parse: parseIds,
    serialize: serializeIds,
  })
}
