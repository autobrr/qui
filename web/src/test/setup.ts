/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup } from "@testing-library/react"
import { afterAll, afterEach, beforeAll } from "vitest"

import { server } from "@/test/msw/server"

if (globalThis.localStorage == null) {
  const store = new Map<string, string>()
  Object.assign(Storage.prototype, {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => void store.set(key, value),
    removeItem: (key: string) => void store.delete(key),
    clear: () => store.clear(),
    key: (index: number) => [...store.keys()][index] ?? null,
  })
  Object.defineProperty(Storage.prototype, "length", {
    get: () => store.size,
    configurable: true,
  })
  globalThis.localStorage = Object.create(Storage.prototype) as Storage
}

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
}

beforeAll(() => {
  server.listen({ onUnhandledFrame: "error" })
})

afterEach(() => {
  cleanup()
  server.resetHandlers()
})

afterAll(() => {
  server.close()
})
