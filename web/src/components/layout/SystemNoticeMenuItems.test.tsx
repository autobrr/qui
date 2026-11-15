/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest"

import { SystemNoticeMenuItems } from "@/components/layout/SystemNoticeMenuItems"
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { SystemNoticeDialog } from "@/components/ui/SystemNotice"
import { useOpenSystemNotice } from "@/hooks/useSystemNotices"
import "@/i18n"
import type { SystemNotice } from "@/types"

const open: SystemNotice[] = [
  { kind: "recording_stopped", severity: "error", cause: "attempt to write a readonly database", openedAt: new Date(Date.now() - 4 * 60 * 1000).toISOString() },
  { kind: "brand_new_kind", severity: "error", cause: "something new broke", openedAt: new Date(Date.now() - 2 * 60 * 60 * 1000).toISOString() },
]

// The header menu and the phone menu wire the rows and the dialog the same way.
function Menu({ notices }: { notices: SystemNotice[] }) {
  const openNotice = useOpenSystemNotice(notices)
  return (
    <>
      <DropdownMenu defaultOpen>
        <DropdownMenuTrigger>menu</DropdownMenuTrigger>
        <DropdownMenuContent>
          <SystemNoticeMenuItems notices={notices} onOpen={openNotice.open} />
        </DropdownMenuContent>
      </DropdownMenu>
      <SystemNoticeDialog notice={openNotice.notice} returnFocusTo={openNotice.menuButton} onClose={openNotice.close} />
    </>
  )
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

describe("SystemNoticeMenuItems", () => {
  it("shows each notice with its short title and fix line", async () => {
    render(<Menu notices={open} />)

    const group = await screen.findByRole("group", { name: /Needs attention/ })
    const rows = within(group).getAllByRole("menuitem")
    expect(rows.map((row) => row.textContent)).toEqual([
      "Recording stoppedFree disk space for the database4m",
      "System noticeRead the qui log2h",
    ])
  })

  it("opens the detail of a selected notice in a dialog", async () => {
    render(<Menu notices={open} />)

    fireEvent.click(await screen.findByRole("menuitem", { name: /Recording stopped/ }))

    const dialog = await screen.findByRole("dialog", { name: "Recording stopped" })
    expect(dialog.textContent).toContain("Make sure that the disk has free space and that qui can write its database file.")
    expect(dialog.textContent).toContain("attempt to write a readonly database")
    expect(screen.queryByRole("menu")).toBeNull()
  })

  // regression: #4110
  it("keeps the age in an open dialog up to date", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "Date"] })
    try {
      const justOpened: SystemNotice = { ...open[0], openedAt: new Date().toISOString() }
      render(<Menu notices={[justOpened]} />)
      // The fake setInterval stops the polling of findBy, so the test queries at once.
      fireEvent.click(screen.getByRole("menuitem", { name: /Recording stopped/ }))
      const dialog = screen.getByRole("dialog", { name: "Recording stopped" })
      expect(dialog.textContent).toContain("1 minute ago")

      act(() => vi.advanceTimersByTime(3 * 60 * 1000))

      expect(dialog.textContent).toContain("3 minutes ago")
    } finally {
      vi.useRealTimers()
    }
  })

  // regression: #4110
  it("gives focus back to the menu button when the dialog closes", async () => {
    render(<Menu notices={open} />)
    fireEvent.click(await screen.findByRole("menuitem", { name: /Recording stopped/ }))
    await screen.findByRole("dialog", { name: "Recording stopped" })

    fireEvent.click(screen.getByRole("button", { name: "Close" }))

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    // Radix moves the focus of a closed dialog on the next task.
    await act(() => new Promise((resolve) => setTimeout(resolve, 0)))
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "menu" }))
  })

  // regression: #4110
  it("keeps the dialog closed when a notice that closed while open comes back", async () => {
    const { rerender } = render(<Menu notices={open} />)
    fireEvent.click(await screen.findByRole("menuitem", { name: /Recording stopped/ }))
    await screen.findByRole("dialog", { name: "Recording stopped" })

    rerender(<Menu notices={[]} />)
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    rerender(<Menu notices={open} />)

    expect(screen.queryByRole("dialog")).toBeNull()
  })
})
