/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { SectionedPage } from "./SectionedPage"

afterEach(cleanup)

describe("SectionedPage", () => {
  it("opens a new section at the top of the page", () => {
    render(
      <main>
        <SectionedPage
          navLabel="Settings"
          groups={[{ sections: [{ value: "a", label: "A" }, { value: "b", label: "B" }] }]}
          activeSection="a"
          onSectionChange={() => {}}
        >
          <p>content</p>
        </SectionedPage>
      </main>
    )
    const main = screen.getByRole("main")
    Object.defineProperty(main, "scrollTop", { value: 500, writable: true })

    fireEvent.click(screen.getByRole("button", { name: "B" }))

    expect(main.scrollTop).toBe(0)
  })
})
