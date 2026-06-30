/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterAll, afterEach, beforeAll } from "vitest"

import { server } from "@/test/msw/server"

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
