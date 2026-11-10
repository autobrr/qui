/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TorrentListEmptyState } from "@/components/torrents/TorrentListEmptyState"
import { cleanup, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

afterEach(cleanup)

describe("TorrentListEmptyState", () => {
  it("offers to clear the filters that hide every torrent", async () => {
    const clear = vi.fn()
    render(<TorrentListEmptyState hasFilters hasSearch onClearFilters={clear} />)

    expect(screen.getByText("emptyState.noFilterMatches")).not.toBeNull()
    await userEvent.click(screen.getByRole("button", { name: "columnFilter.clearFilters" }))
    expect(clear).toHaveBeenCalledOnce()
  })

  it.each([
    ["a search", false, true, "emptyState.noSearchMatches"],
    ["nothing", false, false, "emptyState.noTorrents"],
  ])("shows no button when %s hides the torrents", (_, hasFilters, hasSearch, message) => {
    render(<TorrentListEmptyState hasFilters={hasFilters} hasSearch={hasSearch} onClearFilters={vi.fn()} />)

    expect(screen.getByText(message)).not.toBeNull()
    expect(screen.queryByRole("button")).toBeNull()
  })
})
