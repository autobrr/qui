/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { RenameTorrentFileDialog, TagEditorDialog } from "@/components/torrents/TorrentDialogs"
import { makeFilters } from "@/test/mockFilters"
import { makeTorrent } from "@/test/mockTorrent"
import { cleanup, render, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const translation = vi.hoisted(() => ({ t: (key: string) => key }))
vi.mock("react-i18next", () => ({
  useTranslation: () => translation,
}))
const api = vi.hoisted(() => ({ getTorrentField: vi.fn(() => new Promise(() => {})) }))
vi.mock("@/lib/api", () => ({ api }))
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: () => ({ getTotalSize: () => 0, getVirtualItems: () => [] }),
}))
vi.mock("@/hooks/usePathAutocomplete", () => ({
  usePathAutocomplete: () => ({ suggestions: [], isLoading: false }),
}))

afterEach(cleanup)

function renderDialog(path: string) {
  render(
    <RenameTorrentFileDialog
      open
      onOpenChange={() => {}}
      files={[{ name: path }]}
      onConfirm={() => {}}
      initialPath={path}
    />
  )
  return document.getElementById("fileName") as HTMLInputElement
}

describe("RenameTorrentFileDialog", () => {
  it("selects only the base name, never the extension (#2107)", async () => {
    const name = "Movie.Name.2023.UHD.BluRay.2160p.DTS-HD.MA.5.1.HEVC.REMUX-GROUP.mkv"
    const input = renderDialog(`folder/${name}`)
    await waitFor(() => expect(input.selectionEnd).toBe(name.lastIndexOf(".")))
    expect(input.selectionStart).toBe(0)
    expect(input.value.slice(input.selectionEnd!)).toBe(".mkv")
    // backward direction keeps the caret at the start, so engines that scroll
    // the caret into view show the start of the name, not the dot boundary
    expect(input.selectionDirection).toBe("backward")
  })

  it("scrolls the input back to the start so the unselected extension is not hidden off-screen (#2107)", async () => {
    const name = "Movie.Name.2023.UHD.BluRay.2160p.DTS-HD.MA.5.1.HEVC.REMUX-GROUP.mkv"
    const input = renderDialog(`folder/${name}`)
    // jsdom does no layout, so emulate the scroll a real browser performs when
    // it brings the selection end of an overflowing value into view.
    let scrollLeft = 500
    Object.defineProperty(input, "scrollLeft", {
      configurable: true,
      get: () => scrollLeft,
      set: (v: number) => { scrollLeft = v },
    })
    await waitFor(() => expect(input.selectionEnd).toBe(name.lastIndexOf(".")))
    expect(scrollLeft).toBe(0)
  })

  it("selects the whole name when there is no extension", async () => {
    const input = renderDialog("folder/NoExtensionFile")
    await waitFor(() => expect(input.selectionEnd).toBe("NoExtensionFile".length))
    expect(input.selectionStart).toBe(0)
  })
})

describe("TagEditorDialog", () => {
  // The torrent list rebuilds the request on each stream update. The same
  // selection in new arrays must not restart the tag read, or a read slower
  // than the updates never ends.
  it("reads the tags of a Select all selection one time while the request contents stay the same", () => {
    const onOpenChange = () => {}
    const props = (filters = makeFilters()) => ({
      open: true,
      onOpenChange,
      availableTags: [],
      selectedTorrents: [],
      hashCount: 7500,
      onConfirm: () => {},
      selectionRequest: { instanceId: 1, hashes: [], selectAll: true, filters, search: "", excludeHashes: [] },
    })
    const { rerender } = render(<TagEditorDialog {...props()} />)
    rerender(<TagEditorDialog {...props()} />)
    expect(api.getTorrentField).toHaveBeenCalledTimes(1)

    rerender(<TagEditorDialog {...props(makeFilters({ status: ["paused"] }))} />)
    expect(api.getTorrentField).toHaveBeenCalledTimes(2)
  })
  // A live sort reorders the loaded rows. Their tags do not decide the tags of
  // a selection past the loaded rows, so the reorder must not restart the read.
  it("keeps the Select all tag read when the loaded rows change order", () => {
    api.getTorrentField.mockClear()
    const onOpenChange = () => {}
    const request = { instanceId: 1, hashes: [], selectAll: true, filters: makeFilters(), search: "", excludeHashes: [] }
    const rows = [makeTorrent({ hash: "h0", tags: "a" }), makeTorrent({ hash: "h1", tags: "b" })]
    const props = (selectedTorrents = rows) => ({
      open: true, onOpenChange, availableTags: [], selectedTorrents, hashCount: 7500, onConfirm: () => {}, selectionRequest: request,
    })
    const { rerender } = render(<TagEditorDialog {...props()} />)
    rerender(<TagEditorDialog {...props([rows[1], rows[0]])} />)
    expect(api.getTorrentField).toHaveBeenCalledTimes(1)
  })
})
