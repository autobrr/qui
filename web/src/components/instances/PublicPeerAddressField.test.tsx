/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { PublicPeerAddressField } from "@/components/instances/PublicPeerAddressField"
import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"

afterEach(cleanup)

function input(): HTMLInputElement {
  return screen.getByLabelText("Public peer address") as HTMLInputElement
}

describe("PublicPeerAddressField", () => {
  it("says that qui cannot work out an address when it has none", () => {
    render(<TooltipProvider><PublicPeerAddressField value="" onChange={vi.fn()} /></TooltipProvider>)

    expect(input().value).toBe("")
    expect(input().placeholder).toBe("qui cannot work out an address yet")
  })

  it("shows the address that qui works out only as a placeholder, the address that the user set, and reports what the user types", () => {
    const onChange = vi.fn()
    const field = (value: string) => (
      <TooltipProvider><PublicPeerAddressField value={value} resolvedValue="203.0.113.10:51413" onChange={onChange} /></TooltipProvider>
    )
    const { rerender } = render(field(""))
    expect(input().value).toBe("")
    expect(input().placeholder).toBe("203.0.113.10:51413")

    rerender(field("seedbox.example:6881"))
    expect(input().value).toBe("seedbox.example:6881")

    fireEvent.change(input(), { target: { value: "198.51.100.4:6881" } })
    fireEvent.change(input(), { target: { value: "" } })

    expect(onChange.mock.calls).toEqual([["198.51.100.4:6881"], [""]])
  })
})
