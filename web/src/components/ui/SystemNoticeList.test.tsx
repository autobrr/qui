/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import type { ReactNode } from "react"
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest"

import { SystemNoticeList } from "@/components/ui/SystemNoticeList"
import { systemNoticesQueryKey } from "@/hooks/useSystemNotices"
import "@/i18n"
import { server } from "@/test/msw/server"
import type { SystemNotice } from "@/types"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, search, children, ...props }: { to: string; search?: Record<string, string>; children: ReactNode }) => (
    <a href={`${to}?${new URLSearchParams(search)}`} {...props}>{children}</a>
  ),
}))

// No kind has a fix page yet, so the test adds one.
vi.mock("@/lib/system-notice", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/system-notice")>()),
  systemNoticeRoutes: { key_rejected: { to: "/settings", search: { tab: "indexers" } } },
}))

const minute = 60 * 1000

function notice(kind: string, cause: string, openedMinutesAgo: number): SystemNotice {
  return { kind, severity: "error", cause, openedAt: new Date(Date.now() - openedMinutesAgo * minute).toISOString() }
}

const recordingStopped = notice("recording_stopped", "write the transfer history of instance 1: attempt to write a readonly database", 4)
const unknownKind = notice("brand_new_kind", "something new broke", 6 * 60)
const keyRejected = notice("key_rejected", "401 Unauthorized", 2 * 24 * 60)

function renderList(notices: SystemNotice[]) {
  server.use(http.get("*/api/system-notices", () => HttpResponse.json(notices)))
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <SystemNoticeList />
    </QueryClientProvider>
  )
  return queryClient
}

beforeAll(() => {
  // jsdom has no ResizeObserver, and the Radix popper measures its anchor with one.
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(cleanup)

describe("SystemNoticeList", () => {
  it("shows one row for each open notice under a count", async () => {
    renderList([recordingStopped, unknownKind])

    const list = await screen.findByRole("region", { name: /Needs attention/ })
    const rows = within(list).getAllByRole("button")
    expect(rows.map((row) => row.textContent)).toEqual(["Recording stopped4m", "System notice6h"])
    expect(list.textContent).toContain("Needs attention2")
  })

  // regression: #4110
  it("opens the detail of a notice with its effect, fix, and cause", async () => {
    renderList([recordingStopped])

    fireEvent.click(await screen.findByRole("button", { name: /Recording stopped/ }))

    const detail = await screen.findByRole("dialog", { name: "Recording stopped" })
    expect(detail.textContent).toContain("qui cannot write the transfer history. It tries again at the next write.")
    expect(detail.textContent).toContain("Make sure that the disk has free space and that qui can write its database file.")
    expect(detail.textContent).toContain("attempt to write a readonly database")
    expect(within(detail).getByRole("button", { name: "Copy Cause" })).toBeDefined()
    expect(within(detail).queryByRole("link")).toBeNull()
  })

  // regression: #4110
  it("counts a notice that opened under a minute ago as one minute old", async () => {
    renderList([notice("recording_stopped", "database or disk is full", 0)])

    const row = await screen.findByRole("button", { name: /Recording stopped/ })
    expect(row.textContent).toBe("Recording stopped1m")
    fireEvent.click(row)
    expect((await screen.findByRole("dialog")).textContent).toContain("1 minute ago")
  })

  it("links a notice with a fix page to that page", async () => {
    renderList([keyRejected])

    fireEvent.click(await screen.findByRole("button", { name: /System notice/ }))

    const link = within(await screen.findByRole("dialog")).getByRole("link")
    expect(link.getAttribute("href")).toBe("/settings?tab=indexers")
  })

  it("shows the generic texts and the cause for a kind it does not know", async () => {
    renderList([unknownKind])

    fireEvent.click(await screen.findByRole("button", { name: /System notice/ }))

    const detail = await screen.findByRole("dialog")
    expect(detail.textContent).toContain("qui found a fault that needs an action from you.")
    expect(detail.textContent).toContain("The qui log has the details.")
    expect(detail.textContent).toContain("something new broke")
  })

  it("shows no label when no notice is open", async () => {
    const queryClient = renderList([])

    await waitFor(() => expect(queryClient.getQueryState(systemNoticesQueryKey)?.status).toBe("success"))
    expect(screen.queryByText("Needs attention")).toBeNull()
  })
})
