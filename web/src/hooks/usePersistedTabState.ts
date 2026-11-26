/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useClientSetting } from "@/lib/client-settings"

export function usePersistedTabState<T extends string>(
  storageKey: string,
  defaultValue: T,
  isValid?: (value: string) => value is T
) {
  const parse = (raw: string): T => {
    if (!isValid || isValid(raw)) return raw as T
    throw new Error("invalid tab value")
  }

  return useClientSetting<T>(storageKey, { defaultValue, parse, serialize: String })
}
