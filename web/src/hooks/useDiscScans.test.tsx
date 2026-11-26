/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { DiscScanRun } from "@/types"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

const { listDiscScans, startDiscScan, cancelDiscScan } = vi.hoisted(() => ({
  listDiscScans: vi.fn(),
  startDiscScan: vi.fn(),
  cancelDiscScan: vi.fn(),
}))
vi.mock("@/lib/api", () => ({ api: { listDiscScans, startDiscScan, cancelDiscScan } }))

import { useDiscScans } from "./useDiscScans"

const files = [{ name: "Movie/BDMV/index.bdmv" }] as never
const run = (overrides: Partial<DiscScanRun>): DiscScanRun => ({
  id: 7, instanceId: 1, torrentHash: "abc", discPath: "Movie", resolvedPath: "/data/Movie", status: "completed",
  processedBytes: 0, totalBytes: 0, createdAt: "2026-01-01T00:00:00Z", ...overrides,
})

function mount() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return renderHook(() => useDiscScans(1, "abc", files, true), { wrapper })
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe("useDiscScans list writes", () => {
  it("writes a start or cancel response for this torrent into the list", async () => {
    listDiscScans.mockResolvedValue([run({ id: 1, discPath: "Other" })])
    startDiscScan.mockResolvedValue(run({ status: "pending" }))
    cancelDiscScan.mockResolvedValue(run({ status: "canceled" }))
    const hook = mount()
    await waitFor(() => expect(hook.result.current.runsByPath.size).toBe(1))

    await act(() => hook.result.current.start.mutateAsync({ discPath: "Movie", force: false }))
    await waitFor(() => expect(hook.result.current.runsByPath.get("Movie")?.status).toBe("pending"))

    await act(() => hook.result.current.cancel.mutateAsync(7))
    await waitFor(() => expect(hook.result.current.runsByPath.get("Movie")?.status).toBe("canceled"))
  })

  it("keeps the shared row of another torrent out of the list, but the dialog still reads it from the start", async () => {
    listDiscScans.mockResolvedValue([run({ id: 1, discPath: "Other" })])
    const shared = run({ id: 9, torrentHash: "other", status: "completed" })
    startDiscScan.mockResolvedValue(shared)
    cancelDiscScan.mockResolvedValue({ ...shared, status: "canceled" })
    const hook = mount()
    await waitFor(() => expect(hook.result.current.runsByPath.size).toBe(1))

    await act(() => hook.result.current.start.mutateAsync({ discPath: "Movie", force: false }))
    await waitFor(() => expect(hook.result.current.runFor("Movie")).toEqual(shared))
    expect(hook.result.current.runsByPath.has("Movie")).toBe(false)

    await act(() => hook.result.current.cancel.mutateAsync(9))
    await waitFor(() => expect(hook.result.current.runFor("Movie")?.status).toBe("canceled"))
    expect(hook.result.current.runsByPath.has("Movie")).toBe(false)
  })
})
