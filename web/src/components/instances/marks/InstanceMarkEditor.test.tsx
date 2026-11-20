/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import "@/i18n"

import { InstanceMarkEditor } from "./InstanceMarkEditor"

const markState = vi.hoisted(() => ({ setMark: () => {} }))

vi.mock("@/lib/instance-marks", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/instance-marks")>()
  const value = [actual.DEFAULT_INSTANCE_MARK, markState.setMark] as const
  return { ...actual, useInstanceMark: () => value }
})

afterEach(cleanup)

function renderEditor() {
  const onOpenChange = vi.fn()
  render(<InstanceMarkEditor open onOpenChange={onOpenChange} instanceId={1} name="main" autoLetters="MA" />)
  return onOpenChange
}

describe("InstanceMarkEditor", () => {
  // regression: #4290
  it.each([
    { name: "closes on Enter", init: {}, closes: true },
    { name: "stays open on the Enter that confirms input-method text in Safari", init: { keyCode: 229 }, closes: false },
  ])("$name in the Letters field", ({ init, closes }) => {
    const onOpenChange = renderEditor()

    fireEvent.keyDown(screen.getByLabelText("Letters"), { key: "Enter", ...init })

    expect(onOpenChange.mock.calls).toEqual(closes ? [[false]] : [])
  })

  // regression: #4290
  it("leaves the icon grid with Shift+Tab after an arrow key moved the focus", async () => {
    const user = userEvent.setup()
    renderEditor()
    screen.getByRole("button", { name: "Server" }).focus()

    await user.keyboard("{ArrowRight}")
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "NAS" }))
    await user.tab({ shift: true })

    expect(document.activeElement?.closest("[role=radiogroup]")).not.toBeNull()
  })
})
