/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createRef } from "react"
import { ScrollToTopButton } from "./scroll-to-top-button"

const state = vi.hoisted(() => ({
  isFooterVisible: true,
  selection: { isAllSelected: false, selected: new Set<string>(), excluded: new Set<string>() },
}))

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock("@/contexts/MobileScrollContext", () => ({
  useMobileScroll: () => ({ isFooterVisible: state.isFooterVisible }),
}))

vi.mock("@/contexts/TorrentSelectionContext", () => ({
  useTorrentSelectionContext: () => ({ selection: state.selection }),
}))

function renderButton(extraBottomBars?: string) {
  const container = document.createElement("div")
  document.body.appendChild(container)
  const ref = createRef<HTMLElement>()
  ref.current = container
  render(<ScrollToTopButton scrollContainerRef={ref} extraBottomBars={extraBottomBars} />)
  return { container, button: screen.getByRole("button", { name: "actions.scrollToTop" }) }
}

function scrollTo(container: HTMLElement, scrollTop: number) {
  act(() => {
    container.scrollTop = scrollTop
    container.dispatchEvent(new Event("scroll"))
  })
}

function bottomOf(button: HTMLElement) {
  return button.style.getPropertyValue("--scroll-top-bottom")
}

describe("ScrollToTopButton", () => {
  beforeEach(() => {
    state.isFooterVisible = true
    state.selection = { isAllSelected: false, selected: new Set(), excluded: new Set() }
    vi.spyOn(window, "requestAnimationFrame").mockImplementation(cb => {
      cb(0)
      return 1
    })
  })

  afterEach(() => {
    cleanup()
    document.body.innerHTML = ""
    vi.restoreAllMocks()
  })

  it("shows past the threshold at every width", () => {
    const { container, button } = renderButton()
    expect(button.className).toContain("opacity-0")

    scrollTo(container, 301)
    expect(button.className).toContain("opacity-100")
    expect(button.className).not.toMatch(/(^|\s)(\w+:)?hidden(\s|$)/)

    scrollTo(container, 100)
    expect(button.className).toContain("opacity-0")
  })

  it("sits above the bottom navigation bar and the caller's bars", () => {
    const { button } = renderButton("3.5rem")
    expect(bottomOf(button)).toBe("calc(1rem + 4rem + 3.5rem + env(safe-area-inset-bottom))")
  })

  it("drops the navigation bar height when the bar slides away", () => {
    state.isFooterVisible = false
    const { button } = renderButton()
    expect(bottomOf(button)).toBe("calc(1rem + 0px + 0px + env(safe-area-inset-bottom))")
  })

  it("drops the navigation bar height in selection mode, where the bar is gone", () => {
    state.selection = { isAllSelected: false, selected: new Set(["a"]), excluded: new Set() }
    const { button } = renderButton("4rem")
    expect(bottomOf(button)).toBe("calc(1rem + 0px + 4rem + env(safe-area-inset-bottom))")
  })
})
