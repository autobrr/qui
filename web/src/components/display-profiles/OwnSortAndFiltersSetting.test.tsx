/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import "@/i18n"
import { _resetClientSettingsForTests, markClientSettingsUnavailable, createDisplayProfile, getDisplayProfiles, useDisplayProfiles } from "@/lib/client-settings"

vi.mock("@/components/ui/field-help", () => ({
  FieldHelp: ({ children }: { children: ReactNode }) => <aside>{children}</aside>,
}))

import { OwnSortAndFiltersSetting } from "./OwnSortAndFiltersSetting"

function Harness({ profileId }: { profileId: string }) {
  const profile = useDisplayProfiles().profiles.find((candidate) => candidate.id === profileId)!
  return <OwnSortAndFiltersSetting profile={profile} />
}

function renderFor(profileId: string) {
  render(<Harness profileId={profileId} />)
}

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
  markClientSettingsUnavailable()
})

afterEach(cleanup)

describe("OwnSortAndFiltersSetting", () => {
  it("turns Own sort and filters on, and then warns that turning it off deletes the own values", () => {
    const phone = createDisplayProfile("Phone")
    renderFor(phone)
    const toggle = screen.getByRole("switch", { name: "Own sort and filters" })
    expect(toggle.getAttribute("aria-checked")).toBe("false")
    expect(screen.queryByText(/Turning this off deletes/)).toBeNull()

    act(() => fireEvent.click(toggle))

    expect(getDisplayProfiles().active.ownSortAndFilters).toBe(true)
    expect(screen.getByRole("switch", { name: "Own sort and filters" }).getAttribute("aria-checked")).toBe("true")
    expect(screen.getByText("Turning this off deletes the own sort and filters of Phone.")).not.toBeNull()
  })

  it("shows on Default that it always shares, with no switch", () => {
    renderFor("default")

    expect(screen.queryByRole("switch")).toBeNull()
    expect(screen.getByText("Always shares sort and filters")).not.toBeNull()
    expect(screen.getByRole("complementary").textContent).toBe(
      "Default always shares the sort, the filters, and the other view choices with every profile that does not keep its own. To keep your own, make another profile and turn on Own sort and filters."
    )
  })
})
