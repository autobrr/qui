/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, expect, it, vi } from "vitest"

vi.mock("@/lib/shares-api", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/lib/shares-api")>(),
  listShares: vi.fn(),
}))

import { useTransferQueue } from "@/hooks/useTransferQueue"
import { listShares, type Share } from "@/lib/shares-api"
import { transferPageSize, transfersApi, type TransferList } from "@/lib/transfers-api"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.restoreAllMocks()
})

function wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}

it("counts the Transfers that run or wait, and the Shares that have not ended", async () => {
  vi.spyOn(transfersApi, "list").mockResolvedValue({ rows: [], counts: { active: 2, waiting: 4, finished: 9, failed: 1 } } as TransferList)
  vi.mocked(listShares).mockResolvedValue([{ state: "active" }, { state: "ending" }, { state: "ended" }] as Share[])

  const { result } = renderHook(() => useTransferQueue(), { wrapper })
  expect(result.current).toBe(0)

  await waitFor(() => expect(result.current).toBe(8))
  expect(transfersApi.list).toHaveBeenCalledWith("active", 0, transferPageSize)
})
