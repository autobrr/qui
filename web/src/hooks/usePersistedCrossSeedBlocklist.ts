/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, type Dispatch, type SetStateAction } from "react"

import { parseJsonBoolean, useClientSetting } from "@/lib/client-settings"

export function usePersistedCrossSeedBlocklist(instanceId: number, defaultValue: boolean = false) {
  const persisted = useClientSetting<boolean>(`qui-cross-seed-blocklist-${instanceId}`, {
    defaultValue,
    parse: parseJsonBoolean,
  })
  // Non-positive ids (the all-instances view passes -1) never persisted this
  // choice; keep it session-local there.
  const local = useState<boolean>(defaultValue)
  const blockCrossSeeds = instanceId > 0 ? persisted[0] : local[0]
  const setBlockCrossSeeds: Dispatch<SetStateAction<boolean>> = instanceId > 0 ? persisted[1] : local[1]

  return {
    blockCrossSeeds,
    setBlockCrossSeeds,
  } as const
}
