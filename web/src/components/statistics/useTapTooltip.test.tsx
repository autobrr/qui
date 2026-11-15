/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { useTapTooltip } from "@/components/statistics/useTapTooltip"

// The plot of the test chart, in client coordinates. Below it are the axis and the legend.
const PLOT = { left: 40, top: 0, right: 300, bottom: 200 }
const ON_PLOT = { clientX: 150, clientY: 100 }
const BELOW_PLOT = { clientX: 150, clientY: 230 }

function Chart() {
  const tooltip = useTapTooltip()
  return (
    <>
      <div {...tooltip.wrapper} data-testid="chart" data-active={String(tooltip.tooltip.active)}>
        {/* recharts draws the grid over the plot, and puts its tooltip in a box with this class inside the chart. */}
        <svg><g className="recharts-cartesian-grid" data-testid="grid" /></svg>
        <span>axis</span>
        <div className="recharts-tooltip-wrapper"><span>tooltip</span></div>
      </div>
      <button type="button">outside</button>
    </>
  )
}

function renderChart(hover: boolean): HTMLElement {
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: !hover && query === "(hover: none)", addEventListener: () => {}, removeEventListener: () => {} }))
  // jsdom has no layout, so it has no scrollIntoView.
  Element.prototype.scrollIntoView = vi.fn()
  render(<Chart />)
  screen.getByTestId("grid").getBoundingClientRect = () => ({ ...PLOT, x: PLOT.left, y: PLOT.top, width: PLOT.right - PLOT.left, height: PLOT.bottom - PLOT.top, toJSON: () => PLOT })
  return screen.getByTestId("chart")
}

function tap(element: Element, at: { clientX: number; clientY: number } = ON_PLOT) {
  fireEvent.pointerDown(element, at)
  fireEvent.click(element, at)
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.useRealTimers()
  delete (Element.prototype as { scrollIntoView?: unknown }).scrollIntoView
})

describe("useTapTooltip", () => {
  // regression: #4179
  it("leaves the tooltip to recharts on a screen with hover", () => {
    const chart = renderChart(true)
    tap(chart)

    expect(chart.getAttribute("data-active")).toBe("undefined")
  })

  // regression: #4179
  it("opens the tooltip on a tap on the plot, and keeps it open on the next tap on the plot", () => {
    const chart = renderChart(false)
    expect(chart.getAttribute("data-active")).toBe("false")

    tap(chart)
    expect(chart.getAttribute("data-active")).toBe("true")

    tap(chart, { clientX: 200, clientY: 50 })
    expect(chart.getAttribute("data-active")).toBe("true")
  })

  // regression: #4179
  it("opens no tooltip on a tap below the plot, and closes an open one", () => {
    const chart = renderChart(false)
    tap(screen.getByText("axis"), BELOW_PLOT)
    expect(chart.getAttribute("data-active")).toBe("false")

    tap(chart)
    tap(screen.getByText("axis"), BELOW_PLOT)

    expect(chart.getAttribute("data-active")).toBe("false")
  })

  // regression: #4179
  it("closes the tooltip on a tap on the tooltip", () => {
    const chart = renderChart(false)
    tap(chart)

    tap(screen.getByText("tooltip"))

    expect(chart.getAttribute("data-active")).toBe("false")
  })

  // regression: #4179
  it("closes the tooltip on a tap outside the chart", () => {
    const chart = renderChart(false)
    tap(chart)

    tap(screen.getByText("outside"))

    expect(chart.getAttribute("data-active")).toBe("false")
  })

  // regression: #4179
  it("leaves the tooltip to recharts on a touch screen after a key on the chart, and opens it on the next tap again", () => {
    const chart = renderChart(false)

    fireEvent.keyDown(chart, { key: "ArrowRight" })
    expect(chart.getAttribute("data-active")).toBe("undefined")

    tap(chart)
    expect(chart.getAttribute("data-active")).toBe("true")
  })

  // regression: #4179
  it("scrolls the chart into view above the bottom nav on a tap that opens the tooltip, and keeps it open through that scroll", () => {
    const chart = renderChart(false)

    tap(chart)

    expect(chart.scrollIntoView).toHaveBeenCalledWith({ block: "nearest", behavior: "smooth" })
    expect(chart.style.scrollMarginBottom).toBe("calc(4rem + env(safe-area-inset-bottom))")
    fireEvent.scroll(window)
    expect(chart.getAttribute("data-active")).toBe("true")
  })

  // regression: #4179
  it("closes the tooltip on a scroll of the user right after the tap that opened it", () => {
    const chart = renderChart(false)
    tap(chart)

    fireEvent.touchStart(chart)
    fireEvent.scroll(window)

    expect(chart.getAttribute("data-active")).toBe("false")
  })

  // regression: #4179
  it("closes the tooltip when the page scrolls", () => {
    vi.useFakeTimers({ toFake: ["Date"] })
    const chart = renderChart(false)
    tap(chart)
    // A scroll after the scroll of the tap is one of the user.
    vi.setSystemTime(Date.now() + 1000)

    fireEvent.scroll(window)

    expect(chart.getAttribute("data-active")).toBe("false")
  })
})
