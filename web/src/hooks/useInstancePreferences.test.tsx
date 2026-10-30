/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/api", () => ({
  api: {
    getInstancePreferenceView: vi.fn(),
    updateInstancePreferences: vi.fn(),
  },
}))

import { api } from "@/lib/api"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import type { InstanceMetadata } from "@/hooks/useInstanceMetadata"
import type { PreferenceField } from "@/types"

const mockedApi = vi.mocked(api, true)

function view(listenPort: number): PreferenceField[] {
  return [{ key: "listen_port", value: listenPort, tab: "connection", section: "listeningPort", control: "number" }]
}

function makeWrapper(client: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("useInstancePreferences", () => {
  it("reads the values of the preference view", async () => {
    mockedApi.getInstancePreferenceView.mockResolvedValue(view(6881))
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    const { result } = renderHook(() => useInstancePreferences(1), { wrapper: makeWrapper(client) })

    await waitFor(() => expect(result.current.preferences?.listen_port).toBe(6881))
    expect(mockedApi.getInstancePreferenceView).toHaveBeenCalledWith(1)
  })

  it("fetches the view again when a screen opens, so a change made outside qui shows", async () => {
    mockedApi.getInstancePreferenceView.mockResolvedValue(view(6881))
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = makeWrapper(client)

    const first = renderHook(() => useInstancePreferences(1), { wrapper })
    await waitFor(() => expect(first.result.current.preferences?.listen_port).toBe(6881))
    first.unmount()

    mockedApi.getInstancePreferenceView.mockResolvedValue(view(51413))
    const second = renderHook(() => useInstancePreferences(1), { wrapper })

    await waitFor(() => expect(second.result.current.preferences?.listen_port).toBe(51413))
  })

  it("shows the values of the view that a save returns", async () => {
    mockedApi.getInstancePreferenceView.mockResolvedValue(view(6881))
    mockedApi.updateInstancePreferences.mockResolvedValue(view(51413))
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    const { result } = renderHook(() => useInstancePreferences(1), { wrapper: makeWrapper(client) })
    await waitFor(() => expect(result.current.preferences?.listen_port).toBe(6881))

    await act(async () => {
      await result.current.updatePreferences({ listen_port: 51413 })
    })

    expect(mockedApi.updateInstancePreferences).toHaveBeenCalledWith(1, { listen_port: 51413 })
    await waitFor(() => expect(result.current.preferences?.listen_port).toBe(51413))
  })

  it("does not write preferences into the instance metadata", async () => {
    mockedApi.getInstancePreferenceView.mockResolvedValue(view(6881))
    mockedApi.updateInstancePreferences.mockResolvedValue(view(51413))
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const metadata: InstanceMetadata = { categories: {}, tags: ["linux"] }
    client.setQueryData(["instance-metadata", 1], metadata)

    const { result } = renderHook(() => useInstancePreferences(1), { wrapper: makeWrapper(client) })
    await waitFor(() => expect(result.current.preferences?.listen_port).toBe(6881))
    await act(async () => {
      await result.current.updatePreferences({ listen_port: 51413 })
    })

    expect(client.getQueryData(["instance-metadata", 1])).toBe(metadata)
  })
})
