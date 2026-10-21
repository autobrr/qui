/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render, screen } from "@testing-library/react"
import type { ComponentProps } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { TooltipProvider } from "@/components/ui/tooltip"
import { makeTorrent } from "@/test/mockTorrent"
import { DeleteTorrentDialog } from "./DeleteTorrentDialog"
import "@/i18n"

afterEach(cleanup)

const torrents = ["Alpha", "Bravo", "Charlie", "Delta"].map(name => makeTorrent({ hash: `hash-${name}`, name }))

function renderDialog(props: Partial<ComponentProps<typeof DeleteTorrentDialog>>) {
  return render(
    <DeleteTorrentDialog
      open
      onOpenChange={vi.fn()}
      count={1}
      totalSize={0}
      formattedSize=""
      deleteFiles={false}
      onDeleteFilesChange={vi.fn()}
      isDeleteFilesLocked={false}
      onToggleDeleteFilesLock={vi.fn()}
      deleteCrossSeeds={false}
      onDeleteCrossSeedsChange={vi.fn()}
      onBlockCrossSeedsChange={vi.fn()}
      onConfirm={vi.fn()}
      {...props}
    />,
    { wrapper: TooltipProvider }
  )
}

// Issue #3364: a large delete names its scope, so a select all delete is not a surprise.
describe("DeleteTorrentDialog", () => {
  it("keeps the short dialog for a small delete", () => {
    renderDialog({ count: 2, previewTorrents: torrents.slice(0, 2) })

    expect(screen.getByRole("heading").textContent).toBe("Delete 2 torrent(s)?")
    expect(screen.queryByText("Alpha")).toBeNull()
    expect(screen.getByRole("button", { name: "Delete" })).not.toBeNull()
  })

  it("names three torrents and the rest of the count in a large delete", () => {
    renderDialog({ count: 12, previewTorrents: torrents })

    expect(screen.getByText("Charlie")).not.toBeNull()
    expect(screen.queryByText("Delta")).toBeNull()
    expect(screen.getByText("and 9 more")).not.toBeNull()
    expect(screen.queryByText(/Select all is on/)).toBeNull()
    expect(screen.getByRole("button", { name: "Delete 12 torrents" })).not.toBeNull()
  })

  it("says that select all reaches torrents that are not loaded", () => {
    renderDialog({ count: 7500, isAllSelected: true, previewTorrents: torrents })

    expect(screen.getByRole("heading").textContent).toBe("Delete all 7500 torrents that match the filter?")
    expect(screen.getByText(/Select all is on\. .* 7496 of them are not loaded in the table\./)).not.toBeNull()
    expect(screen.getByText("and 7497 more")).not.toBeNull()
    expect(screen.getByRole("button", { name: "Delete 7500 torrents" })).not.toBeNull()
  })

  it("shows the select all scope also below the large delete limit", () => {
    renderDialog({ count: 2, isAllSelected: true, previewTorrents: torrents.slice(0, 2) })

    expect(screen.getByText(/^Select all is on\.[^.]*\.$/)).not.toBeNull()
    expect(screen.getByRole("button", { name: "Delete 2 torrents" })).not.toBeNull()
  })
})
