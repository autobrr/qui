/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { SectionedPage } from "./SectionedPage"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const GROUPS = [{ label: "General", sections: [{ value: "a", label: "Alpha" }, { value: "b", label: "Beta" }] }] as const
type Section = "a" | "b"

function stubWidth(kind: "phone" | "desktop") {
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: kind === "phone" && query === "(max-width: 767px)",
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }))
}

function renderPage(section: Section | undefined, onSectionChange = vi.fn()) {
  const page = (current: Section | undefined) => (
    <main>
      <SectionedPage title="Settings" description="All settings" groups={GROUPS} section={current} defaultSection="a" onSectionChange={onSectionChange}>
        {(shown) => <p>content of {shown}</p>}
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

  it("shows the menu first on a phone and adds a history entry for the section that the user taps", () => {
    stubWidth("phone")
    const { onSectionChange } = renderPage(undefined)

    expect(screen.queryByText(/content of/)).toBeNull()
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Settings")
    fireEvent.click(screen.getByRole("button", { name: "Beta" }))

    expect(onSectionChange).toHaveBeenCalledWith("b", { replace: false })
  })

  it("goes back in history to the menu after a tap in the menu", () => {
    stubWidth("phone")
    const back = vi.spyOn(window.history, "back").mockImplementation(() => {})
    const { onSectionChange, rerender } = renderPage(undefined)
    fireEvent.click(screen.getByRole("button", { name: "Beta" }))
    rerender("b")

    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Beta")
    fireEvent.click(screen.getByRole("button", { name: "Settings" }))

    expect(back).toHaveBeenCalledOnce()
    expect(onSectionChange).toHaveBeenCalledTimes(1)
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

  it("returns to the menu in place when a link opened the section", () => {
    stubWidth("phone")
    const back = vi.spyOn(window.history, "back").mockImplementation(() => {})
    const { onSectionChange } = renderPage("b")

    expect(screen.getByText("content of b")).not.toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Settings" }))

    expect(back).not.toHaveBeenCalled()
    expect(onSectionChange).toHaveBeenCalledWith(undefined, { replace: true })
  })
})
