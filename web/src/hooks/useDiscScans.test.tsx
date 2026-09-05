/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { DiscScanRun } from "@/types"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

const { listDiscScans, toast } = vi.hoisted(() => ({
  listDiscScans: vi.fn(),
  toast: { success: vi.fn(), error: vi.fn() },
}))
vi.mock("@/lib/api", () => ({ api: { listDiscScans } }))
vi.mock("sonner", () => ({ toast }))
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string, options?: { error?: string }) => options?.error ? `${key}:${options.error}` : key }),
}))

import { useDiscScans } from "./useDiscScans"

const files = [{ name: "Movie/BDMV/index.bdmv" }] as never
const run = (status: DiscScanRun["status"], errorMessage?: string): DiscScanRun => ({
  id: 7, instanceId: 1, torrentHash: "abc", discPath: "Movie", resolvedPath: "/data/Movie", status, errorMessage,
  processedBytes: 0, totalBytes: 0, createdAt: "2026-01-01T00:00:00Z",
})

function mount() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  const hook = renderHook(() => useDiscScans(1, "abc", files, true), { wrapper })
  return { queryClient, hook }
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe("useDiscScans completion toast", () => {
  it("toasts when a refetch moves a known run to completed", async () => {
    listDiscScans.mockResolvedValueOnce([run("scanning")]).mockResolvedValueOnce([run("completed")])
    const { queryClient, hook } = mount()
    await waitFor(() => expect(hook.result.current.runsByPath.get("Movie")?.status).toBe("scanning"))
    expect(toast.success).not.toHaveBeenCalled()

    await act(() => queryClient.invalidateQueries({ queryKey: ["disc-scans", 1] }))
    await waitFor(() => expect(hook.result.current.runsByPath.get("Movie")?.status).toBe("completed"))
    expect(toast.success).toHaveBeenCalledTimes(1)
    expect(toast.success).toHaveBeenCalledWith("discScan.toast.completed")
  })

  it("toasts the error when a known run fails, and stays silent for rows that were already finished", async () => {
    listDiscScans.mockResolvedValueOnce([run("completed"), { ...run("pending"), id: 8, discPath: "Other" }])
      .mockResolvedValueOnce([run("completed"), { ...run("failed", "interrupted by qui restart"), id: 8, discPath: "Other" }])
    const { queryClient, hook } = mount()
    await waitFor(() => expect(hook.result.current.runsByPath.size).toBe(2))
    expect(toast.success).not.toHaveBeenCalled()

    await act(() => queryClient.invalidateQueries({ queryKey: ["disc-scans", 1] }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("discScan.toast.failed:interrupted by qui restart"))
    expect(toast.success).not.toHaveBeenCalled()
  })
})
