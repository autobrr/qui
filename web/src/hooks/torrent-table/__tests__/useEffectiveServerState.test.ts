/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffectiveServerState } from "@/hooks/torrent-table/useEffectiveServerState"
import { makeServerState } from "@/test/mockServerState"
import type { ServerState } from "@/types"
import { renderHook } from "@testing-library/react"
import { describe, expect, it } from "vitest"

type Props = { instanceId: number; serverState: ServerState | null | undefined }

function renderEffectiveServerState(initialProps: Props) {
  return renderHook((props: Props) => useEffectiveServerState(props), { initialProps })
}

describe("useEffectiveServerState", () => {
  it("returns the live server state for the current instance", () => {
    const state = makeServerState({ connection_status: "connected" })
    const { result } = renderEffectiveServerState({ instanceId: 1, serverState: state })
    expect(result.current).toBe(state)
  })

  it("returns null when serverState is explicitly null", () => {
    const { result } = renderEffectiveServerState({ instanceId: 1, serverState: null })
    expect(result.current).toBeNull()
  })

  it("returns the cached state when serverState goes missing for the same instance", () => {
    const state = makeServerState()
    const { result, rerender } = renderEffectiveServerState({ instanceId: 1, serverState: state })
    expect(result.current).toBe(state)

    rerender({ instanceId: 1, serverState: undefined })
    expect(result.current).toBe(state)
  })

  it("clears to null when the instance changes while serverState is missing", () => {
    const state = makeServerState()
    const { result, rerender } = renderEffectiveServerState({ instanceId: 1, serverState: state })
    expect(result.current).toBe(state)

    rerender({ instanceId: 2, serverState: undefined })
    expect(result.current).toBeNull()
  })

  it("replaces the cached state when a newer server state arrives", () => {
    const first = makeServerState({ dl_info_speed: 100 })
    const second = makeServerState({ dl_info_speed: 200 })
    const { result, rerender } = renderEffectiveServerState({ instanceId: 1, serverState: first })
    expect(result.current).toBe(first)

    rerender({ instanceId: 1, serverState: second })
    expect(result.current).toBe(second)
  })
})
