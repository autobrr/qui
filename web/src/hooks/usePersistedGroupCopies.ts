/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback } from "react"

import { parseJsonBoolean, readClientSetting, useClientSetting } from "@/lib/client-settings"
import { isDemo } from "@/lib/demo"

const GROUP_COPIES_KEY = "qui-group-copies"
const GROUP_COPIES_SETTING = { defaultValue: false, parse: parseJsonBoolean }

/**
 * The Group copies switch of the torrent list. One setting for both Layouts and
 * every row style; off by default. Always off in the demo, which has no matcher.
 */
export function usePersistedGroupCopies(): [boolean, () => void] {
  const [groupCopies, setGroupCopies] = useClientSetting<boolean>(GROUP_COPIES_KEY, GROUP_COPIES_SETTING)
  const toggle = useCallback(() => setGroupCopies((current) => !current), [setGroupCopies])
  return [!isDemo && groupCopies, toggle]
}

/** The Group copies switch, for code outside a component. */
export function readGroupCopies(): boolean {
  return !isDemo && readClientSetting(GROUP_COPIES_KEY, GROUP_COPIES_SETTING)
}
