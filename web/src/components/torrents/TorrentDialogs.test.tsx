/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { RenameTorrentFileDialog } from "@/components/torrents/TorrentDialogs"
import { cleanup, render, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock("@/lib/api", () => ({ api: {} }))
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
