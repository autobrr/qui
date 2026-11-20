/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { ApplicationInfo, ExperimentalFeature } from "@/types"

const scenario = vi.hoisted(() => ({ isDemo: false }))
const getApplicationInfo = vi.hoisted(() => vi.fn())
vi.mock("@/lib/api", () => ({ api: { getApplicationInfo } }))
vi.mock("@/lib/demo", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/demo")>()),
  get isDemo() { return scenario.isDemo },
}))

import { useExperimentalFeature } from "@/hooks/useExperimentalFeature"

const testFeature: ExperimentalFeature = "problemReports"
const infoWithTestFeature = { experimentalFeatures: [testFeature] } as unknown as ApplicationInfo

afterEach(() => {
  cleanup()
  getApplicationInfo.mockReset()
  scenario.isDemo = false
})

function mountHook(cached?: ApplicationInfo) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  if (cached) queryClient.setQueryData(["application-info"], cached)
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return { queryClient, ...renderHook(() => useExperimentalFeature(testFeature), { wrapper }) }
}

describe("useExperimentalFeature", () => {
  it("answers off while the application info loads", () => {
    getApplicationInfo.mockReturnValue(new Promise(() => {}))

    const { result } = mountHook()

    expect(result.current).toBe(false)
  })

  it("answers on when the application info names the feature", async () => {
    getApplicationInfo.mockResolvedValue(infoWithTestFeature)

    const { result } = mountHook()

    await waitFor(() => expect(result.current).toBe(true))
  })

  it("answers off when the application info request fails", async () => {
    getApplicationInfo.mockRejectedValue(new Error("Application info unavailable"))

    const { queryClient, result } = mountHook()

    await waitFor(() => expect(queryClient.getQueryState(["application-info"])?.status).toBe("error"))
    expect(result.current).toBe(false)
  })

  // regression: #4287
  it("answers off when a refetch fails after the application info named the feature", async () => {
    getApplicationInfo.mockRejectedValue(new Error("Application info unavailable"))

    const { queryClient, result } = mountHook(infoWithTestFeature)
    expect(result.current).toBe(true)

    await act(() => queryClient.refetchQueries({ queryKey: ["application-info"] }))

    expect(queryClient.getQueryState(["application-info"])?.status).toBe("error")
    await waitFor(() => expect(result.current).toBe(false))
  })

  it("answers off in the demo, even when the application info names the feature", () => {
    scenario.isDemo = true
    getApplicationInfo.mockResolvedValue(infoWithTestFeature)

    const { result } = mountHook(infoWithTestFeature)

    expect(result.current).toBe(false)
    expect(getApplicationInfo).not.toHaveBeenCalled()
  })
})
