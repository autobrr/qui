/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useClientSetting } from "@/lib/client-settings"

const parseInstanceId = (raw: string): number | undefined => {
  const parsed = JSON.parse(raw)
  if (typeof parsed !== "number") throw new Error("invalid instance id")
  return parsed
}

// "" is the cleared sentinel; settings never remove their key.
const serializeInstanceId = (value: number | undefined): string =>
  typeof value === "number" ? JSON.stringify(value) : ""

export function usePersistedInstanceSelection(storageNamespace: string) {
  return useClientSetting<number | undefined>(`qui-selected-instance-${storageNamespace}`, {
    defaultValue: undefined,
    parse: parseInstanceId,
    serialize: serializeInstanceId,
  })
}
