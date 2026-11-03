/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { InstanceCapabilities } from "@/types"

const getInstanceCapabilities = vi.hoisted(() => vi.fn())
vi.mock("@/lib/api", () => ({ api: { getInstanceCapabilities } }))

import { refreshChangedCapabilities, useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"

afterEach(() => {
  cleanup()
  getInstanceCapabilities.mockReset()
})

async function mountCapabilities(version: string) {
  getInstanceCapabilities.mockResolvedValue({ webAPIVersion: version } as InstanceCapabilities)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  const { result } = renderHook(() => useInstanceCapabilities(1), { wrapper })
  await waitFor(() => expect(result.current.data?.webAPIVersion).toBe(version))
  return { queryClient, result }
}

describe("refreshChangedCapabilities", () => {
  it("sends no request when the version is unchanged or the instance has no capabilities loaded", async () => {
    const { queryClient } = await mountCapabilities("2.15.1")

    refreshChangedCapabilities(queryClient, { webAPIVersions: { 1: "2.15.1", 2: "2.9.3" } })

    expect(queryClient.isFetching()).toBe(0)
    expect(getInstanceCapabilities).toHaveBeenCalledTimes(1)
  })
})
