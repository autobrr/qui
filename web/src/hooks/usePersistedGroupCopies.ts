/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback } from "react"

import { parseJsonBoolean, useClientSetting } from "@/lib/client-settings"

/** The Group copies switch of the desktop torrent list. One setting for every row style; off by default. */
export function usePersistedGroupCopies(): [boolean, () => void] {
  const [groupCopies, setGroupCopies] = useClientSetting<boolean>("qui-group-copies", { defaultValue: false, parse: parseJsonBoolean })
  const toggle = useCallback(() => setGroupCopies((current) => !current), [setGroupCopies])
  return [groupCopies, toggle]
}
