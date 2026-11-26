/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import { _resetClientSettingsForTests, markClientSettingsUnavailable } from "@/lib/client-settings"
import type { LogSettings } from "@/types"

const mocks = vi.hoisted(() => ({
  settings: undefined as LogSettings | undefined,
  updateLogSettings: vi.fn(),
  updateLogExclusions: vi.fn(),
  translation: { t: (key: string) => key, i18n: { resolvedLanguage: "en", language: "en" } },
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.translation,
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
vi.mock("@/lib/api", () => ({
  api: {
    getLogSettings: () => Promise.resolve(mocks.settings),
    updateLogSettings: mocks.updateLogSettings,
    getLogStreamUrl: () => "/api/logs/stream",
    getLogFiles: () => Promise.resolve([]),
    getLogExclusions: () => Promise.resolve({ patterns: [] }),
    updateLogExclusions: mocks.updateLogExclusions,
  },
}))

import { LogSettingsPanel } from "./LogSettingsPanel"

class FakeEventSource {
  static instances: FakeEventSource[] = []
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  constructor() {
    FakeEventSource.instances.push(this)
  }
  close() {}
}

beforeEach(() => {
  _resetClientSettingsForTests()
  FakeEventSource.instances = []
  vi.stubGlobal("EventSource", FakeEventSource)
  Object.defineProperty(Element.prototype, "scrollIntoView", { configurable: true, value: vi.fn() })
  mocks.settings = { level: "INFO", path: "/config/logs/qui.log", maxSize: 50, maxBackups: 3 }
  mocks.updateLogSettings.mockReset().mockImplementation((update: Partial<LogSettings>) => Promise.resolve({ ...mocks.settings, ...update }))
  mocks.updateLogExclusions.mockReset().mockImplementation((update: { patterns: string[] }) => Promise.resolve(update))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><TooltipProvider><LogSettingsPanel /></TooltipProvider></QueryClientProvider>)
}

const save = () => fireEvent.click(screen.getByRole("button", { name: "logs.configuration.saveSettings" }))

describe("LogSettingsPanel", () => {
  it("saves a level change while the environment locks the max size at 0", async () => {
    mocks.settings = { ...mocks.settings!, maxSize: 0, locked: { maxSize: "environment" } }
    renderPanel()

    fireEvent.keyDown(await screen.findByRole("combobox"), { key: "ArrowDown" })
    fireEvent.click(screen.getByRole("option", { name: "DEBUG" }))
    save()

    await waitFor(() => expect(mocks.updateLogSettings).toHaveBeenCalledWith({ level: "DEBUG" }))
    expect(screen.queryByText("logs.validation.maxSize")).toBeNull()
  })

  it("reads a max size in exponent form as the whole number", async () => {
    renderPanel()

    fireEvent.change(await screen.findByLabelText("logs.configuration.maxSize"), { target: { value: "1e3" } })
    save()

    await waitFor(() => expect(mocks.updateLogSettings).toHaveBeenCalledWith({ maxSize: 1000 }))
  })

  it("mutes an entry, and does not open it, on Enter on its level badge", async () => {
    const user = userEvent.setup()
    renderPanel()
    act(() => FakeEventSource.instances[0].onmessage?.({ data: JSON.stringify({ level: "info", time: "2026-01-02T03:04:05Z", message: "tick", n: 1 }) }))

    screen.getByRole("button", { name: "info" }).focus()
    await user.keyboard("{Enter}")

    await waitFor(() => expect(mocks.updateLogExclusions).toHaveBeenCalledWith({ patterns: ["tick"] }))
    expect(screen.queryByText("logs.entryDialog.title")).toBeNull()
  })

  it("takes no level pick until the saved client settings load", async () => {
    renderPanel()
    fireEvent.click(screen.getByRole("button", { name: /logs\.viewer\.allLevels/ }))

    const checkboxes = await screen.findAllByRole("checkbox")
    expect(checkboxes.every((checkbox) => checkbox.hasAttribute("disabled"))).toBe(true)
    expect(screen.getByRole("button", { name: "logs.viewer.none" }).hasAttribute("disabled")).toBe(true)

    act(() => markClientSettingsUnavailable())
    expect(screen.getAllByRole("checkbox").some((checkbox) => checkbox.hasAttribute("disabled"))).toBe(false)
    expect(screen.getByRole("button", { name: "logs.viewer.none" }).hasAttribute("disabled")).toBe(false)
  })
})
