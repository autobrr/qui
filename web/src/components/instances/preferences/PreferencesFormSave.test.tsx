/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import type { ComponentType } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import { server } from "@/test/msw/server"
import type { AppPreferences } from "@/types"

import { AdvancedNetworkForm } from "./AdvancedNetworkForm"
import { ConnectionSettingsForm } from "./ConnectionSettingsForm"
import { FileManagementForm } from "./FileManagementForm"
import { NetworkDiscoveryForm } from "./NetworkDiscoveryForm"
import { QueueManagementForm } from "./QueueManagementForm"
import { SeedingLimitsForm } from "./SeedingLimitsForm"
import { SpeedLimitsForm } from "./SpeedLimitsForm"

const { toast, i18n, fieldVisibility, capabilities } = vi.hoisted(() => ({
  toast: { success: vi.fn(), error: vi.fn() },
  i18n: { t: (key: string) => key },
  fieldVisibility: {},
  capabilities: { data: undefined },
}))

vi.mock("sonner", () => ({ toast }))
vi.mock("react-i18next", () => ({ useTranslation: () => i18n, Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey }))
vi.mock("@/hooks/useQBittorrentAppInfo", () => ({ useQBittorrentFieldVisibility: () => fieldVisibility }))
vi.mock("@/hooks/useInstanceCapabilities", () => ({ useInstanceCapabilities: () => capabilities }))

type FormComponent = ComponentType<{ instanceId: number; onSuccess?: () => void }>

// Each form with one switch the edit tests flip: its preference field and its label key.
const forms: [string, FormComponent, keyof AppPreferences, string][] = [
  ["SeedingLimitsForm", SeedingLimitsForm, "max_ratio_enabled", "preferences.seedingLimits.enableShareRatioLimit"],
  ["SpeedLimitsForm", SpeedLimitsForm, "scheduler_enabled", "preferences.speedLimits.scheduleAltLimits"],
  ["QueueManagementForm", QueueManagementForm, "queueing_enabled", "preferences.queueManagement.enableQueueing"],
  ["FileManagementForm", FileManagementForm, "auto_tmm_enabled", "preferences.fileManagement.autoTorrentManagement"],
  ["ConnectionSettingsForm", ConnectionSettingsForm, "upnp", "preferences.connectionSettings.enableUpnp"],
  ["AdvancedNetworkForm", AdvancedNetworkForm, "limit_lan_peers", "preferences.advancedNetwork.limitUtpProtocol"],
  ["NetworkDiscoveryForm", NetworkDiscoveryForm, "dht", "preferences.networkDiscovery.enableDht"],
]

// Only the fields a form dereferences without a fallback.
const preferences = {
  encryption: 0,
  max_connec: 500,
  max_connec_per_torrent: 100,
  max_uploads: 20,
  max_uploads_per_torrent: 4,
  scheduler_days: 0,
} as AppPreferences

// Radix Switch sizes its hidden form input through ResizeObserver, which jsdom lacks.
vi.stubGlobal("ResizeObserver", class {
  observe() {}
  unobserve() {}
  disconnect() {}
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  localStorage.clear()
})

function renderForm(Form: FormComponent, seed: AppPreferences = preferences, onSuccess?: () => void) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  queryClient.setQueryData(["instance-preferences", 1], seed)
  const ui = (instanceId: number) => (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <Form instanceId={instanceId} onSuccess={onSuccess} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  const { container, rerender } = render(ui(1))
  return { queryClient, form: container.querySelector("form")!, showInstance: (instanceId: number) => rerender(ui(instanceId)) }
}

function submitForm(Form: FormComponent, onSuccess: () => void) {
  fireEvent.submit(renderForm(Form, preferences, onSuccess).form)
}

// The forms do not tie every switch to its label, so walk up from the label text.
function switchFor(label: string) {
  let node: HTMLElement | null = screen.getByText(label)
  while (node && !node.querySelector("[role=switch]")) {
    node = node.parentElement
  }
  return node!.querySelector<HTMLElement>("[role=switch]")!
}

function failSaves() {
  server.use(http.patch("*/api/instances/1/preferences", () => HttpResponse.json({ error: "boom" }, { status: 500 })))
}

describe.each(forms)("%s save", (_name, Form, field, label) => {
  it("shows the error toast and keeps the dialog open when the save fails", async () => {
    failSaves()
    const onSuccess = vi.fn()

    submitForm(Form, onSuccess)

    await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1))
    expect(toast.success).not.toHaveBeenCalled()
    expect(onSuccess).not.toHaveBeenCalled()
  })

  it("shows the success toast only after the save responds", async () => {
    let respond = () => {}
    const response = new Promise<void>(resolve => { respond = resolve })
    let received = false
    server.use(http.patch("*/api/instances/1/preferences", async () => {
      received = true
      await response
      return HttpResponse.json(preferences)
    }))
    const onSuccess = vi.fn()

    submitForm(Form, onSuccess)

    await waitFor(() => expect(received).toBe(true))
    expect(toast.success).not.toHaveBeenCalled()
    expect(onSuccess).not.toHaveBeenCalled()

    respond()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(onSuccess).toHaveBeenCalledTimes(1)
    expect(toast.error).not.toHaveBeenCalled()
  })

  it("keeps the user's edit when the save fails", async () => {
    failSaves()
    const { form } = renderForm(Form, { ...preferences, [field]: false })

    fireEvent.click(switchFor(label))
    expect(switchFor(label).getAttribute("aria-checked")).toBe("true")
    fireEvent.submit(form)

    await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1))
    expect(switchFor(label).getAttribute("aria-checked")).toBe("true")
  })

  it("keeps its values when the preferences change while it is open", async () => {
    const { queryClient } = renderForm(Form, { ...preferences, [field]: false })

    queryClient.setQueryData(["instance-preferences", 1], { ...preferences, [field]: true })
    // React Query notifies its observers on a setTimeout(0) tick.
    await act(() => new Promise(resolve => setTimeout(resolve, 0)))

    expect(switchFor(label).getAttribute("aria-checked")).toBe("false")
  })
})

describe.each(forms)("%s instance switch", (_name, Form, field, label) => {
  it("seeds again from the new instance", () => {
    const { queryClient, showInstance } = renderForm(Form, { ...preferences, [field]: false })
    queryClient.setQueryData(["instance-preferences", 2], { ...preferences, [field]: true })

    showInstance(2)

    expect(switchFor(label).getAttribute("aria-checked")).toBe("true")
  })

  it("waits for an uncached instance's preferences before it seeds", async () => {
    server.use(http.get("*/api/instances/2/preferences", () => HttpResponse.json({ ...preferences, [field]: true })))
    const { showInstance } = renderForm(Form, { ...preferences, [field]: false })

    showInstance(2)

    await waitFor(() => expect(switchFor(label).getAttribute("aria-checked")).toBe("true"))
  })
})

describe("FileManagementForm start paused", () => {
  it("leaves the stored value unchanged when the save fails", async () => {
    failSaves()
    const { form } = renderForm(FileManagementForm)
    const before = localStorage.getItem("qui-start-paused-instance-1")

    fireEvent.click(switchFor("preferences.fileManagement.startTorrentsPaused"))
    fireEvent.submit(form)

    await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1))
    expect(localStorage.getItem("qui-start-paused-instance-1")).toBe(before)
  })
})
