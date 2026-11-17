/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createRef } from "react"
import { ScrollToTopButton } from "./scroll-to-top-button"

const noSelection = { isAllSelected: false, selected: new Set<string>(), excluded: new Set<string>() }
const oneSelected = { isAllSelected: false, selected: new Set(["a"]), excluded: new Set<string>() }

// Each mock returns the same object until a test swaps it.
const state = vi.hoisted(() => ({
  isMobile: true,
  mobileScroll: { isFooterVisible: true },
  selectionContext: { selection: {} as unknown },
  translation: { t: (key: string) => key },
}))

vi.mock("react-i18next", () => ({
  useTranslation: () => state.translation,
}))

vi.mock("@/hooks/useMediaQuery", () => ({
  useIsMobile: () => state.isMobile,
}))

vi.mock("@/contexts/MobileScrollContext", () => ({
  useMobileScroll: () => state.mobileScroll,
}))

vi.mock("@/contexts/TorrentSelectionContext", () => ({
  useTorrentSelectionContext: () => state.selectionContext,
}))

function renderButton(extraBottomBarsRem?: number) {
  const container = document.createElement("div")
  const ref = createRef<HTMLElement>()
  ref.current = container
  render(<ScrollToTopButton scrollContainerRef={ref} extraBottomBarsRem={extraBottomBarsRem} />)
  return container
}

function button() {
  return screen.getByRole("button", { hidden: true })
}

function shownButton() {
  return screen.queryByRole("button", { name: "actions.scrollToTop" })
}

function scrollTo(container: HTMLElement, scrollTop: number) {
  act(() => {
    container.scrollTop = scrollTop
    container.dispatchEvent(new Event("scroll"))
  })
}

function bottomOf(el: HTMLElement) {
  return el.style.bottom
}

// regression: #4264 (the CSS breakpoints themselves need a browser; jsdom applies none)
describe("ScrollToTopButton", () => {
  beforeEach(() => {
    state.isMobile = true
    state.mobileScroll = { isFooterVisible: true }
    state.selectionContext = { selection: noSelection }
    vi.spyOn(window, "requestAnimationFrame").mockImplementation(cb => {
      cb(0)
      return 1
    })
  })

  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it("shows only past the scroll threshold, and stays out of the tab order until then", () => {
    const container = renderButton()
    expect(shownButton()).toBeNull()
    expect(button().tabIndex).toBe(-1)

    scrollTo(container, 301)
    expect(shownButton()).not.toBeNull()
    expect(button().tabIndex).toBe(0)

    scrollTo(container, 100)
    expect(shownButton()).toBeNull()
  })

  it("sits above the bottom navigation bar and the caller's bars", () => {
    renderButton(3.5)
    expect(bottomOf(button())).toBe("calc(8.5rem + env(safe-area-inset-bottom))")
  })

  it("drops to the screen edge when the bars slide away", () => {
    state.mobileScroll = { isFooterVisible: false }
    renderButton()
    expect(bottomOf(button())).toBe("calc(1.5rem + env(safe-area-inset-bottom))")
  })

  it("drops the navigation bar height in selection mode, where the bar is gone", () => {
    state.selectionContext = { selection: oneSelected }
    renderButton(4)
    expect(bottomOf(button())).toBe("calc(5rem + env(safe-area-inset-bottom))")
  })

  it("sets no screen offset outside the card list", () => {
    state.isMobile = false
    renderButton()
    expect(bottomOf(button())).toBe("")
  })
})
