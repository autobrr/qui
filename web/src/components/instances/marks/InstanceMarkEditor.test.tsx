/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { INSTANCE_MARK_COLORS, type InstanceMark } from "@/lib/instance-marks"

import { InstanceMarkEditor } from "./InstanceMarkEditor"

const markState = vi.hoisted(() => ({ value: null as [InstanceMark, (mark: InstanceMark) => void] | null }))

vi.mock("@/lib/instance-marks", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/instance-marks")>()
  const fallback = [actual.DEFAULT_INSTANCE_MARK, () => {}] as const
  return { ...actual, useInstanceMark: () => markState.value ?? fallback }
})

afterEach(() => {
  cleanup()
  markState.value = null
})

function withMark(mark: InstanceMark) {
  const setMark = vi.fn()
  markState.value = [mark, setMark]
  return setMark
}

function renderEditor() {
  const onOpenChange = vi.fn()
  render(<InstanceMarkEditor open onOpenChange={onOpenChange} instanceId={1} name="main" autoLetters="MA" />, { wrapper: TooltipProvider })
  return onOpenChange
}

describe("InstanceMarkEditor", () => {
  it.each([
    { name: "closes on Enter", init: {}, closes: true },
    { name: "stays open on the Enter that confirms input-method text in Safari", init: { keyCode: 229 }, closes: false },
  ])("$name in the Letters field", ({ init, closes }) => {
    const onOpenChange = renderEditor()

    fireEvent.keyDown(screen.getByLabelText("Letters"), { key: "Enter", ...init })

    expect(onOpenChange.mock.calls).toEqual(closes ? [[false]] : [])
  })

  it("leaves the icon grid with Shift+Tab after an arrow key moved the focus", async () => {
    const user = userEvent.setup()
    renderEditor()
    screen.getByRole("button", { name: "Server" }).focus()

    await user.keyboard("{ArrowRight}")
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "NAS" }))
    await user.tab({ shift: true })

    expect(document.activeElement?.closest("[role=radiogroup]")).not.toBeNull()
  })

  it.each([
    { name: "keeps the icon when the user types a space", typed: " ", saves: [] },
    { name: "replaces the icon with the letters that the user types", typed: "x", saves: [[{ icon: null, color: null, letters: "X" }]] },
  ])("$name in the Letters field", ({ typed, saves }) => {
    const setMark = withMark({ icon: "cat", color: null, letters: "" })
    renderEditor()

    fireEvent.change(screen.getByLabelText("Letters"), { target: { value: typed } })

    expect(setMark.mock.calls).toEqual(saves)
  })

  it("selects the letters on focus, so the next two letters replace them", () => {
    withMark({ icon: null, color: null, letters: "AB" })
    renderEditor()
    const field = screen.getByLabelText<HTMLInputElement>("Letters")

    fireEvent.focus(field)

    expect([field.selectionStart, field.selectionEnd]).toEqual([0, 2])
  })

  it("saves input-method text once, when the character is done", () => {
    const setMark = withMark({ icon: null, color: null, letters: "" })
    renderEditor()
    const field = screen.getByLabelText("Letters")

    fireEvent.compositionStart(field)
    fireEvent.change(field, { target: { value: "ㄱ" } })
    expect(setMark).not.toHaveBeenCalled()
    fireEvent.compositionEnd(field, { target: { value: "가" } })

    expect(setMark.mock.calls).toEqual([[{ icon: null, color: null, letters: "가" }]])
  })

  it("picks the next color when an arrow key moves the focus in the color group", () => {
    const setMark = withMark({ icon: null, color: null, letters: "" })
    renderEditor()

    fireEvent.keyDown(screen.getByRole("radio", { name: "No color" }), { key: "ArrowRight" })

    expect(document.activeElement).toBe(screen.getByRole("radio", { name: "Sky" }))
    expect(setMark.mock.calls).toEqual([[{ icon: null, color: INSTANCE_MARK_COLORS[0], letters: "" }]])
  })

  it("moves the focus to Automatic after a reset", async () => {
    const user = userEvent.setup()
    withMark({ icon: "cat", color: "teal", letters: "" })
    renderEditor()

    await user.click(screen.getByRole("button", { name: "Reset to default" }))

    expect(document.activeElement).toBe(screen.getByRole("button", { name: /Automatic/ }))
  })
})
