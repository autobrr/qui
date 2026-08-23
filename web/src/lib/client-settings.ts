/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useMemo, useRef, useSyncExternalStore } from "react"
import { getApiBaseUrl } from "@/lib/base-url"

/**
 * DB-backed client settings (issue #2406).
 *
 * localStorage stays as the instant-boot cache; the server's client_settings
 * table is the source of truth. Every write goes to localStorage immediately
 * and is queued for a debounced bulk PUT. useClientSettingsSync pulls the
 * server state, applies it locally, seeds missing keys from localStorage and
 * opens the push gate.
 *
 * Values are raw strings end to end; each setting keeps the exact encoding it
 * always had in localStorage, and the backend never parses them. A cleared
 * setting stores "" instead of removing the key, so "absent on the server"
 * always means "never synced".
 */

const CHANGE_EVENT = "qui-client-setting-changed"
const FLUSH_DELAY_MS = 800

// Keys synced to the server. Grown as hooks convert; keys not listed here
// (theme boot caches, dismissed banners, sessionStorage) stay local-only.
const SYNCED_KEYS = new Set<string>([
  "qui-delete-files-default",
  "qui-delete-files-lock",
  "qui-speed-units",
  "qui-incognito-mode",
  "qui-datetime-preferences",
  "qui-titlebar-speeds-enabled",
  "qui.language",
])
const SYNCED_PREFIXES = [
  "qui-start-paused-instance-",
  "qui-cross-seed-blocklist-",
]

function isSyncedKey(key: string): boolean {
  return SYNCED_KEYS.has(key) || SYNCED_PREFIXES.some((prefix) => key.startsWith(prefix))
}

export function readRaw(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

function writeLocal(key: string, raw: string): boolean {
  try {
    localStorage.setItem(key, raw)
  } catch (error) {
    console.error("Failed to write client setting to localStorage:", error)
    return false
  }
  window.dispatchEvent(new CustomEvent(CHANGE_EVENT, { detail: { key } }))
  return true
}

/**
 * Store a setting locally and queue it for the server. No-op when the value
 * is unchanged, which also breaks server-apply echo loops.
 */
export function writeRaw(key: string, raw: string): void {
  if (readRaw(key) === raw) return
  if (writeLocal(key, raw)) enqueuePush(key, raw)
}

// --- push queue ---

const pending = new Map<string, string>()
let flushTimer: ReturnType<typeof setTimeout> | null = null
// Opens after the first successful GET, so nothing fires while logged out.
let syncReady = false

function enqueuePush(key: string, raw: string): void {
  pending.set(key, raw)
  scheduleFlush()
}

function scheduleFlush(): void {
  if (!syncReady || pending.size === 0) return
  if (flushTimer !== null) clearTimeout(flushTimer)
  flushTimer = setTimeout(() => {
    flushTimer = null
    void flushPending()
  }, FLUSH_DELAY_MS)
}

let flushInFlight = false
// A timer or tab-hide flush that fired while a PUT was in flight; replayed
// once the PUT settles so the write it carried is not stalled.
let flushRequestedInFlight = false

async function flushPending(): Promise<void> {
  if (flushInFlight) {
    flushRequestedInFlight = true
    return
  }
  if (!syncReady || pending.size === 0) return
  // Keys stay in pending while the PUT is in flight so a concurrent server
  // apply cannot clobber the newer local value (the echo guard checks
  // pending). On failure they simply stay queued for the next flush.
  const batch = Object.fromEntries(pending)
  flushInFlight = true
  try {
    // Plain fetch instead of the api client: keepalive lets the tab-hidden
    // flush finish, and this module must stay import-light (i18n boots on it).
    const response = await fetch(`${getApiBaseUrl()}/client-settings`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(batch),
      keepalive: true,
    })
    if (!response.ok) throw new Error(`HTTP ${response.status}`)
    // Drop what this PUT delivered. Entries replaced mid-flight stay queued;
    // their own scheduleFlush timer (or the replay below) picks them up.
    for (const [key, raw] of Object.entries(batch)) {
      if (pending.get(key) === raw) pending.delete(key)
    }
  } catch (error) {
    console.error("Failed to push client settings:", error)
  } finally {
    flushInFlight = false
    if (flushRequestedInFlight) {
      flushRequestedInFlight = false
      scheduleFlush()
    }
  }
}

