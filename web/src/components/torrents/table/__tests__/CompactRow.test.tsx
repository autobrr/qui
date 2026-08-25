/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { CompactRow } from "@/components/torrents/table/CompactRow"
import { getLinuxIsoName } from "@/lib/incognito"
import type { TrackerCustomizationLookup } from "@/lib/tracker-customizations"
import { makeTorrent } from "@/test/mockTorrent"
import { cleanup, fireEvent, render } from "@testing-library/react"
import type { Torrent } from "@/types"
import { afterEach, describe, expect, it, vi } from "vitest"

// Presentational component: stub i18n with a passthrough translator so we can
// render without bootstrapping the real i18next instance.
vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, opts?: { defaultValue?: string }) => opts?.defaultValue ?? key,
  }),
}))

afterEach(cleanup)

const emptyLookup: TrackerCustomizationLookup = new Map()

function renderCompactRow(overrides: {
  torrent?: Torrent
  incognitoMode?: boolean
  showCheckbox?: boolean
  onCheckboxChange?: (torrent: Torrent, rowId: string, checked: boolean) => void
} = {}) {
  const torrent = overrides.torrent ?? makeTorrent({ name: "My Torrent", hash: "hash-1" })
  return render(
    <CompactRow
      torrent={torrent}
      rowId="row-0"
      rowIndex={0}
      isSelected={false}
      isRowSelected={false}
      showCheckbox={overrides.showCheckbox ?? true}
      onClick={vi.fn()}
      onContextMenu={vi.fn()}
      onCheckboxPointerDown={vi.fn()}
      onCheckboxChange={overrides.onCheckboxChange ?? vi.fn()}
      incognitoMode={overrides.incognitoMode ?? false}
      speedUnit="bytes"
      supportsTrackerHealth={false}
      trackerIcons={undefined}
      trackerCustomizationLookup={emptyLookup}
      style={{}}
    />
  )
}

describe("CompactRow", () => {
  it("renders the torrent name when not in incognito mode", () => {
    const { container } = renderCompactRow({
      torrent: makeTorrent({ name: "Ubuntu 24.04 ISO", hash: "hash-1" }),
    })
    expect(container.textContent).toContain("Ubuntu 24.04 ISO")
  })

  it("masks the name with a Linux ISO alias in incognito mode", () => {
    const torrent = makeTorrent({ name: "Ubuntu 24.04 ISO", hash: "hash-xyz" })
    const { container } = renderCompactRow({ torrent, incognitoMode: true })
    expect(container.textContent).not.toContain("Ubuntu 24.04 ISO")
    expect(container.textContent).toContain(getLinuxIsoName(torrent.hash))
  })

  it("renders the progress percentage", () => {
    const { container } = renderCompactRow({
      torrent: makeTorrent({ name: "t", hash: "h", progress: 0.5 }),
    })
    expect(container.textContent).toContain("50%")
  })

  it("renders an infinite ratio as the infinity glyph", () => {
    const { container } = renderCompactRow({
      torrent: makeTorrent({ name: "t", hash: "h", ratio: -1 }),
    })
    expect(container.textContent).toContain("∞")
  })

  it("shows a download-speed indicator when downloading", () => {
    const { container } = renderCompactRow({
      torrent: makeTorrent({ name: "t", hash: "h", dlspeed: 1024 }),
    })
    expect(container.textContent).toContain("↓")
  })

  it("omits speed indicators when idle", () => {
    const { container } = renderCompactRow({
      torrent: makeTorrent({ name: "t", hash: "h", dlspeed: 0, upspeed: 0 }),
    })
    expect(container.textContent).not.toContain("↓")
    expect(container.textContent).not.toContain("↑")
  })

  it("renders a checkbox and forwards toggles to onCheckboxChange", () => {
    const onCheckboxChange = vi.fn()
    const torrent = makeTorrent({ name: "t", hash: "hash-1" })
    const { container } = renderCompactRow({ torrent, onCheckboxChange })
    const checkbox = container.querySelector("[role=\"checkbox\"]")
    expect(checkbox).not.toBeNull()
    fireEvent.click(checkbox!)
    expect(onCheckboxChange).toHaveBeenCalledWith(torrent, "row-0", true)
  })

  it("hides the checkbox when showCheckbox is false", () => {
    const { container } = renderCompactRow({ showCheckbox: false })
    expect(container.querySelector("[role=\"checkbox\"]")).toBeNull()
  })
})
