/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { useTapTooltip } from "@/components/statistics/useTapTooltip"

function Chart() {
  const tooltip = useTapTooltip()
  return (
    <>
      <div {...tooltip.wrapper} data-testid="chart" data-active={String(tooltip.tooltip.active)}>
        <span>bar</span>
        {/* recharts puts its tooltip in a box with this class inside the chart. */}
        <div className="recharts-tooltip-wrapper"><span>tooltip</span></div>
      </div>
      <button type="button">outside</button>
    </>
  )
}

function renderChart(hover: boolean): HTMLElement {
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: !hover && query === "(hover: none)", addEventListener: () => {}, removeEventListener: () => {} }))
  render(<Chart />)
  return screen.getByTestId("chart")
}

function tap(element: Element) {
  fireEvent.pointerDown(element)
  fireEvent.click(element)
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("useTapTooltip", () => {
  it("leaves the tooltip to recharts on a screen with hover", () => {
    const chart = renderChart(true)
    tap(screen.getByText("bar"))

    expect(chart.getAttribute("data-active")).toBe("undefined")
  })

  it("opens the tooltip on a tap on the chart, and keeps it open on the next tap on the chart", () => {
    const chart = renderChart(false)
    expect(chart.getAttribute("data-active")).toBe("false")

    tap(screen.getByText("bar"))
    expect(chart.getAttribute("data-active")).toBe("true")

    tap(screen.getByText("bar"))
    expect(chart.getAttribute("data-active")).toBe("true")
  })

  it("closes the tooltip on a tap on the tooltip", () => {
    const chart = renderChart(false)
    tap(screen.getByText("bar"))

    tap(screen.getByText("tooltip"))

    expect(chart.getAttribute("data-active")).toBe("false")
  })

  it("closes the tooltip on a tap outside the chart", () => {
    const chart = renderChart(false)
    tap(screen.getByText("bar"))

    tap(screen.getByText("outside"))

    expect(chart.getAttribute("data-active")).toBe("false")
  })

  it("closes the tooltip when the page scrolls", () => {
    const chart = renderChart(false)
    tap(screen.getByText("bar"))

    fireEvent.scroll(window)

    expect(chart.getAttribute("data-active")).toBe("false")
  })
})
