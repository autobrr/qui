/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useState, type SetStateAction } from "react"

import { parseJsonBoolean, readRaw, useClientSetting, writeRaw } from "@/lib/client-settings"

const PREF_KEY = "qui-delete-files-default"
const LOCK_KEY = "qui-delete-files-lock"

export function usePersistedDeleteFiles(defaultValue: boolean = false) {
  const [storedValue, setStoredValue] = useClientSetting<boolean>(PREF_KEY, {
    defaultValue,
    parse: parseJsonBoolean,
  })
  const [lockRaw, setLockRaw] = useClientSetting<string>(LOCK_KEY, {
    defaultValue: "",
    parse: String,
    serialize: String,
  })
  const [localValue, setLocalValue] = useState<boolean>(defaultValue)

  const isLocked = lockRaw !== "" ? lockRaw === "true" : (readRaw(PREF_KEY) ?? "") !== ""

  const deleteFiles = isLocked ? storedValue : localValue

  const setDeleteFiles = useCallback(
    (value: SetStateAction<boolean>) => {
      if (isLocked) {
        setStoredValue(value)
      } else {
        setLocalValue(value)
      }
    },
    [isLocked, setStoredValue]
  )

  const toggleLock = useCallback(() => {
    if (isLocked) {
      setLocalValue(storedValue)
      setLockRaw("false")
      writeRaw(PREF_KEY, "")
    } else {
      setStoredValue(localValue)
      setLockRaw("true")
    }
  }, [isLocked, storedValue, localValue, setStoredValue, setLockRaw])

  return {
    deleteFiles,
    setDeleteFiles,
    isLocked,
    toggleLock,
  } as const
}
