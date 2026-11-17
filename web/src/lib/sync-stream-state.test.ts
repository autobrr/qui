/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import type { StreamState } from "@/contexts/SyncStreamContext"
import {
  isStreamUsable,
  STREAM_ERROR_DISCONNECTED,
  streamFallbackPollInterval
} from "./sync-stream-state"

const healthy: StreamState = {
  connected: true,
  initialized: true,
  dataStalled: false,
  error: null,
  retrying: false,
  retryAttempt: 0,
}

describe("isStreamUsable", () => {
  it.each([
    ["healthy", {}, true],
    ["disconnected", { connected: false }, false],
    ["uninitialized", { initialized: false }, false],
    ["stalled", { dataStalled: true }, false],
    ["failed", { error: "client:disconnected" }, false],
  ] as const)("%s", (_name, overrides, expected) => {
    expect(isStreamUsable({ ...healthy, ...overrides })).toBe(expected)
  })
})

describe("streamFallbackPollInterval", () => {
  it.each([
    ["never connected", { connected: false, initialized: false }, 3000],
    ["stalled", { dataStalled: true }, 30000],
    ["server error", { connected: false, error: "instance unreachable: connection refused" }, false],
    ["disconnected", { connected: false, error: STREAM_ERROR_DISCONNECTED }, 3000],
  ] as const)("%s", (_name, overrides, expected) => {
    expect(streamFallbackPollInterval({ ...healthy, ...overrides }, 3000)).toBe(expected)
  })
})