if (typeof document !== "undefined") {
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState !== "hidden") return
    if (flushTimer !== null) {
      clearTimeout(flushTimer)
      flushTimer = null
    }
    void flushPending()
  })
}

// --- server sync (called by useClientSettingsSync) ---

/**
 * Apply a server snapshot to localStorage. Keys with a pending local push and
 * keys already equal are skipped. Returns the keys that changed.
 */
export function applyServerSettings(settings: Record<string, string>): string[] {
  const changed: string[] = []
  for (const [key, raw] of Object.entries(settings)) {
    if (pending.has(key)) continue
    if (readRaw(key) === raw) continue
    if (writeLocal(key, raw)) changed.push(key)
  }
  return changed
}

/**
 * Queue every synced localStorage key the server does not know yet, then open
 * the push gate. Idempotent: after one push the server has the key.
 */
export function seedAndMarkReady(serverSettings: Record<string, string>): void {
  try {
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i)
      if (!key || !isSyncedKey(key) || key in serverSettings || pending.has(key)) continue
      const raw = localStorage.getItem(key)
      if (raw !== null) pending.set(key, raw)
    }
  } catch (error) {
    console.error("Failed to scan localStorage for client settings seed:", error)
  }
  syncReady = true
  scheduleFlush()
}

// Test-only: reset module state between vitest cases.
export function _resetClientSettingsForTests(): void {
  pending.clear()
  if (flushTimer !== null) clearTimeout(flushTimer)
  flushTimer = null
  flushInFlight = false
  flushRequestedInFlight = false
  syncReady = false
}

// --- React hook ---

/** JSON-parse a raw value, accepting only a real boolean. */
export function parseJsonBoolean(raw: string): boolean {
  const parsed = JSON.parse(raw)
  if (typeof parsed !== "boolean") throw new Error("not a boolean")
  return parsed
}

interface ClientSettingOptions<T> {
  defaultValue: T
  /** Raw string to value; throw to fall back to defaultValue. Pass a stable function. */
  parse: (raw: string) => T
  /** Value to raw string; defaults to JSON.stringify. Pass a stable function. */
  serialize?: (value: T) => string
}

/**
 * One DB-backed setting as React state. All hook instances for a key stay in
 * sync in the same tab (change event) and across tabs (storage event); writes
 * persist to localStorage and queue a debounced server push.
 */
export function useClientSetting<T>(
  key: string,
  { defaultValue, parse, serialize = (value) => JSON.stringify(value) }: ClientSettingOptions<T>
): [T, (value: T | ((prev: T) => T)) => void] {
  const subscribe = useCallback(
    (callback: () => void) => {
      // A storage event without a key is localStorage.clear() (or a test's
      // bare Event); re-read for those too.
      const onStorage = (e: StorageEvent) => {
        if (e.key == null || e.key === key) callback()
      }
      const onChange = (e: Event) => {
        if ((e as CustomEvent<{ key?: string }>).detail?.key === key) callback()
      }
      window.addEventListener("storage", onStorage)
      window.addEventListener(CHANGE_EVENT, onChange)
      return () => {
        window.removeEventListener("storage", onStorage)
        window.removeEventListener(CHANGE_EVENT, onChange)
      }
    },
    [key]
  )

  const raw = useSyncExternalStore(subscribe, () => readRaw(key))

  const value = useMemo(() => {
    // "" is the cleared sentinel (settings never remove their key).
    if (raw == null || raw === "") return defaultValue
    try {
      return parse(raw)
    } catch {
      return defaultValue
    }
  }, [raw, parse, defaultValue])

  const valueRef = useRef(value)
  valueRef.current = value

  const set = useCallback(
    (next: T | ((prev: T) => T)) => {
      const resolved = typeof next === "function" ? (next as (prev: T) => T)(valueRef.current) : next
      writeRaw(key, serialize(resolved))
    },
    [key, serialize]
  )

  return [value, set]
}
