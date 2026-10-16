/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { server } from "@/test/msw/server"
import type { PreferenceField } from "@/types"

import { InstancePreferencesDialog } from "./InstancePreferencesDialog"

const { toast, i18n, instances } = vi.hoisted(() => ({
  toast: { success: vi.fn(), error: vi.fn() },
  i18n: { t: (key: string) => key },
  instances: {
    instances: [{ id: 1, name: "qbit", isActive: true }],
    deleteInstance: () => {},
    setInstanceStatus: () => {},
    isDeleting: false,
    isUpdatingStatus: false,
    updatingStatusId: undefined,
  },
}))

vi.mock("sonner", () => ({ toast }))
vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => i18n,
  Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
}))
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => instances }))
vi.mock("./InstanceSettingsPanel", () => ({ InstanceSettingsPanel: () => null }))

// Radix Switch sizes its hidden form input through ResizeObserver, which jsdom lacks.
vi.stubGlobal("ResizeObserver", class {
  observe() {}
  unobserve() {}
  disconnect() {}
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.clearAllMocks()
})

const view: PreferenceField[] = [
  { key: "save_path", value: "/data", tab: "downloads", section: "savingManagement", control: "path", optionSource: "pathAutocomplete" },
  { key: "listen_port", value: 51413, tab: "connection", section: "listeningPort", control: "number" },
  { key: "up_limit", value: 2048, tab: "speed", section: "globalRateLimits", control: "number", wireUnit: "B/s", displayUnit: "KiB/s" },
  { key: "dl_limit", value: 0, tab: "speed", section: "globalRateLimits", control: "number", wireUnit: "B/s", displayUnit: "KiB/s" },
  { key: "dht", value: true, tab: "bittorrent", section: "privacy", control: "switch" },
  { key: "max_ratio_enabled", value: false, tab: "bittorrent", section: "seedingLimits", control: "switch" },
  { key: "max_ratio", value: 2, tab: "bittorrent", section: "seedingLimits", control: "number", dependsOn: "max_ratio_enabled" },
  { key: "add_trackers_enabled", value: false, tab: "bittorrent", section: "autoAddTrackers", control: "switch", readOnly: true, reason: "pending BitTorrent PR" },
  { key: "rss_processing_enabled", value: true, tab: "rss", section: "reader", control: "switch" },
  { key: "rss_refresh_interval", value: 30, tab: "rss", section: "reader", control: "number", wireUnit: "min", displayUnit: "min", dependsOn: "rss_processing_enabled" },
  { key: "rss_max_articles_per_feed", value: 50, tab: "rss", section: "reader", control: "number", dependsOn: "rss_processing_enabled" },
  { key: "rss_auto_downloading_enabled", value: false, tab: "rss", section: "autoDownloader", control: "switch" },
  { key: "rss_download_repack_proper_episodes", value: true, tab: "rss", section: "smartEpisodeFilter", control: "switch" },
  { key: "disk_queue_size", value: 1048576, tab: "advanced", section: "libtorrent", control: "number", wireUnit: "B", displayUnit: "KiB" },
  { key: "scan_dirs", value: { "/watch/a": 0, "/watch/b": 1 }, tab: "downloads", section: "savingManagement", control: "watchFolders" },
]

function serveView(fields: PreferenceField[] = view) {
  server.use(http.get("*/api/instances/1/preferences", () => HttpResponse.json(fields)))
}

// Records the body of each save and answers with the view.
function recordSaves() {
  const bodies: Record<string, unknown>[] = []
  server.use(http.patch("*/api/instances/1/preferences", async ({ request }) => {
    bodies.push(await request.json() as Record<string, unknown>)
    return HttpResponse.json(view)
  }))
  return bodies
}

// The input that a field label points at.
async function fieldFor(labelKey: string) {
  return document.getElementById((await screen.findByText(labelKey)).closest("label")!.htmlFor) as HTMLInputElement
}

function save() {
  fireEvent.click(screen.getByRole("button", { name: "preferences.common.saveChanges" }))
}

function renderDialog(defaultTab?: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const result = render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <InstancePreferencesDialog open onOpenChange={() => {}} instanceId={1} instanceName="qbit" defaultTab={defaultTab} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  return { ...result, queryClient }
}

