/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const stubs = vi.hoisted(() => ({
  translation: { t: (key: string) => key },
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => stubs.translation,
}))

import { TooltipProvider } from "@/components/ui/tooltip"
import type { RunState } from "@/lib/run-state"
import { RunFooter } from "../RunFooter"

afterEach(cleanup)

function footer(state: RunState, onCancel: () => void) {
  return (
    <RunFooter
      state={state}
      runLabel="Run now"
      runIcon={null}
      onRun={() => {}}
      onCancel={onCancel}
      cancelDialog={{ title: "Cancel run?", description: "The run stops." }}
    >
      <button type="button">Save</button>
    </RunFooter>
  )
}

describe("RunFooter", () => {
  it("cancels a run only after the user confirms", () => {
    const onCancel = vi.fn()
    const { rerender } = render(footer({ kind: "running" }, onCancel))

    fireEvent.click(screen.getByRole("button", { name: "automation.cancel" }))
    fireEvent.click(screen.getByRole("button", { name: "cancelDialog.keepRunning" }))
    expect(onCancel).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole("button", { name: "automation.cancel" }))
    fireEvent.click(screen.getByRole("button", { name: "cancelDialog.cancelRun" }))
    expect(onCancel).toHaveBeenCalledTimes(1)

    rerender(footer({ kind: "cancelling" }, onCancel))
    expect(screen.getByRole("button", { name: "automation.stopping" }).hasAttribute("disabled")).toBe(true)
  })

  // regression: #4263
  it("shows why the run is blocked on a tap of the disabled run button", () => {
    const touchWindow = window as Window & { ontouchstart?: unknown }
    touchWindow.ontouchstart = null
    try {
      render(<TooltipProvider>{footer({ kind: "blocked", reason: "Add an indexer first" }, () => {})}</TooltipProvider>)

      // A disabled Button has pointer-events: none, so the tap lands on the wrapper around it.
      const wrapper = screen.getByRole("button", { name: "Run now" }).parentElement!
      fireEvent.pointerDown(wrapper, { pointerType: "touch" })
      fireEvent.click(wrapper)

      expect(screen.getByRole("tooltip").textContent).toContain("Add an indexer first")
    } finally {
      vi.unstubAllGlobals()
      delete touchWindow.ontouchstart
    }
  })
})
