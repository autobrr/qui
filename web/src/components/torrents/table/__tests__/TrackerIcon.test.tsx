/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TrackerIcon } from "@/components/torrents/table/TrackerIcon"
import { cleanup, fireEvent, render } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

afterEach(cleanup)

describe("TrackerIcon", () => {
  it("renders the favicon image when a src is provided", () => {
    const { container } = render(
      <TrackerIcon title="Example" fallback="E" src="https://icons.example/e.png" />
    )
    const img = container.querySelector("img")
    expect(img).not.toBeNull()
    expect(img?.getAttribute("src")).toBe("https://icons.example/e.png")
  })

  it("renders the fallback glyph when src is null", () => {
    const { container } = render(
      <TrackerIcon title="Example" fallback="E" src={null} />
    )
    expect(container.querySelector("img")).toBeNull()
    expect(container.textContent).toContain("E")
  })

  it("falls back to the glyph when the image errors", () => {
    const { container } = render(
      <TrackerIcon title="Example" fallback="E" src="https://icons.example/e.png" />
    )
    const img = container.querySelector("img")
    expect(img).not.toBeNull()
    fireEvent.error(img!)
    expect(container.querySelector("img")).toBeNull()
    expect(container.textContent).toContain("E")
  })

  it("resets the error state when src changes", () => {
    const { container, rerender } = render(
      <TrackerIcon title="Example" fallback="E" src="https://icons.example/e.png" />
    )
    fireEvent.error(container.querySelector("img")!)
    expect(container.querySelector("img")).toBeNull()

    rerender(<TrackerIcon title="Example" fallback="E" src="https://icons.example/e2.png" />)
    const img = container.querySelector("img")
    expect(img).not.toBeNull()
    expect(img?.getAttribute("src")).toBe("https://icons.example/e2.png")
  })

  it("applies the glyph size class for the requested size", () => {
    const { container } = render(
      <TrackerIcon title="Example" fallback="E" src={null} size="xs" />
    )
    const glyphBox = container.querySelector("[title=\"Example\"]")?.firstElementChild
    expect(glyphBox?.className).toContain("h-3 w-3")
  })

  it("defaults to the medium size class", () => {
    const { container } = render(
      <TrackerIcon title="Example" fallback="E" src={null} />
    )
    const glyphBox = container.querySelector("[title=\"Example\"]")?.firstElementChild
    expect(glyphBox?.className).toContain("h-4 w-4")
  })
})
