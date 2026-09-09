/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterAll, afterEach, beforeAll } from "vitest"

import { server } from "@/test/msw/server"

// Node >=26 ships a global localStorage stub that shadows jsdom's and reports
// `undefined` unless the process runs with --localstorage-file. Back it with an
// in-memory store instead so no local flag is needed. The implementation goes
// on Storage.prototype (not the instance) so `vi.spyOn(Storage.prototype, ...)`
// in tests still intercepts calls.
if (globalThis.localStorage == null) {
  const store = new Map<string, string>()
  Object.assign(Storage.prototype, {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => void store.set(key, value),
    removeItem: (key: string) => void store.delete(key),
    clear: () => store.clear(),
    key: (index: number) => [...store.keys()][index] ?? null,
  })
  // jsdom's brand-checked `length` accessor throws on this stand-in instance;
  // replace it with the store's size so key/length iteration works.
  Object.defineProperty(Storage.prototype, "length", {
    get: () => store.size,
    configurable: true,
  })
  globalThis.localStorage = Object.create(Storage.prototype) as Storage
}

// Global MSW lifecycle for the vitest suite. `onUnhandledRequest: "error"`
// makes any un-stubbed request fail loudly so missing handlers surface as test
// failures instead of silent network calls. Hooks are imported explicitly
// because vitest runs with `globals: false`.
beforeAll(() => {
  server.listen({ onUnhandledRequest: "error" })
})

afterEach(() => {
  server.resetHandlers()
})

afterAll(() => {
  server.close()
})
