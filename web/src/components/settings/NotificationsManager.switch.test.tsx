/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { NotificationEventDefinition, NotificationTarget, NotificationTargetRequest } from "@/types"

const mocks = vi.hoisted(() => ({
  listNotificationTargets: vi.fn(),
  updateNotificationTarget: vi.fn(),
}))

const EVENTS: NotificationEventDefinition[] = [{ type: "torrent_completed", label: "Torrent completed", description: "" }]

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
vi.mock("@/lib/api", () => ({
  api: {
    listNotificationEvents: () => Promise.resolve(EVENTS),
    listNotificationTargets: mocks.listNotificationTargets,
    updateNotificationTarget: mocks.updateNotificationTarget,
  },
}))

import { TooltipProvider } from "@/components/ui/tooltip"
import { NotificationsManager } from "./NotificationsManager"

const TARGET: NotificationTarget = {
  id: 1,
  name: "Old name",
  url: "discord://token@channel",
  enabled: true,
  eventTypes: ["torrent_completed"],
  createdAt: "",
  updatedAt: "",
}

function renderManager() {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <TooltipProvider>
        <NotificationsManager />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

async function renameTarget(name: string) {
  fireEvent.click(await screen.findByRole("button", { name: "kit.edit" }))
  const dialog = await screen.findByRole("dialog")
  fireEvent.change(within(dialog).getByLabelText("notifications.form.name"), { target: { value: name } })
  fireEvent.click(within(dialog).getByRole("button", { name: "notifications.dialog.save" }))
}

const sentRequests = () => mocks.updateNotificationTarget.mock.calls.map(([, request]) => request as NotificationTargetRequest)

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

describe("NotificationsManager switch", () => {
  it("keeps a saved edit when the switch is pressed before the list refetch answers", async () => {
    mocks.listNotificationTargets.mockResolvedValueOnce([TARGET]).mockReturnValue(new Promise(() => {}))
    mocks.updateNotificationTarget.mockImplementation((id: number, request: NotificationTargetRequest) => Promise.resolve({ ...TARGET, ...request, id }))
    renderManager()

    await renameTarget("New name")
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    fireEvent.click(screen.getByRole("switch", { name: "kit.enabled" }))

    await waitFor(() => expect(mocks.updateNotificationTarget).toHaveBeenCalledTimes(2))
    expect(sentRequests()[1]).toMatchObject({ name: "New name", enabled: false })
  })

  it("sends an edit only after the switch save of the same target ends", async () => {
    mocks.listNotificationTargets.mockResolvedValue([TARGET])
    let finishSwitch: (target: NotificationTarget) => void = () => {}
    mocks.updateNotificationTarget
      .mockImplementationOnce(() => new Promise((resolve) => { finishSwitch = resolve }))
      .mockImplementation((id: number, request: NotificationTargetRequest) => Promise.resolve({ ...TARGET, ...request, id }))
    renderManager()

    fireEvent.click(await screen.findByRole("switch", { name: "kit.enabled" }))
    await waitFor(() => expect(mocks.updateNotificationTarget).toHaveBeenCalledTimes(1))
    await renameTarget("New name")
    expect(mocks.updateNotificationTarget).toHaveBeenCalledTimes(1)

    finishSwitch({ ...TARGET, enabled: false })
    const save = within(screen.getByRole("dialog")).getByRole("button", { name: "notifications.dialog.save" })
    await waitFor(() => expect(save.hasAttribute("disabled")).toBe(false))
    fireEvent.click(save)

    await waitFor(() => expect(mocks.updateNotificationTarget).toHaveBeenCalledTimes(2))
    expect(sentRequests()[1]).toMatchObject({ name: "New name", enabled: false })
  })
})
