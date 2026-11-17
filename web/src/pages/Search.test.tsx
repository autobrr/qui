/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))

import { TooltipProvider } from "@/components/ui/tooltip"
import { SearchTableRow } from "@/pages/Search"
import type { TorznabSearchResult } from "@/types"

afterEach(cleanup)

const result: TorznabSearchResult = {
  indexer: "Rig",
  indexerId: 1,
  title: "Sample.Film.2023.2160p.BluRay-RIG",
  downloadUrl: "http://127.0.0.1/dl/2.torrent",
  size: 1024,
  seeders: 3,
  leechers: 1,
  categoryId: 2000,
  categoryName: "Movies",
  publishDate: "2026-11-16T09:00:00Z",
  downloadVolumeFactor: 1,
  uploadVolumeFactor: 1,
  guid: "rig-2",
}

describe("SearchTableRow", () => {
  // regression: #4263
  it("says there is no info URL on a tap of the disabled details button, and leaves the row unselected", () => {
    const touchWindow = window as Window & { ontouchstart?: unknown }
    touchWindow.ontouchstart = null
    // The open tooltip measures its arrow through ResizeObserver, which jsdom lacks.
    vi.stubGlobal("ResizeObserver", class {
      observe() {}
      unobserve() {}
      disconnect() {}
    })
    try {
      const onToggleSelection = vi.fn()
      render(
        <TooltipProvider>
          <SearchTableRow
            result={result}
            isSelected={false}
            isEven
            height={40}
            translateY={0}
            categoryLabel="Movies"
            publishedLabel="date"
            onToggleSelection={onToggleSelection}
            onDownload={() => {}}
            onViewDetails={() => {}}
          />
        </TooltipProvider>
      )

      // A disabled Button has pointer-events: none, so the tap lands on the wrapper around it.
      const wrapper = screen.getByRole("button", { name: "card.viewDetails" }).parentElement!
      fireEvent.pointerDown(wrapper, { pointerType: "touch" })
      fireEvent.click(wrapper)

      expect(screen.getByRole("tooltip").textContent).toContain("table.noInfoUrl")
      expect(onToggleSelection).not.toHaveBeenCalled()
    } finally {
      vi.unstubAllGlobals()
      delete touchWindow.ontouchstart
    }
  })
})
