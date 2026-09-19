/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const { copyMock } = vi.hoisted(() => ({ copyMock: vi.fn() }))

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, options?: { snippet?: string }) => (options?.snippet ? `${key}:${options.snippet}` : key),
  }),
}))

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  copyTextToClipboard: copyMock,
}))

import { PathTemplateHelp } from "./PathTemplateHelp"
import { MOVE_PATH_TEMPLATE_VARIABLES } from "./pathTemplateVariables"

const triggerName = "preferences.workflowDialog.move.templateHelp.trigger"
const titleKey = "preferences.workflowDialog.move.templateHelp.title"

beforeEach(() => {
  copyMock.mockResolvedValue(undefined)
  // Radix popper measures the content through ResizeObserver, which jsdom lacks.
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
  copyMock.mockReset()
  cleanup()
})

function renderHelp() {
  render(<PathTemplateHelp variables={MOVE_PATH_TEMPLATE_VARIABLES} />)
  return screen.getByRole("button", { name: triggerName })
}

describe("PathTemplateHelp", () => {
  it("opens on click and lists every variable", () => {
    fireEvent.click(renderHelp())

    for (const variable of MOVE_PATH_TEMPLATE_VARIABLES) {
      expect(screen.getByText(variable.snippet)).toBeTruthy()
    }
  })

  it("copies the snippet, not the translated description", async () => {
    fireEvent.click(renderHelp())

    await act(async () => {
      fireEvent.click(screen.getByRole("button", {
        name: "preferences.workflowDialog.move.templateHelp.copySnippet:{{ sanitize .Name }}",
      }))
    })

    expect(copyMock).toHaveBeenCalledWith("{{ sanitize .Name }}")
  })

  it("stays open when a click follows the hover that opened it", () => {
    const trigger = renderHelp()

    fireEvent.pointerEnter(trigger, { pointerType: "mouse" })
    fireEvent.click(trigger)
    fireEvent.pointerLeave(trigger, { pointerType: "mouse" })

    expect(screen.getByText(titleKey)).toBeTruthy()
  })

  it("closes after the mouse leaves a hover-opened popover", () => {
    vi.useFakeTimers()
    const trigger = renderHelp()

    fireEvent.pointerEnter(trigger, { pointerType: "mouse" })
    expect(screen.getByText(titleKey)).toBeTruthy()

    fireEvent.pointerLeave(trigger, { pointerType: "mouse" })
    act(() => vi.runAllTimers())

    expect(screen.queryByText(titleKey)).toBeNull()
  })

  it("does not open from a touch pointer entering the trigger", () => {
    fireEvent.pointerEnter(renderHelp(), { pointerType: "touch" })

    expect(screen.queryByText(titleKey)).toBeNull()
  })
})
