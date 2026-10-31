/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { Automation } from "@/types"

const mocks = vi.hoisted(() => {
  const t = (key: string) => key
  return {
    translation: { t, i18n: { t, language: "en" } },
    apply: vi.fn(),
    toast: { loading: vi.fn(() => "toast-id"), success: vi.fn(), error: vi.fn() },
  }
})

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.translation,
}))
vi.mock("sonner", () => ({ toast: mocks.toast }))
vi.mock("@/lib/api", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/lib/api")>(),
  api: { applyAutomationNow: mocks.apply },
}))

import { APIError } from "@/lib/api"
import { RunWorkflowNowDialog } from "./RunWorkflowNowDialog"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

const rule: Automation = {
  id: 5,
  instanceId: 2,
  name: "Pause and tag",
  trackerPattern: "*",
  conditions: { schemaVersion: "1", pause: { enabled: true }, tags: [{ enabled: true, tags: ["x"], mode: "full" }], delete: { enabled: false } },
  enabled: false,
  dryRun: false,
  notify: false,
  sortOrder: 0,
}

function renderDialog(onClose = vi.fn(), target = rule) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <RunWorkflowNowDialog target={{ instanceId: 2, rule: target }} onClose={onClose} />
    </QueryClientProvider>
  )
  return onClose
}

describe("RunWorkflowNowDialog", () => {
  it("lists the enabled actions and applies nothing on cancel", () => {
    const onClose = renderDialog()

    const items = screen.getAllByRole("listitem").map(item => item.textContent)
    expect(items).toEqual(["preferences.workflowDialog.actions.pause", "preferences.workflowDialog.actions.tag"])

    fireEvent.click(screen.getByRole("button", { name: "preferences.runWorkflowNow.cancel" }))
    expect(onClose).toHaveBeenCalled()
    expect(mocks.apply).not.toHaveBeenCalled()
  })

  it("runs the saved rule on confirm", async () => {
    mocks.apply.mockResolvedValue({ status: "applied", activities: [{ outcome: "success" }] })
    renderDialog()

    fireEvent.click(screen.getByRole("button", { name: "preferences.runWorkflowNow.confirm" }))

    await waitFor(() => expect(mocks.apply).toHaveBeenCalledWith(2, 5))
    await waitFor(() => expect(mocks.toast.success).toHaveBeenCalledWith("preferences.runWorkflowNow.toast.completed", { id: "toast-id" }))
  })

  it("says when the run changed nothing", async () => {
    mocks.apply.mockResolvedValue({ status: "applied" })
    renderDialog()

    fireEvent.click(screen.getByRole("button", { name: "preferences.runWorkflowNow.confirm" }))

    await waitFor(() => expect(mocks.toast.success).toHaveBeenCalledWith("preferences.runWorkflowNow.toast.noChanges", { id: "toast-id" }))
  })

  it("lists an auto-management action that turns management off", () => {
    renderDialog(vi.fn(), { ...rule, conditions: { schemaVersion: "1", autoManagement: { enabled: false } } })

    expect(screen.getAllByRole("listitem").map(item => item.textContent)).toEqual(["preferences.workflowDialog.actions.autoManagement"])
  })

  it("does not claim no changes while exports still run", async () => {
    mocks.apply.mockResolvedValue({ status: "applied" })
    renderDialog(vi.fn(), { ...rule, conditions: { schemaVersion: "1", exportToInstance: { enabled: true, targetInstanceId: 3, savePath: "" } } })

    fireEvent.click(screen.getByRole("button", { name: "preferences.runWorkflowNow.confirm" }))

    await waitFor(() => expect(mocks.toast.success).toHaveBeenCalledWith("preferences.runWorkflowNow.toast.completed", { id: "toast-id" }))
  })

  it("shows the run-in-progress message on 409", async () => {
    mocks.apply.mockRejectedValue(new APIError("A run is in progress. Try again in a moment.", 409))
    renderDialog()

    fireEvent.click(screen.getByRole("button", { name: "preferences.runWorkflowNow.confirm" }))

    await waitFor(() => expect(mocks.toast.error).toHaveBeenCalledWith("preferences.runWorkflowNow.toast.inProgress", { id: "toast-id" }))
  })
})
