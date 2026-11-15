/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { PageHeader } from "@/components/layout/PageHeader"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function stubWidth(phone: boolean) {
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: phone && query === "(max-width: 639px)",
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }))
}

/** The text of each row of the header, in page order. */
function rows(): string[] {
  const header = screen.getByRole("heading", { level: 1 }).closest("[data-page-header]")!
  return [...header.children].map(row => row.textContent ?? "")
}

describe("PageHeader", () => {
  it("puts the controls in the title row on a wide screen", () => {
    stubWidth(false)
    render(<PageHeader title="Title" description="Description" controls={<><button>One</button><button>Two</button></>} />)

    expect(rows()).toEqual(["TitleOneTwo", "Description"])
  })

  it("puts the controls in one row below the description on a phone, and keeps the actions in the title row", () => {
    stubWidth(true)
    render(<PageHeader title="Title" description="Description" actions={<button>Action</button>} controls={<><button>One</button><button>Two</button></>} />)

    expect(rows()).toEqual(["TitleAction", "Description", "OneTwo"])
  })
})
