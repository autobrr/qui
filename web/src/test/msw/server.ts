/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { setupServer } from "msw/node"

import { handlers } from "@/test/msw/handlers"

// Shared MSW node server for the vitest suite. Lifecycle (listen / reset /
// close) is wired in src/test/setup.ts; individual tests extend it via
// server.use(...) for per-case payload or error overrides.
export const server = setupServer(...handlers)
