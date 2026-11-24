/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { Download, Send, Trash2 } from "lucide-react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"

import { ResourceList, type ResourceRow } from "./ResourceList"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

interface Item {
  id: number
  name: string
  kind?: string
}

function renderList(items: Item[], row: (item: Item) => ResourceRow, group?: (item: Item) => string) {
  render(
    <TooltipProvider>
      <ResourceList label="Items" items={items} itemKey={(item) => item.id} row={row} group={group} empty={{ title: "None yet" }} />
    </TooltipProvider>
  )
}

const openMenu = (title: string) => {
  const row = screen.getByRole("listitem", { name: title })
  fireEvent.pointerDown(within(row).getByRole("button", { name: "More actions" }), { button: 0, ctrlKey: false })
  return screen.getByRole("menu")
}

describe("ResourceList", () => {
  it("adds a box to a row with select, keeps the place of the box in a row without one, and reports the new state", () => {
    const onCheckedChange = vi.fn()
    renderList([{ id: 1, name: "alpha" }, { id: 2, name: "beta" }], (item) => ({
      title: item.name,
      select: item.id === 1 ? { checked: false, onCheckedChange } : undefined,
    }))

    fireEvent.click(screen.getByRole("checkbox", { name: "alpha" }))

    expect(onCheckedChange).toHaveBeenCalledWith(true)
    expect(screen.queryByRole("checkbox", { name: "beta" })).toBeNull()
    // The row without a box keeps an empty column, so that the two rows stay in line.
    const beta = screen.getByRole("listitem", { name: "beta" })
    expect(beta.firstElementChild?.childElementCount).toBe(0)
  })

  it("puts the items under their headings, in the order of the first item of each heading", () => {
    renderList(
      [{ id: 1, name: "one", kind: "Radarr" }, { id: 2, name: "two", kind: "Sonarr" }, { id: 3, name: "three", kind: "Radarr" }],
      (item) => ({ title: item.name }),
      (item) => item.kind ?? ""
    )

    expect(screen.getAllByRole("heading").map((heading) => heading.textContent)).toEqual(["Radarr", "Sonarr"])
    const radarr = screen.getByRole("list", { name: "Radarr" })
    expect(within(radarr).getAllByRole("listitem").map((item) => item.textContent)).toEqual(["one", "three"])
  })

  it("lists the destructive steps last in the menu, under a line", () => {
    renderList([{ id: 1, name: "alpha" }], (item) => ({
      title: item.name,
      actions: [
        { label: "Delete", icon: Trash2, destructive: true, onSelect: () => {} },
        { label: "Test", icon: Send, onSelect: () => {} },
      ],
    }))

    const menu = openMenu("alpha")

    expect(within(menu).getAllByRole("menuitem").map((item) => item.textContent)).toEqual(["Test", "Delete"])
    const children = [...menu.children]
    const separator = within(menu).getByRole("separator")
    expect(children.indexOf(separator)).toBe(children.indexOf(within(menu).getByRole("menuitem", { name: "Delete" })) - 1)
  })

  it("shows a lone step as a button on a phone, with no menu", () => {
    const onSelect = vi.fn()
    renderList([{ id: 1, name: "qui.log" }], (item) => ({ title: item.name, actions: [{ label: "Download", icon: Download, onSelect }] }))
    const row = screen.getByRole("listitem", { name: "qui.log" })

    // A wide screen shows the step as a button too, so the row has two Download buttons.
    const buttons = within(row).getAllByRole("button", { name: "Download" })
    fireEvent.click(buttons[buttons.length - 1])

    expect(onSelect).toHaveBeenCalledOnce()
    expect(within(row).queryByRole("button", { name: "More actions" })).toBeNull()
  })

  it("opens the item from the row and from its Edit step", () => {
    const onOpen = vi.fn()
    renderList([{ id: 1, name: "search" }], (item) => ({ title: item.name, onOpen }))
    const row = screen.getByRole("listitem", { name: "search" })

    fireEvent.click(within(row).getByRole("button", { name: "search" }))
    // A wide screen shows Edit as a button, and a phone as an arrow with the same name.
    for (const button of within(row).getAllByRole("button", { name: "Edit" })) fireEvent.click(button)

    expect(onOpen).toHaveBeenCalledTimes(3)
  })

  it("opens the details of a row under it on a press, and closes them on a second press", () => {
    const onOpen = vi.fn()
    renderList([{ id: 1, name: "search" }], (item) => ({ title: item.name, onOpen, expand: <p>the request</p> }))
    const toggle = within(screen.getByRole("listitem", { name: "search" })).getByRole("button", { name: "search" })

    expect(toggle.getAttribute("aria-expanded")).toBe("false")
    expect(screen.queryByText("the request")).toBeNull()

    fireEvent.click(toggle)
    expect(toggle.getAttribute("aria-expanded")).toBe("true")
    expect(document.getElementById(toggle.getAttribute("aria-controls") ?? "")?.textContent).toBe("the request")

    fireEvent.click(toggle)
    expect(screen.queryByText("the request")).toBeNull()
    expect(onOpen).not.toHaveBeenCalled()
  })
})
