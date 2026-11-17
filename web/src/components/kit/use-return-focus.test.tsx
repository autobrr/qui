/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { useState } from "react"
import { afterEach, describe, expect, it } from "vitest"

import "@/i18n"

import { Editor } from "@/components/kit/Editor"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"

afterEach(cleanup)

function RowMenu() {
  const [editing, setEditing] = useState(false)
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button type="button">More actions</button>
        </DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem onSelect={() => setEditing(true)}>Edit</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Editor open={editing} onOpenChange={setEditing} title="Edit target" submitLabel="Save" onSubmit={() => {}}>
        <input aria-label="Name" />
      </Editor>
    </>
  )
}

describe("useReturnFocus", () => {
  it("returns focus to the menu button when a keyboard user cancels a dialog that a menu item opened", async () => {
    render(<RowMenu />)
    const trigger = screen.getByRole("button", { name: "More actions" })
    trigger.focus()
    fireEvent.keyDown(trigger, { key: "Enter" })
    const item = await screen.findByRole("menuitem", { name: "Edit" })
    item.focus()
    fireEvent.keyDown(item, { key: "Enter" })
    await screen.findByRole("dialog")
    expect(screen.queryByRole("menuitem")).toBeNull()

    fireEvent.keyDown(document.activeElement!, { key: "Escape" })

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    expect(document.activeElement).toBe(trigger)
  })
})
