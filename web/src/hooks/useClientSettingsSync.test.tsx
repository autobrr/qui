/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { useClientSettingsSync } from "@/hooks/useClientSettingsSync"
import { _resetClientSettingsForTests, useClientSettingsLoaded, useClientSettingsSettled } from "@/lib/client-settings"

const mocks = vi.hoisted(() => ({ getClientSettings: vi.fn() }))

vi.mock("@/hooks/useIsAuthed", () => ({ useIsAuthed: () => true }))
vi.mock("@/contexts/SyncStreamContext", () => ({ useActivityStream: () => {} }))
vi.mock("@/lib/api", () => ({ api: { getClientSettings: mocks.getClientSettings } }))

afterEach(() => {
  cleanup()
  _resetClientSettingsForTests()
})

function renderSync() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
  return renderHook(() => {
    useClientSettingsSync()
    return { loaded: useClientSettingsLoaded(), settled: useClientSettingsSettled() }
  }, { wrapper })
}

describe("useClientSettingsSync", () => {
  it("settles the settings without opening the push gate when the first read fails", async () => {
    mocks.getClientSettings.mockRejectedValue(new Error("qui is restarting"))
    const { result } = renderSync()
    expect(result.current).toEqual({ loaded: false, settled: false })

    await vi.waitFor(() => expect(result.current).toEqual({ loaded: false, settled: true }))
  })

  it("settles and loads the settings when the first read succeeds", async () => {
    mocks.getClientSettings.mockResolvedValue({})
    const { result } = renderSync()

    await vi.waitFor(() => expect(result.current).toEqual({ loaded: true, settled: true }))
  })
})
