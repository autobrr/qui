/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, describe, expect, it, vi } from "vitest"

import { SystemNoticeList } from "@/components/ui/SystemNoticeList"
import { systemNoticesQueryKey } from "@/hooks/useSystemNotices"
import type { SystemNotice } from "@/types"
import { server } from "@/test/msw/server"

const i18n = vi.hoisted(() => ({
  t: (key: string | string[], values?: Record<string, string>) => {
    const first = Array.isArray(key) ? key[0] : key
    return values?.cause ? `${first} ${values.cause}` : first
  },
}))
vi.mock("react-i18next", () => ({ useTranslation: () => i18n }))

function renderBanner(notices: SystemNotice[]) {
  server.use(http.get("*/api/system-notices", () => HttpResponse.json(notices)))
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <SystemNoticeList />
    </QueryClientProvider>
  )
  return queryClient
}

afterEach(cleanup)

describe("SystemNoticeList", () => {
  it("shows an open notice with its fix and cause", async () => {
    renderBanner([{
      kind: "recording_stopped",
      severity: "error",
      cause: "write the transfer history of instance 1: attempt to write a readonly database",
      openedAt: "2026-11-13T20:00:00Z",
    }])

    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain("systemNotice.kinds.recording_stopped.title")
    expect(alert.textContent).toContain("systemNotice.kinds.recording_stopped.fix")
    expect(alert.textContent).toContain("attempt to write a readonly database")
  })

  it("shows nothing when no notice is open", async () => {
    const queryClient = renderBanner([])

    await waitFor(() => expect(queryClient.getQueryState(systemNoticesQueryKey)?.status).toBe("success"))
    expect(screen.queryByRole("alert")).toBeNull()
  })
})
