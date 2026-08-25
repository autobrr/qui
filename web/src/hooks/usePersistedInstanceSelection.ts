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

/**
 * The selection an instance picker should hold given the current instance
 * list. Keeps the saved selection while the list is still loading (undefined).
 */
export function resolveInstanceSelection(
  instances: Array<{ id: number; connected: boolean }> | undefined,
  selected: number | undefined
): number | undefined {
  if (!instances) return selected
  if (instances.length === 0) return undefined
  if (selected !== undefined && instances.some((i) => i.id === selected)) return selected
  const fallback = instances.find((i) => i.connected) ?? instances[0]
  return fallback.id
}
