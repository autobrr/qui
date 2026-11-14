/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, render, waitFor } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, describe, expect, it, vi } from "vitest"

import { DatabaseShrinkProvider } from "@/components/database/DatabaseShrinkProvider"
import { databaseShrinkQueryKey } from "@/hooks/useDatabaseShrink"
import "@/i18n"
import { server } from "@/test/msw/server"
import type { DatabaseShrinkStatus } from "@/types"

const i18n = vi.hoisted(() => ({ t: (key: string) => key }))
// The byte units come from the loaded locale, and the toast text stays as keys.
vi.mock("react-i18next", async (importOriginal) => ({ ...(await importOriginal<typeof import("react-i18next")>()), useTranslation: () => i18n }))
const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }))
vi.mock("sonner", () => ({ toast }))
const selfUpdate = vi.hoisted(() => ({ shrink: vi.fn(async () => true), pending: false, overlay: "hidden" }))
vi.mock("@/hooks/useSelfUpdate", () => ({ useSelfUpdate: () => selfUpdate }))
vi.mock("@/components/settings/RestartOverlay", () => ({ RestartOverlay: () => null }))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

const base: DatabaseShrinkStatus = { available: true, fileBytes: 1024, freeBytes: 1 << 30, needBytes: 2048, restartAvailable: true, pending: false }

describe("DatabaseShrinkProvider", () => {
  // regression: #4109
  it("shows each shrink result one time and dismisses it", async () => {
    let status: DatabaseShrinkStatus = { ...base, result: { beforeBytes: 300, afterBytes: 100 } }
    let dismissals = 0
    server.use(
      http.get("*/api/database/shrink", () => HttpResponse.json(status)),
      http.delete("*/api/database/shrink/result", () => {
        dismissals++
        status = base
        return new HttpResponse(null, { status: 204 })
      })
    )
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={queryClient}>
        <DatabaseShrinkProvider>{null}</DatabaseShrinkProvider>
      </QueryClientProvider>
    )

    await waitFor(() => expect(dismissals).toBe(1))
    await waitFor(() => expect(queryClient.getQueryData<DatabaseShrinkStatus>(databaseShrinkQueryKey)?.result).toBeUndefined())
    expect(toast.success).toHaveBeenCalledTimes(1)

    // A shrink at a later start, while the tab stays open.
    status = { ...base, result: { beforeBytes: 500, afterBytes: 200 } }
    await act(() => queryClient.invalidateQueries({ queryKey: databaseShrinkQueryKey }))

    await waitFor(() => expect(dismissals).toBe(2))
    expect(toast.success).toHaveBeenCalledTimes(2)
  })
})
