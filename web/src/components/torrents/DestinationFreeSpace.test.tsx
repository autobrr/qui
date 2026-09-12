/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, render } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/api", () => ({
  api: { getFreeSpaceAtPath: vi.fn() },
}))

// Render the interpolated value so the assertions read what a user sees.
vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, options?: { value?: string }) => (options?.value ? `${key}: ${options.value}` : key),
  }),
}))

const capabilities = vi.hoisted(() => ({ current: { supportsFreeSpaceAtPath: true } as Record<string, boolean> }))
vi.mock("@/hooks/useInstanceCapabilities.ts", () => ({
  useInstanceCapabilities: () => ({ data: capabilities.current }),
}))

import { api } from "@/lib/api"
import { DestinationFreeSpace } from "./DestinationFreeSpace"

const mockedApi = vi.mocked(api, true)

function renderAt(path: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return render(<DestinationFreeSpace instanceId={1} path={path} />, { wrapper })
}

describe("DestinationFreeSpace", () => {
  beforeEach(() => {
    vi.useFakeTimers()
    capabilities.current = { supportsFreeSpaceAtPath: true }
  })

  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    vi.clearAllMocks()
  })

  it("shows the free space qBittorrent reports for the destination", async () => {
    mockedApi.getFreeSpaceAtPath.mockResolvedValue({ path: "/downloads", bytes: 1099511627776 })

    const { container } = renderAt("/downloads")
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })

    expect(mockedApi.getFreeSpaceAtPath).toHaveBeenCalledWith(1, "/downloads")
    expect(container.textContent).toContain("1 TiB")
  })

  it("shows zero bytes as zero, not as unavailable", async () => {
    mockedApi.getFreeSpaceAtPath.mockResolvedValue({ path: "/downloads", bytes: 0 })

    const { container } = renderAt("/downloads")
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })

    expect(container.textContent).toContain("0 B")
    expect(container.textContent).not.toContain("status.unavailable")
  })

  it("shows an unmeasurable path as unavailable", async () => {
    mockedApi.getFreeSpaceAtPath.mockResolvedValue({ path: "/downloads", bytes: null })

    const { container } = renderAt("/downloads")
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })

    expect(container.textContent).toContain("status.unavailable")
  })

  it("shows a failed read as unavailable", async () => {
    mockedApi.getFreeSpaceAtPath.mockRejectedValue(new Error("boom"))

    const { container } = renderAt("/downloads")
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })

    expect(container.textContent).toContain("status.unavailable")
  })

  it("keeps the current destination when a late answer for an earlier one arrives", async () => {
    mockedApi.getFreeSpaceAtPath.mockImplementation(async (_id: number, path: string) => {
      if (path === "/slow") {
        await new Promise(resolve => setTimeout(resolve, 5000))
        return { path, bytes: 1099511627776 }
      }
      return { path, bytes: 536870912000 }
    })

    const { container, rerender } = renderAt("/slow")
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })

    rerender(<DestinationFreeSpace instanceId={1} path="/current" />)
    // Two ticks: the first passes the debounce and starts the query, the second
    // lets its answer land.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    expect(container.textContent).toContain("500 GiB")

    // The earlier destination answers now; the shown value must stay with the current one.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(container.textContent).toContain("500 GiB")
    expect(container.textContent).not.toContain("1 TiB")
  })

  it("stays quiet and asks nothing on instances without the endpoint", async () => {
    capabilities.current = { supportsFreeSpaceAtPath: false }

    const { container } = renderAt("/downloads")
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })

    expect(container.textContent).toBe("")
    expect(mockedApi.getFreeSpaceAtPath).not.toHaveBeenCalled()
  })
})
