import { describe, expect, it } from "vitest"

import type { StreamState } from "@/contexts/SyncStreamContext"
import { isStreamUsable } from "./sync-stream-state"

describe("isStreamUsable", () => {
  const healthy: StreamState = {
    connected: true,
    initialized: true,
    dataStalled: false,
    error: null,
    retrying: false,
    retryAttempt: 0,
  }

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
