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

import { refreshChangedCapabilities, useInstanceCapabilities, useInstancesSupport } from "@/hooks/useInstanceCapabilities"

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

// regression: #3764
describe("useInstancesSupport", () => {
  function mountSupport(answers: Record<number, Promise<Partial<InstanceCapabilities>>>, instanceIds: number[] | undefined, activeIds: number[] = []) {
    getInstanceCapabilities.mockImplementation((id: number) => answers[id])
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    queryClient.setQueryData(["instances"], activeIds.map(id => ({ id, isActive: true })))
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )
    const answered = (id: number) => expect(queryClient.getQueryData(["instance-capabilities", id])).toBeDefined()
    return { answered, ...renderHook(() => useInstancesSupport(instanceIds, "supportsShareLimitsAction"), { wrapper }) }
  }

  it("answers yes when every instance supports the capability", async () => {
    const { result } = mountSupport({
      1: Promise.resolve({ supportsShareLimitsAction: true }),
      2: Promise.resolve({ supportsShareLimitsAction: true }),
    }, [1, 2])

    await waitFor(() => expect(result.current).toBe(true))
  })

  it("answers no when one instance does not support the capability", async () => {
    const { answered, result } = mountSupport({
      1: Promise.resolve({ supportsShareLimitsAction: true }),
      2: Promise.resolve({ supportsShareLimitsAction: false }),
    }, [1, 2])

    await waitFor(() => { answered(1); answered(2) })
    expect(result.current).toBe(false)
  })

  it("answers no while one instance has not answered", async () => {
    const { answered, result } = mountSupport({
      1: Promise.resolve({ supportsShareLimitsAction: true }),
      2: new Promise(() => {}),
    }, [1, 2])

    await waitFor(() => answered(1))
    expect(result.current).toBe(false)
  })

  it("asks every active instance when no instance subset is set", async () => {
    const { answered, result } = mountSupport({
      1: Promise.resolve({ supportsShareLimitsAction: true }),
      3: Promise.resolve({ supportsShareLimitsAction: false }),
    }, undefined, [1, 3])

    await waitFor(() => { answered(1); answered(3) })
    expect(result.current).toBe(false)
  })
})
