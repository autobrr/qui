/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const mocks = vi.hoisted(() => ({
  listBlocklist: vi.fn(() => Promise.resolve([])),
  addBlocklist: vi.fn(() => Promise.resolve({})),
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => ({ formatDate: (date: Date) => date.toISOString() }),
}))
vi.mock("@/lib/api", () => ({
  api: {
    listCrossSeedBlocklist: mocks.listBlocklist,
    addCrossSeedBlocklist: mocks.addBlocklist,
    deleteCrossSeedBlocklist: vi.fn(),
  },
}))

import type { Instance } from "@/types"
import { BlocklistTab } from "../BlocklistTab"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

const alpha = { id: 1, name: "alpha" } as Instance
const beta = { id: 2, name: "beta" } as Instance

describe("BlocklistTab instance", () => {
  it("keeps the first instance it showed when the instances are reordered", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const view = render(
      <QueryClientProvider client={client}>
        <BlocklistTab instances={[alpha, beta]} />
      </QueryClientProvider>
    )
    view.rerender(
      <QueryClientProvider client={client}>
        <BlocklistTab instances={[beta, alpha]} />
      </QueryClientProvider>
    )

    fireEvent.change(document.getElementById("blocklist-infohash")!, { target: { value: "a".repeat(40) } })
    fireEvent.click(screen.getByText("blocklist.add"))

    await waitFor(() => expect(mocks.addBlocklist).toHaveBeenCalledTimes(1))
    expect(mocks.addBlocklist.mock.calls[0]).toEqual([{ instanceId: 1, infoHash: "a".repeat(40), note: undefined }])
    expect(mocks.listBlocklist).not.toHaveBeenCalledWith(2)
  })

  it("moves to the first instance when its instance is removed", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const view = render(
      <QueryClientProvider client={client}>
        <BlocklistTab instances={[alpha, beta]} />
      </QueryClientProvider>
    )
    view.rerender(
      <QueryClientProvider client={client}>
        <BlocklistTab instances={[beta]} />
      </QueryClientProvider>
    )

    fireEvent.change(document.getElementById("blocklist-infohash")!, { target: { value: "b".repeat(40) } })
    fireEvent.click(screen.getByText("blocklist.add"))

    await waitFor(() => expect(mocks.addBlocklist).toHaveBeenCalledTimes(1))
    expect(mocks.addBlocklist.mock.calls[0]).toEqual([{ instanceId: 2, infoHash: "b".repeat(40), note: undefined }])
  })
})
