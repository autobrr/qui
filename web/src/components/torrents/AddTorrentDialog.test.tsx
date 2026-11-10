/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { AppPreferences, PreferenceField } from "@/types"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { AddTorrentDialog, type AddTorrentDropPayload } from "./AddTorrentDialog"

const { metadataResult } = vi.hoisted(() => ({
  metadataResult: { data: undefined as { categories: Record<string, never>; tags: string[] } | undefined },
}))

vi.mock("@/lib/api", () => ({ api: {
  addTorrent: vi.fn(),
  checkTorrentDuplicates: vi.fn(),
  getInstancePreferenceView: vi.fn(),
} }))
vi.mock("@/hooks/useInstanceMetadata", () => ({ useInstanceMetadata: () => metadataResult }))
vi.mock("@/hooks/useInstanceCapabilities", () => ({ useInstanceCapabilities: () => ({ data: { supportsTorrentTmpPath: true, supportsPathAutocomplete: false } }) }))
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }))
vi.mock("sonner", () => ({ toast: { success: vi.fn(), warning: vi.fn(), error: vi.fn() } }))

beforeEach(() => {
  // The tabs indicator measures itself through ResizeObserver, which jsdom lacks.
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(() => {
  cleanup()
  metadataResult.data = undefined
  vi.resetAllMocks()
  vi.unstubAllGlobals()
})

const preferences = {
  auto_tmm_enabled: false,
  save_path: "/data/complete",
  torrent_content_layout: "Subfolder",
  temp_path_enabled: true,
  temp_path: "/data/incomplete",
} as AppPreferences

const magnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"

// Mounts the dialog with no metadata yet, the way the /add magnet handler route does.
function renderColdDialog() {
  vi.mocked(api.checkTorrentDuplicates).mockResolvedValue({ duplicates: [] })
  vi.mocked(api.addTorrent).mockResolvedValue({ added: 1, failed: 0, message: "" })
  // The test resolves the preferences itself.
  vi.mocked(api.getInstancePreferenceView).mockReturnValue(new Promise(() => {}))

  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  const dialog = (dropPayload: AddTorrentDropPayload | null) => (
    <QueryClientProvider client={client}>
      <AddTorrentDialog instanceId={1} open onOpenChange={() => {}} dropPayload={dropPayload} />
    </QueryClientProvider>
  )

  const ui = render(dialog({ type: "url", urls: [magnet] }))
  return {
    ...ui,
    resolvePreferences(resolved: AppPreferences) {
      client.setQueryData(["instance-preferences", 1], resolved)
      ui.rerender(dialog(null))
    },
    openAdvancedTab() {
      fireEvent.mouseDown(ui.getByRole("tab", { name: "addTorrentDialog.tabs.advanced" }))
    },
  }
}

it("seeds the option defaults from preferences that resolve after a cold mount", async () => {
  const ui = renderColdDialog()

  ui.resolvePreferences(preferences)
  ui.openAdvancedTab()

  await waitFor(() => expect(ui.getByLabelText("addTorrentDialog.options.autoTmm").getAttribute("aria-checked")).toBe("false"))
  expect((ui.getByLabelText("addTorrentDialog.options.savePath") as HTMLInputElement).value).toBe("/data/complete")
  expect(ui.getByLabelText("addTorrentDialog.options.useTemporaryPath").getAttribute("aria-checked")).toBe("true")
  expect((ui.getByLabelText("addTorrentDialog.options.tempPath") as HTMLInputElement).value).toBe("/data/incomplete")

  fireEvent.click(ui.getByRole("button", { name: "addTorrentDialog.footer.add" }))

  await waitFor(() => expect(api.addTorrent).toHaveBeenCalled())
  expect(vi.mocked(api.addTorrent).mock.calls[0][1]).toMatchObject({
    autoTMM: false,
    savePath: "/data/complete",
    contentLayout: "Subfolder",
    // The late seed must leave the magnet the route injected alone.
    urls: [magnet],
  })
})

it("keeps a change made before preferences resolve", async () => {
  const ui = renderColdDialog()
  ui.openAdvancedTab()

  // Cold mount falls back to autoTMM on; the user turns it off before preferences land.
  const toggle = await waitFor(() => ui.getByLabelText("addTorrentDialog.options.autoTmm"))
  expect(toggle.getAttribute("aria-checked")).toBe("true")
  fireEvent.click(toggle)

  ui.resolvePreferences({ ...preferences, auto_tmm_enabled: true })

  expect(ui.getByLabelText("addTorrentDialog.options.autoTmm").getAttribute("aria-checked")).toBe("false")

  fireEvent.click(ui.getByRole("button", { name: "addTorrentDialog.footer.add" }))
  await waitFor(() => expect(api.addTorrent).toHaveBeenCalled())
  expect(vi.mocked(api.addTorrent).mock.calls[0][1]).toMatchObject({ autoTMM: false })
})

it("applies the defaults that the fetch on open returns, after a dropped magnet", async () => {
  vi.mocked(api.checkTorrentDuplicates).mockResolvedValue({ duplicates: [] })
  let resolveView: (fields: PreferenceField[]) => void = () => {}
  vi.mocked(api.getInstancePreferenceView).mockReturnValue(new Promise(resolve => { resolveView = resolve }) as never)

  // Another screen filled the cache before a default changed in qBittorrent.
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  client.setQueryData(["instance-preferences", 1], preferences)
  const dialog = (open: boolean, dropPayload: AddTorrentDropPayload | null) => (
    <QueryClientProvider client={client}>
      <AddTorrentDialog instanceId={1} open={open} onOpenChange={() => {}} dropPayload={dropPayload} />
    </QueryClientProvider>
  )

  const ui = render(dialog(false, null))
  // The dropped magnet touches the form before the fetch answers.
  ui.rerender(dialog(true, { type: "url", urls: [magnet] }))
  await waitFor(() => expect(api.getInstancePreferenceView).toHaveBeenCalled())
  resolveView([{ key: "auto_tmm_enabled", value: false }, { key: "save_path", value: "/data/changed" }] as PreferenceField[])
  ui.rerender(dialog(true, null))
  fireEvent.mouseDown(ui.getByRole("tab", { name: "addTorrentDialog.tabs.advanced" }))

  await waitFor(() => expect((ui.getByLabelText("addTorrentDialog.options.savePath") as HTMLInputElement).value).toBe("/data/changed"))
})
