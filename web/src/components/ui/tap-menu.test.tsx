/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { DropdownMenuItem } from "./dropdown-menu"
import { TapMenu } from "./tap-menu"

// A row component that takes only its own props, as the mobile peer row does
function Row({ label }: { label: string }) {
  return <div>{label}</div>
}

afterEach(cleanup)

describe("TapMenu", () => {
  // regression: #3702
  it("opens on a tap on a row component", () => {
    render(<TapMenu content={<DropdownMenuItem>Copy address</DropdownMenuItem>}><Row label="peer" /></TapMenu>)

    const row = screen.getByText("peer")
    fireEvent.pointerDown(row, { pointerType: "touch", button: 0 })
    expect(screen.queryByRole("menuitem")).toBeNull()
    fireEvent.click(row)

    expect(screen.getByRole("menuitem", { name: "Copy address" })).toBeTruthy()
  })
})
