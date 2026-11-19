/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it } from "vitest"

import "@/i18n"
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { _resetClientSettingsForTests, markClientSettingsUnavailable, createDisplayProfile, recordColumnSetupInstance } from "@/lib/client-settings"

import { ColumnsMenuHeader } from "./ColumnsMenuHeader"
import { SameColumnsSwitch } from "./SameColumnsSwitch"

const instances = [{ id: 1, name: "seedbox" }, { id: 2, name: "home" }]

function renderMenu(instanceId: number, instanceName?: string) {
  render(
    <DropdownMenu open>
      <DropdownMenuTrigger>columns</DropdownMenuTrigger>
      <DropdownMenuContent>
        <ColumnsMenuHeader instanceId={instanceId} instanceName={instanceName} />
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

const heading = () => screen.getByTestId("columns-menu-heading").textContent

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
  markClientSettingsUnavailable()
  // A stored Column setup of an instance makes the computed switch start off.
  localStorage.setItem("qui-column-visibility:1", JSON.stringify({ ratio: true }))
})

afterEach(cleanup)

describe("ColumnsMenuHeader", () => {
  it("names the profile and the instance on screen, then every instance after the switch goes on", () => {
    createDisplayProfile("Laptop")
    renderMenu(1, "seedbox")
    expect(heading()).toBe("Columns in Laptop · seedbox only")

    const toggle = screen.getByRole("menuitemcheckbox", { name: /Same columns on every instance/ })
    expect(toggle.getAttribute("aria-checked")).toBe("false")
    expect(toggle.textContent).toContain("Sort and column filters stay per instance.")
    act(() => fireEvent.click(toggle))

    expect(heading()).toBe("Columns in Laptop · every instance")
    expect(toggle.getAttribute("aria-checked")).toBe("true")
  })

  it("takes a change only after the first read of the server ends", () => {
    _resetClientSettingsForTests()
    renderMenu(1, "seedbox")
    const toggle = screen.getByRole("menuitemcheckbox", { name: /Same columns on every instance/ })

    act(() => fireEvent.click(toggle))
    expect(toggle.getAttribute("aria-checked")).toBe("false")

    act(() => markClientSettingsUnavailable())
    act(() => fireEvent.click(toggle))
    expect(toggle.getAttribute("aria-checked")).toBe("true")
  })

  it("names the Unified view", () => {
    renderMenu(0)
    expect(heading()).toBe("Columns in Default · Unified view only")
  })
})

describe("SameColumnsSwitch", () => {
  it("names the instance that turning it on copies, and the instances that share one setup once it is on", () => {
    recordColumnSetupInstance(1)
    render(<SameColumnsSwitch profileId="default" instances={instances} />)
    const toggle = screen.getByRole("switch", { name: "Same columns on every instance" })
    const help = () => document.getElementById(toggle.getAttribute("aria-describedby")!)?.textContent
    expect(help()).toBe("Each instance keeps its own. Turning this on gives every instance the seedbox columns.")

    act(() => fireEvent.click(toggle))

    expect(help()).toBe("One Column setup for seedbox, home, and the Unified view.")
  })
})
