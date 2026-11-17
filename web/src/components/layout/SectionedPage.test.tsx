/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { useState, type ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { SIDEWAYS_PHONE_QUERY } from "@/hooks/useMediaQuery"

import { SectionedPage } from "./SectionedPage"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const GROUPS = [{ label: "General", sections: [{ value: "a", label: "Alpha" }, { value: "b", label: "Beta" }] }] as const
type Section = "a" | "b"

type Width = "phone" | "sideways" | "desktop"

/** Returns a resize to another width, which tells the media queries that listen. */
function stubWidth(initial: Width) {
  let kind = initial
  const listeners = new Set<() => void>()
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: (kind === "phone" && query === "(max-width: 767px)") || (kind === "sideways" && query === SIDEWAYS_PHONE_QUERY),
    media: query,
    addEventListener: (_type: string, listener: () => void) => listeners.add(listener),
    removeEventListener: (_type: string, listener: () => void) => listeners.delete(listener),
  }))
  return (next: Width) => act(() => {
    kind = next
    listeners.forEach((listener) => listener())
  })
}

function Draft() {
  const [value, setValue] = useState("")
  return <input aria-label="Key" value={value} onChange={(event) => setValue(event.target.value)} />
}

function renderPage(section: Section | undefined, onSectionChange = vi.fn(), content: (shown: Section) => ReactNode = (shown) => <p>content of {shown}</p>) {
  const page = (current: Section | undefined) => (
    <main>
      <SectionedPage title="Settings" description="All settings" groups={GROUPS} section={current} defaultSection="a" onSectionChange={onSectionChange}>
        {content}
      </SectionedPage>
    </main>
  )
  const view = render(page(section))
  return { onSectionChange, rerender: (next: Section | undefined) => view.rerender(page(next)) }
}

describe("SectionedPage", () => {
  it("shows the default section on a wide screen and opens a new one at the top of the page", () => {
    stubWidth("desktop")
    const { onSectionChange } = renderPage(undefined)
    const main = screen.getByRole("main")
    Object.defineProperty(main, "scrollTop", { value: 500, writable: true })

    expect(screen.getByText("content of a")).not.toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Beta" }))

    expect(main.scrollTop).toBe(0)
    expect(onSectionChange).toHaveBeenCalledWith("b", { replace: true })
  })

  it("shows the menu first on a phone, adds a history entry for the section that the user taps, and goes back to the menu", () => {
    stubWidth("phone")
    const back = vi.spyOn(window.history, "back").mockImplementation(() => {})
    const { onSectionChange, rerender } = renderPage(undefined)

    expect(screen.queryByText(/content of/)).toBeNull()
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Settings")
    fireEvent.click(screen.getByRole("button", { name: "Beta" }))
    rerender("b")

    expect(onSectionChange).toHaveBeenCalledWith("b", { replace: false })
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Beta")
    fireEvent.click(screen.getByRole("button", { name: "Settings" }))

    expect(back).toHaveBeenCalledOnce()
    expect(onSectionChange).toHaveBeenCalledTimes(1)
  })

  // regression: #4233
  it("returns to the menu in place when a link opened the section, also after a tap on another section", () => {
    stubWidth("phone")
    const back = vi.spyOn(window.history, "back").mockImplementation(() => {})
    // A link, for example from the command palette, opens Beta with no tap in the menu.
    const { onSectionChange, rerender } = renderPage("b")
    fireEvent.click(screen.getByRole("button", { name: "Settings" }))

    expect(back).not.toHaveBeenCalled()
    expect(onSectionChange).toHaveBeenLastCalledWith(undefined, { replace: true })

    rerender(undefined)
    fireEvent.click(screen.getByRole("button", { name: "Beta" }))
    rerender("b")
    // A link in Beta opens Alpha with a new history entry, so the entry before Alpha is Beta, not the menu.
    rerender("a")
    fireEvent.click(screen.getByRole("button", { name: "Settings" }))

    expect(back).not.toHaveBeenCalled()
    expect(onSectionChange).toHaveBeenLastCalledWith(undefined, { replace: true })
  })

  // regression: #4233
  it("goes back to the menu in place when links left the section that the menu opened and then opened it again", () => {
    stubWidth("phone")
    const back = vi.spyOn(window.history, "back").mockImplementation(() => {})
    const { onSectionChange, rerender } = renderPage(undefined)
    fireEvent.click(screen.getByRole("button", { name: "Beta" }))
    rerender("b")
    // Links, for example in the bottom nav of a phone, open Alpha and then Beta, each with a new history entry.
    rerender("a")
    rerender("b")
    fireEvent.click(screen.getByRole("button", { name: "Settings" }))

    expect(back).not.toHaveBeenCalled()
    expect(onSectionChange).toHaveBeenLastCalledWith(undefined, { replace: true })
  })

  // regression: #4233
  it("keeps what the user typed in a section when the window narrows to a phone and widens again", () => {
    const resize = stubWidth("desktop")
    renderPage("b", vi.fn(), () => <Draft />)
    const key = () => screen.getByRole<HTMLInputElement>("textbox", { name: "Key" })
    fireEvent.change(key(), { target: { value: "draft" } })

    resize("phone")
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Beta")
    expect(key().value).toBe("draft")

    resize("desktop")
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Settings")
    expect(key().value).toBe("draft")
  })

  // regression: #4233
  it("shows the menu first on a phone turned sideways", () => {
    stubWidth("sideways")
    renderPage(undefined)

    expect(screen.queryByText(/content of/)).toBeNull()
    expect(screen.getByRole("button", { name: "Beta" })).not.toBeNull()
  })

  it("returns to the place in the menu where the user tapped", () => {
    stubWidth("phone")
    const { rerender } = renderPage(undefined)
    const main = screen.getByRole("main")
    Object.defineProperty(main, "scrollTop", { value: 300, writable: true })

    fireEvent.click(screen.getByRole("button", { name: "Beta" }))
    rerender("b")
    expect(main.scrollTop).toBe(0)
    main.scrollTop = 900
    // The back gesture of the phone pops the history entry, and the route shows the menu again.
    rerender(undefined)

    expect(main.scrollTop).toBe(300)
  })
})