// Opens the dialog, waits for its tabs, and closes it. The returned function opens it again.
async function openAndClose() {
  // The stale time of the app's query client.
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 5000 } } })
  const dialog = (open: boolean) => (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <InstancePreferencesDialog open={open} onOpenChange={() => {}} instanceId={1} instanceName="qbit" />
      </TooltipProvider>
    </QueryClientProvider>
  )
  const { rerender } = render(dialog(true))
  await screen.findByRole("tab", { name: "preferences.dialog.tabs.downloads" })
  rerender(dialog(false))
  await waitFor(() => expect(screen.queryByRole("tab", { name: "preferences.dialog.tabs.downloads" })).toBeNull())
  return () => rerender(dialog(true))
}

describe("InstancePreferencesDialog", () => {
  it("shows the Instance tab, then the qBittorrent tabs that have fields, in the qBittorrent order", async () => {
    serveView()

    renderDialog()

    await screen.findByRole("tab", { name: "preferences.dialog.tabs.downloads" })
    expect(screen.getAllByRole("tab").map(tab => tab.textContent)).toEqual([
      "preferences.dialog.tabs.instance",
      "preferences.dialog.tabs.downloads",
      "preferences.dialog.tabs.connection",
      "preferences.dialog.tabs.speed",
      "preferences.dialog.tabs.bittorrent",
      "preferences.dialog.tabs.rss",
      "preferences.dialog.tabs.advanced",
    ])
  })

  it("shows the rate limits in KiB/s and 0 as unlimited", async () => {
    serveView()

    renderDialog("speed")

    const upload = await fieldFor("preferences.speedLimits.uploadLimit")
    const download = await fieldFor("preferences.speedLimits.downloadLimit")
    expect(upload.value).toBe("2")
    expect(upload.nextElementSibling?.textContent).toBe("KiB/s")
    expect(download.value).toBe("")
    expect(download.placeholder).toBe("common:numberInput.unlimited")
  })

  it("shows a size in bytes in KiB", async () => {
    serveView()

    renderDialog("advanced")

    const queueSize = await fieldFor("preferences.advancedNetwork.diskQueueSize")
    expect(queueSize.value).toBe("1024")
    expect(queueSize.nextElementSibling?.textContent).toBe("KiB")
  })

  it("shows a read-only field disabled, with its note", async () => {
    serveView()

    renderDialog("bittorrent")

    const field = await fieldFor("preferences.fields.addTrackersEnabled")
    expect(field.disabled).toBe(true)
    expect(screen.getAllByText("preferences.readOnlyNote")).toHaveLength(1)
  })

  it("enables a dependent field only while its switch is on", async () => {
    serveView()

    renderDialog("bittorrent")

    const ratio = await fieldFor("preferences.seedingLimits.maxShareRatio")
    expect(ratio.disabled).toBe(true)
    fireEvent.click(await fieldFor("preferences.seedingLimits.enableShareRatioLimit"))
    expect(ratio.disabled).toBe(false)
  })

  it("sends only the changed keys, in the wire unit", async () => {
    const bodies = recordSaves()
    serveView()
    renderDialog("speed")

    fireEvent.change(await fieldFor("preferences.speedLimits.uploadLimit"), { target: { value: "3" } })
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([{ up_limit: 3072 }])
  })

  it("sends no request when nothing changed", async () => {
    const bodies = recordSaves()
    serveView()
    renderDialog("speed")

    await fieldFor("preferences.speedLimits.uploadLimit")
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([])
  })

  it("sends a key that is changed back after a save", async () => {
    const bodies = recordSaves()
    serveView()
    renderDialog("bittorrent")

    const dht = await fieldFor("preferences.networkDiscovery.enableDht")
    fireEvent.click(dht)
    save()
    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    fireEvent.click(dht)
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(2))
    expect(bodies).toEqual([{ dht: false }, { dht: true }])
  })

  it("shows an error toast with the reason and keeps the edit when the save fails", async () => {
    server.use(http.patch("*/api/instances/1/preferences", () =>
      HttpResponse.text("invalid preferences patch: \"dht\" has a value of the wrong type", { status: 400 })
    ))
    serveView()
    renderDialog("bittorrent")

    const dht = await fieldFor("preferences.networkDiscovery.enableDht")
    fireEvent.click(dht)
    save()

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("preferences.toast.saveFailed", {
      description: "Invalid preferences patch: \"dht\" has a value of the wrong type",
    }))
    expect(toast.success).not.toHaveBeenCalled()
    expect(dht.getAttribute("aria-checked")).toBe("false")
  })

  it("keeps its starting values and the edits when the view refreshes", async () => {
    serveView()
    const { queryClient } = renderDialog("bittorrent")

    fireEvent.click(await fieldFor("preferences.seedingLimits.enableShareRatioLimit"))
    serveView(view.map(field => field.key === "dht" || field.key === "max_ratio_enabled" ? { ...field, value: !field.value } : field))
    await queryClient.refetchQueries({ queryKey: ["instance-preference-view", 1] })

    expect((await fieldFor("preferences.seedingLimits.enableShareRatioLimit")).getAttribute("aria-checked")).toBe("true")
    expect((await fieldFor("preferences.networkDiscovery.enableDht")).getAttribute("aria-checked")).toBe("true")
  })

  it("shows the five RSS settings in the RSS tab", async () => {
    serveView()

    renderDialog("rss")

    expect((await fieldFor("rss:warnings.enableRss")).getAttribute("aria-checked")).toBe("true")
    expect((await fieldFor("rss:settingsPopover.refreshInterval")).value).toBe("30")
    expect((await fieldFor("rss:settingsPopover.maxArticles")).value).toBe("50")
    expect((await fieldFor("rss:settingsPopover.autoDownload")).getAttribute("aria-checked")).toBe("false")
    expect((await fieldFor("rss:settingsPopover.downloadRepack")).getAttribute("aria-checked")).toBe("true")
    expect(screen.getAllByRole("heading", { level: 3 }).map(heading => heading.textContent)).toEqual([
      "preferences.sections.rssReader",
      "preferences.sections.rssAutoDownloader",
      "preferences.sections.rssSmartEpisodeFilter",
    ])
  })

  it("keeps an open RSS edit when the view refreshes, and saves only that edit", async () => {
    const bodies = recordSaves()
    serveView()
    const { queryClient } = renderDialog("rss")

    fireEvent.change(await fieldFor("rss:settingsPopover.refreshInterval"), { target: { value: "15" } })
    serveView(view.map(field => field.key === "rss_refresh_interval" ? { ...field, value: 60 } : field))
    await queryClient.refetchQueries({ queryKey: ["instance-preference-view", 1] })

    expect((await fieldFor("rss:settingsPopover.refreshInterval")).value).toBe("15")
    save()
    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([{ rss_refresh_interval: 15 }])
  })

  it("shows the qBittorrent tabs again when it reopens while the view is still fresh", async () => {
    serveView()
    const reopen = await openAndClose()

    reopen()

    await screen.findByRole("tab", { name: "preferences.dialog.tabs.downloads" })
  })

  it("shows the retry and not the old fields when the fetch fails on a reopen", async () => {
    serveView()
    const reopen = await openAndClose()
    server.use(http.get("*/api/instances/1/preferences", () => HttpResponse.json({ error: "unreachable" }, { status: 502 })))

    reopen()

    await screen.findByRole("button", { name: "preferences.dialog.retry" })
    expect(screen.queryByRole("tab", { name: "preferences.dialog.tabs.downloads" })).toBeNull()
  })

  it("sends scan_dirs when one watch folder changes", async () => {
    const bodies = recordSaves()
    serveView()
    renderDialog("downloads")

    const [first] = await screen.findAllByPlaceholderText("preferences.fileManagement.watchFolders.monitoredFolderPlaceholder")
    fireEvent.change(first, { target: { value: "/watch/c" } })
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([{ scan_dirs: { "/watch/c": 0, "/watch/b": 1 } }])
  })

  it("adds no watch folder when the user clicks the Watch Folders label", async () => {
    serveView()
    renderDialog("downloads")

    fireEvent.click((await screen.findByText("preferences.fileManagement.watchFolders.title")).closest("label")!)

    expect(screen.getAllByPlaceholderText("preferences.fileManagement.watchFolders.monitoredFolderPlaceholder")).toHaveLength(2)
  })
})
