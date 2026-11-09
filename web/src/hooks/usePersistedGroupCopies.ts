/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback } from "react"

import { parseJsonBoolean, useClientSetting } from "@/lib/client-settings"
import { isDemo } from "@/lib/demo"

/**
 * The Group copies switch of the torrent list. One setting for both Layouts and
 * every row style; off by default. Always off in the demo, which has no matcher.
 */
export function usePersistedGroupCopies(): [boolean, () => void] {
  const [groupCopies, setGroupCopies] = useClientSetting<boolean>("qui-group-copies", { defaultValue: false, parse: parseJsonBoolean })
  const toggle = useCallback(() => setGroupCopies((current) => !current), [setGroupCopies])
  return [!isDemo && groupCopies, toggle]
}
