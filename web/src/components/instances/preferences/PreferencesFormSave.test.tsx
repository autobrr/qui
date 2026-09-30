/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react"
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

const forms: [string, FormComponent][] = [
  ["SeedingLimitsForm", SeedingLimitsForm],
  ["SpeedLimitsForm", SpeedLimitsForm],
  ["QueueManagementForm", QueueManagementForm],
  ["FileManagementForm", FileManagementForm],
  ["ConnectionSettingsForm", ConnectionSettingsForm],
  ["AdvancedNetworkForm", AdvancedNetworkForm],
  ["NetworkDiscoveryForm", NetworkDiscoveryForm],
]

// Only the fields a form dereferences without a fallback.
const preferences = {
  encryption: 0,
  max_connec: 500,
  max_connec_per_torrent: 100,
  max_uploads: 20,
  max_uploads_per_torrent: 4,
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
})

function submitForm(Form: FormComponent, onSuccess: () => void) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  queryClient.setQueryData(["instance-preferences", 1], preferences)
  const { container } = render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <Form instanceId={1} onSuccess={onSuccess} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  fireEvent.submit(container.querySelector("form")!)
}

describe.each(forms)("%s save", (_name, Form) => {
  it("shows the error toast and keeps the dialog open when the save fails", async () => {
    server.use(http.patch("*/api/instances/1/preferences", () => HttpResponse.json({ error: "boom" }, { status: 500 })))
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
})
