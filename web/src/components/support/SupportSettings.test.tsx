/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import { _resetClientSettingsForTests, readRaw } from "@/lib/client-settings"
import { setSupportOpen, useSupportOpen } from "./open"
import { SHORTCUT_HIDDEN_KEY } from "./reminder"
import { SupportSettings } from "./SupportSettings"

const reminder = vi.hoisted(() => ({ settled: true, isSupporter: false, shortcutHidden: false, reminderOff: false }))

vi.mock("./reminder", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./reminder")>()),
  useSupportReminder: () => reminder,
}))
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

function OpenProbe() {
  return useSupportOpen() ? <p>dialog open</p> : null
}

const renderSection = () => render(<TooltipProvider><SupportSettings /><OpenProbe /></TooltipProvider>)
const reminderSwitch = () => screen.getByLabelText("application.support.reminder")

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
})

afterEach(() => {
  cleanup()
  setSupportOpen(false)
  Object.assign(reminder, { settled: true, isSupporter: false, shortcutHidden: false, reminderOff: false })
})

describe("SupportSettings", () => {
  it("hides the shortcut from its switch", () => {
    renderSection()

    fireEvent.click(screen.getByLabelText("application.support.shortcut"))

    expect(readRaw(SHORTCUT_HIDDEN_KEY)).toBe("true")
  })

  it("shows the reminder off while the shortcut is hidden, and still opens the dialog", () => {
    reminder.shortcutHidden = true
    renderSection()

    expect(reminderSwitch().getAttribute("aria-checked")).toBe("false")
    expect((reminderSwitch() as HTMLButtonElement).disabled).toBe(true)

    fireEvent.click(screen.getByText("application.support.open"))
    expect(screen.getByText("dialog open")).not.toBeNull()
  })

  it("shows the reminder off for a supporter", () => {
    reminder.isSupporter = true
    renderSection()

    expect(reminderSwitch().getAttribute("aria-checked")).toBe("false")
    expect(screen.getByText("application.support.reminderSupporter")).not.toBeNull()
  })

  it("takes no input before the client settings settle", () => {
    reminder.settled = false
    renderSection()

    expect((screen.getByLabelText("application.support.shortcut") as HTMLButtonElement).disabled).toBe(true)
  })
})
