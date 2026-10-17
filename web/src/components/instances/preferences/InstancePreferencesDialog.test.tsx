/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { server } from "@/test/msw/server"
import type { NetworkBindingInterface, PreferenceField } from "@/types"

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
  { key: "file_log_enabled", value: true, tab: "behavior", section: "logFile", control: "switch" },
  { key: "file_log_backup_enabled", value: false, tab: "behavior", section: "logFile", control: "switch", dependsOn: "file_log_enabled" },
  { key: "file_log_max_size", value: 65, tab: "behavior", section: "logFile", control: "number", wireUnit: "KiB", displayUnit: "KiB", dependsOn: "file_log_backup_enabled" },
  { key: "file_log_age_type", value: 1, tab: "behavior", section: "logFile", control: "select", optionSource: "fixed", options: [0, 1, 2] },
  { key: "save_path", value: "/data", tab: "downloads", section: "savingManagement", control: "path", optionSource: "pathAutocomplete" },
  { key: "listen_port", value: 51413, tab: "connection", section: "listeningPort", control: "number" },
  { key: "up_limit", value: 2048, tab: "speed", section: "globalRateLimits", control: "number", wireUnit: "B/s", displayUnit: "KiB/s" },
  { key: "dl_limit", value: 0, tab: "speed", section: "globalRateLimits", control: "number", wireUnit: "B/s", displayUnit: "KiB/s" },
  { key: "dht", value: true, tab: "bittorrent", section: "privacy", control: "switch" },
  { key: "max_ratio_enabled", value: false, tab: "bittorrent", section: "seedingLimits", control: "switch" },
  { key: "max_ratio", value: 2, tab: "bittorrent", section: "seedingLimits", control: "number", dependsOn: "max_ratio_enabled" },
  { key: "max_inactive_seeding_time_enabled", value: false, tab: "bittorrent", section: "seedingLimits", control: "switch" },
  { key: "max_inactive_seeding_time", value: -1, tab: "bittorrent", section: "seedingLimits", control: "number", wireUnit: "min", displayUnit: "min", dependsOn: "max_inactive_seeding_time_enabled" },
  { key: "max_ratio_act", value: 3, tab: "bittorrent", section: "seedingLimits", control: "select", optionSource: "fixed", options: [0, 1, 3, 2] },
  { key: "add_trackers_url_list", value: "udp://tracker.example.invalid:1337/announce", tab: "bittorrent", section: "autoAddTrackersFromUrl", control: "list", readOnly: true, reason: "qBittorrent fetches the list from the URL" },
  { key: "rss_processing_enabled", value: true, tab: "rss", section: "reader", control: "switch" },
  { key: "rss_refresh_interval", value: 30, tab: "rss", section: "reader", control: "number", wireUnit: "min", displayUnit: "min", dependsOn: "rss_processing_enabled" },
  { key: "rss_fetch_delay", value: 2, tab: "rss", section: "reader", control: "number", wireUnit: "s", displayUnit: "s", dependsOn: "rss_processing_enabled" },
  { key: "rss_max_articles_per_feed", value: 50, tab: "rss", section: "reader", control: "number", dependsOn: "rss_processing_enabled" },
  { key: "rss_auto_downloading_enabled", value: false, tab: "rss", section: "autoDownloader", control: "switch" },
  { key: "rss_download_repack_proper_episodes", value: true, tab: "rss", section: "smartEpisodeFilter", control: "switch" },
  { key: "rss_smart_episode_filters", value: "s(\\d+)e(\\d+)\n(\\d+)x(\\d+)", tab: "rss", section: "smartEpisodeFilter", control: "list" },
  { key: "disk_queue_size", value: 1048576, tab: "advanced", section: "libtorrent", control: "number", wireUnit: "B", displayUnit: "KiB" },
  { key: "scan_dirs", value: { "/watch/a": 0, "/watch/b": 1 }, tab: "downloads", section: "savingManagement", control: "watchFolders" },
]

function serveView(fields: PreferenceField[] = view, { supportsPathAutocomplete = false, supportsTestEmail = false } = {}) {
  server.use(
    http.get("*/api/instances/1/preferences", () => HttpResponse.json(fields)),
    http.get("*/api/instances/1/capabilities", () => HttpResponse.json({ webAPIVersion: "2.15.1", supportsPathAutocomplete, supportsTestEmail }))
  )
}

// The email notification section of qBittorrent 5.3, with email notification and authentication on.
function emailView(enabled = true): PreferenceField[] {
  return [
    { key: "mail_notification_enabled", value: enabled, tab: "downloads", section: "emailNotification", control: "switch" },
    { key: "mail_notification_smtp", value: "smtp.example.invalid:465", tab: "downloads", section: "emailNotification", control: "text", dependsOn: "mail_notification_enabled" },
    { key: "mail_notification_encryption_type", value: "SMTPS", tab: "downloads", section: "emailNotification", control: "select", optionSource: "fixed", options: ["None", "STARTTLS", "SMTPS"], dependsOn: "mail_notification_enabled" },
    { key: "mail_notification_auth_enabled", value: true, tab: "downloads", section: "emailNotification", control: "switch", dependsOn: "mail_notification_enabled" },
    { key: "mail_notification_password", value: "secret", tab: "downloads", section: "emailNotification", control: "password", dependsOn: "mail_notification_auth_enabled" },
  ]
}

// Counts the test email requests.
function recordTestEmails() {
  const sent = { count: 0 }
  server.use(http.post("*/api/instances/1/preferences/test-email", () => {
    sent.count++
    return new HttpResponse(null, { status: 204 })
  }))
  return sent
}

// Answers each folder listing with the folders under /data, and records the folders that were listed.
function serveFolders() {
  const listed: string[] = []
  server.use(http.get("*/api/instances/1/getDirectoryContent", ({ request }) => {
    const dirPath = new URL(request.url).searchParams.get("dirPath") ?? ""
    listed.push(dirPath)
    return HttpResponse.json(dirPath === "/data/" ? ["/data/movies", "/data/music", "/data/tv"] : [])
  }))
  return listed
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

function renderDialog(defaultTab?: string, onOpenChange: (open: boolean) => void = () => {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const result = render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <InstancePreferencesDialog open onOpenChange={onOpenChange} instanceId={1} instanceName="qbit" defaultTab={defaultTab} />
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
      "preferences.dialog.tabs.behavior",
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

  it("shows the socket buffers in KiB and the .torrent size limit in MiB, and saves them in bytes", async () => {
    const bodies = recordSaves()
    serveView([
      ...view,
      { key: "torrent_file_size_limit", value: 104857600, tab: "advanced", section: "qbittorrent", control: "number", wireUnit: "B", displayUnit: "MiB" },
      { key: "socket_send_buffer_size", value: 0, tab: "advanced", section: "libtorrent", control: "number", wireUnit: "B", displayUnit: "KiB" },
      { key: "socket_receive_buffer_size", value: 131072, tab: "advanced", section: "libtorrent", control: "number", wireUnit: "B", displayUnit: "KiB" },
    ])
    renderDialog("advanced")

    const sizeLimit = await fieldFor("preferences.fields.torrentFileSizeLimit")
    const send = await fieldFor("preferences.fields.socketSendBufferSize")
    const receive = await fieldFor("preferences.fields.socketReceiveBufferSize")
    expect(sizeLimit.value).toBe("100")
    expect(sizeLimit.nextElementSibling?.textContent).toBe("MiB")
    expect(send.value).toBe("0")
    expect(receive.value).toBe("128")
    expect(receive.nextElementSibling?.textContent).toBe("KiB")

    fireEvent.change(sizeLimit, { target: { value: "50" } })
    fireEvent.change(send, { target: { value: "256" } })
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([{ torrent_file_size_limit: 52428800, socket_send_buffer_size: 262144 }])
  })

  it("shows the fetched trackers read-only, with the note of the field", async () => {
    serveView()

    renderDialog("bittorrent")

    const fetched = await fieldFor("preferences.fields.addTrackersUrlList")
    expect(fetched.value).toBe("udp://tracker.example.invalid:1337/announce")
    expect(fetched.disabled).toBe(true)
    expect(screen.getAllByText("preferences.fields.addTrackersUrlListNote")).toHaveLength(1)
  })

  it("shows the log file in the Behavior tab, with the size in KiB behind two switches", async () => {
    serveView()

    renderDialog("behavior")

    const maxSize = await fieldFor("preferences.fields.fileLogMaxSize")
    expect(maxSize.value).toBe("65")
    expect(maxSize.nextElementSibling?.textContent).toBe("KiB")
    expect(maxSize.disabled).toBe(true)
    fireEvent.click(await fieldFor("preferences.fields.fileLogBackupEnabled"))
    expect(maxSize.disabled).toBe(false)
    fireEvent.click(await fieldFor("preferences.fields.fileLogEnabled"))
    expect(maxSize.disabled).toBe(true)
    expect((await fieldFor("preferences.fields.fileLogAgeType")).textContent).toBe("preferences.fields.fileLogAgeMonths")
    expect(screen.getAllByRole("heading", { level: 3 }).map(heading => heading.textContent)).toEqual(["preferences.sections.logFile"])
  })

  it("shows the inactive seeding limit in minutes and the action at the limit", async () => {
    const bodies = recordSaves()
    serveView()
    renderDialog("bittorrent")

    const inactive = await fieldFor("preferences.fields.maxInactiveSeedingTime")
    expect(inactive.disabled).toBe(true)
    expect(inactive.nextElementSibling?.textContent).toBe("preferences.units.minutes")
    expect((await fieldFor("preferences.workflowDialog.shareLimitAction.label")).textContent).toBe("preferences.workflowDialog.shareLimitAction.removeWithContent")
    fireEvent.click(await fieldFor("preferences.fields.maxInactiveSeedingTimeEnabled"))
    fireEvent.change(inactive, { target: { value: "1440" } })
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([{ max_inactive_seeding_time_enabled: true, max_inactive_seeding_time: 1440 }])
  })

  it("fills an empty seeding limit with a starting value when its switch turns on", async () => {
    const bodies = recordSaves()
    serveView()
    renderDialog("bittorrent")

    const inactive = await fieldFor("preferences.fields.maxInactiveSeedingTime")
    expect(inactive.value).toBe("")
    fireEvent.click(await fieldFor("preferences.fields.maxInactiveSeedingTimeEnabled"))
    expect(inactive.value).toBe("1440")
    fireEvent.click(await fieldFor("preferences.seedingLimits.enableShareRatioLimit"))
    expect((await fieldFor("preferences.seedingLimits.maxShareRatio")).value).toBe("2")
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([{ max_inactive_seeding_time_enabled: true, max_inactive_seeding_time: 1440, max_ratio_enabled: true }])
  })

  it("sends no seeding limit when its switch turns on and off again", async () => {
    const bodies = recordSaves()
    serveView(view.map(field => field.key === "max_ratio_enabled" ? { ...field, value: true } : field))
    renderDialog("bittorrent")

    const inactive = await fieldFor("preferences.fields.maxInactiveSeedingTime")
    const enabled = await fieldFor("preferences.fields.maxInactiveSeedingTimeEnabled")
    fireEvent.click(enabled)
    fireEvent.change(inactive, { target: { value: "60" } })
    fireEvent.blur(inactive)
    fireEvent.click(enabled)
    expect(inactive.value).toBe("")
    // A saved limit comes back when its switch turns on again.
    const ratio = await fieldFor("preferences.seedingLimits.maxShareRatio")
    const ratioEnabled = await fieldFor("preferences.seedingLimits.enableShareRatioLimit")
    fireEvent.click(ratioEnabled)
    expect(ratio.value).toBe("")
    fireEvent.click(ratioEnabled)
    expect(ratio.value).toBe("2")
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([])
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

  it("shows the latest fetched trackers when the view refreshes", async () => {
    serveView()
    const { queryClient } = renderDialog("bittorrent")

    await fieldFor("preferences.fields.addTrackersUrlList")
    serveView(view.map(field => field.key === "add_trackers_url_list" ? { ...field, value: "udp://other.example.invalid:6969/announce" } : field))
    await queryClient.refetchQueries({ queryKey: ["instance-preference-view", 1] })

    expect((await fieldFor("preferences.fields.addTrackersUrlList")).value).toBe("udp://other.example.invalid:6969/announce")
  })

  it("shows the seven RSS settings in the RSS tab", async () => {
    serveView()

    renderDialog("rss")

    expect((await fieldFor("rss:warnings.enableRss")).getAttribute("aria-checked")).toBe("true")
    expect((await fieldFor("rss:settingsPopover.refreshInterval")).value).toBe("30")
    const fetchDelay = await fieldFor("preferences.fields.rssFetchDelay")
    expect(fetchDelay.value).toBe("2")
    expect(fetchDelay.nextElementSibling?.textContent).toBe("preferences.advancedNetwork.unitSeconds")
    expect((await fieldFor("rss:settingsPopover.maxArticles")).value).toBe("50")
    expect((await fieldFor("rss:settingsPopover.autoDownload")).getAttribute("aria-checked")).toBe("false")
    expect((await fieldFor("rss:settingsPopover.downloadRepack")).getAttribute("aria-checked")).toBe("true")
    expect((await fieldFor("preferences.fields.rssSmartEpisodeFilters")).value).toBe("s(\\d+)e(\\d+)\n(\\d+)x(\\d+)")
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

  it("suggests folders in a path field when qBittorrent can list them, and saves the chosen folder", async () => {
    const bodies = recordSaves()
    const listed = serveFolders()
    serveView(view, { supportsPathAutocomplete: true })
    renderDialog("downloads")

    // The field becomes an autocomplete box when the capabilities arrive.
    await waitFor(async () => expect((await fieldFor("preferences.fileManagement.defaultSavePath")).getAttribute("autocomplete")).toBe("off"))
    const savePath = await fieldFor("preferences.fileManagement.defaultSavePath")
    fireEvent.change(savePath, { target: { value: "/data/m" } })
    const movies = await screen.findByRole("button", { name: "/data/movies" })
    expect(screen.getByRole("button", { name: "/data/music" })).toBeTruthy()
    expect(screen.queryByRole("button", { name: "/data/tv" })).toBeNull()
    fireEvent.click(movies)
    expect(savePath.value).toBe("/data/movies/")
    expect(listed).toContain("/data/")
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([{ save_path: "/data/movies/" }])
  })

  it("closes only the folder suggestions on the first Escape, and the dialog on the second", async () => {
    serveFolders()
    serveView(view, { supportsPathAutocomplete: true })
    const onOpenChange = vi.fn()
    renderDialog("downloads", onOpenChange)

    await waitFor(async () => expect((await fieldFor("preferences.fileManagement.defaultSavePath")).getAttribute("autocomplete")).toBe("off"))
    const savePath = await fieldFor("preferences.fileManagement.defaultSavePath")
    fireEvent.change(savePath, { target: { value: "/data/m" } })
    await screen.findByRole("button", { name: "/data/movies" })

    fireEvent.keyDown(savePath, { key: "Escape" })
    await waitFor(() => expect(screen.queryByRole("button", { name: "/data/movies" })).toBeNull())
    expect(onOpenChange).not.toHaveBeenCalled()

    fireEvent.keyDown(savePath, { key: "Escape" })
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it("keeps the same folder input when the capabilities arrive after the user starts typing", async () => {
    serveFolders()
    serveView(view, { supportsPathAutocomplete: true })
    let answerCapabilities = () => {}
    const capabilitiesSent = new Promise<void>(resolve => { answerCapabilities = resolve })
    server.use(http.get("*/api/instances/1/capabilities", async () => {
      await capabilitiesSent
      return HttpResponse.json({ webAPIVersion: "2.15.1", supportsPathAutocomplete: true })
    }))
    renderDialog("downloads")

    const savePath = await fieldFor("preferences.fileManagement.defaultSavePath")
    savePath.focus()
    fireEvent.change(savePath, { target: { value: "/data" } })
    answerCapabilities()

    await waitFor(() => expect(savePath.getAttribute("autocomplete")).toBe("off"))
    expect(await fieldFor("preferences.fileManagement.defaultSavePath")).toBe(savePath)
    expect(document.activeElement).toBe(savePath)
  })

  it("suggests no folders for a file path, also when qBittorrent can list folders", async () => {
    const listed = serveFolders()
    serveView([
      { key: "ip_filter_enabled", value: true, tab: "connection", section: "ipFiltering", control: "switch" },
      { key: "ip_filter_path", value: "", tab: "connection", section: "ipFiltering", control: "path", dependsOn: "ip_filter_enabled" },
    ], { supportsPathAutocomplete: true })
    const { queryClient } = renderDialog("connection")

    await waitFor(() => expect(queryClient.getQueryData(["instance-capabilities", 1])).toBeDefined())
    const filterPath = await fieldFor("preferences.connectionSettings.ipFilterPath")
    expect(filterPath.getAttribute("autocomplete")).toBeNull()
    fireEvent.change(filterPath, { target: { value: "/data/m" } })
    await new Promise(resolve => setTimeout(resolve, 50))
    expect(listed).toEqual([])
  })

  it("shows a plain text box for a path when qBittorrent cannot list folders", async () => {
    const bodies = recordSaves()
    const listed = serveFolders()
    serveView()
    const { queryClient } = renderDialog("downloads")

    const savePath = await fieldFor("preferences.fileManagement.defaultSavePath")
    await waitFor(() => expect(queryClient.getQueryData(["instance-capabilities", 1])).toBeDefined())
    fireEvent.change(savePath, { target: { value: "/data/m" } })
    await new Promise(resolve => setTimeout(resolve, 50))
    expect(listed).toEqual([])
    expect(screen.queryByRole("button", { name: "/data/movies" })).toBeNull()
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([{ save_path: "/data/m" }])
  })

  it("shows the stop condition and the deletion of .torrent files, and the 5.3 backup folders", async () => {
    const bodies = recordSaves()
    serveView([
      { key: "torrent_stop_condition", value: "None", tab: "downloads", section: "whenAdding", control: "select", optionSource: "fixed", options: ["None", "MetadataReceived", "FilesChecked"] },
      { key: "auto_delete_mode", value: 2, tab: "downloads", section: "whenAdding", control: "select", optionSource: "fixed", options: [0, 1, 2] },
      { key: "merge_trackers", value: false, tab: "downloads", section: "whenAddingDuplicate", control: "switch" },
      { key: "torrent_files_backup_enabled", value: false, tab: "downloads", section: "torrentFileBackup", control: "switch" },
      { key: "torrent_files_backup_dir", value: "backup", tab: "downloads", section: "torrentFileBackup", control: "path", optionSource: "pathAutocomplete", dependsOn: "torrent_files_backup_enabled" },
      { key: "preallocate_all", value: false, tab: "downloads", section: "files", control: "switch" },
    ])
    renderDialog("downloads")

    expect((await fieldFor("preferences.fields.torrentStopCondition")).textContent).toBe("preferences.fields.torrentStopConditionNone")
    expect((await fieldFor("preferences.fields.autoDeleteMode")).textContent).toBe("preferences.fields.autoDeleteModeAlways")
    const backupDir = await fieldFor("preferences.fields.torrentFilesBackupDir")
    expect(backupDir.disabled).toBe(true)
    fireEvent.click(await fieldFor("preferences.fields.torrentFilesBackupEnabled"))
    fireEvent.change(backupDir, { target: { value: "/backup" } })
    fireEvent.click(await fieldFor("preferences.fields.preallocateAll"))
    expect(screen.getAllByRole("heading", { level: 3 }).map(heading => heading.textContent)).toEqual([
      "preferences.sections.whenAdding",
      "preferences.sections.whenAddingDuplicate",
      "preferences.sections.torrentFileBackup",
    ])
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([{ torrent_files_backup_enabled: true, torrent_files_backup_dir: "/backup", preallocate_all: true }])
  })

  it("shows the email settings with a password box, and sends a test email", async () => {
    const bodies = recordSaves()
    const sent = recordTestEmails()
    serveView(emailView(), { supportsTestEmail: true })
    renderDialog("downloads")

    expect((await fieldFor("preferences.fields.mailNotificationEncryptionType")).textContent).toBe("preferences.fields.mailNotificationEncryptionSmtps")
    const password = await fieldFor("preferences.fields.mailNotificationPassword")
    expect(password.type).toBe("password")
    expect(password.disabled).toBe(false)
    fireEvent.change(password, { target: { value: "hunter2" } })

    fireEvent.click(await screen.findByRole("button", { name: "preferences.fields.sendTestEmail" }))
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("preferences.toast.testEmailQueued", expect.anything()))
    expect(sent.count).toBe(1)

    save()
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("preferences.toast.saved"))
    expect(bodies).toEqual([{ mail_notification_password: "hunter2" }])
  })

  it("shows no test email button when qBittorrent cannot send one", async () => {
    serveView(emailView(), { supportsTestEmail: false })
    const { queryClient } = renderDialog("downloads")

    await fieldFor("preferences.fields.mailNotificationPassword")
    await waitFor(() => expect(queryClient.getQueryData(["instance-capabilities", 1])).toBeDefined())
    expect(screen.queryByRole("button", { name: "preferences.fields.sendTestEmail" })).toBeNull()
  })

  it("keeps the test email button off until email notification is saved on", async () => {
    const bodies = recordSaves()
    const sent = recordTestEmails()
    serveView(emailView(false), { supportsTestEmail: true })
    renderDialog("downloads")

    const button = await screen.findByRole("button", { name: "preferences.fields.sendTestEmail" }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
    // qBittorrent sends nothing until it has the switch on.
    fireEvent.click(await fieldFor("preferences.fields.mailNotificationEnabled"))
    expect(button.disabled).toBe(true)
    expect((await fieldFor("preferences.fields.mailNotificationPassword")).disabled).toBe(false)

    save()
    await waitFor(() => expect(button.disabled).toBe(false))
    expect(bodies).toEqual([{ mail_notification_enabled: true }])
    fireEvent.click(button)
    await waitFor(() => expect(sent.count).toBe(1))
  })

  it("shows the proxy with a password box behind the authentication switch", async () => {
    const bodies = recordSaves()
    serveView([
      { key: "i2p_enabled", value: false, tab: "connection", section: "i2p", control: "switch" },
      { key: "proxy_type", value: "SOCKS5", tab: "connection", section: "proxy", control: "select", optionSource: "fixed", options: ["None", "SOCKS4", "SOCKS5", "HTTP"] },
      { key: "proxy_ip", value: "127.0.0.1", tab: "connection", section: "proxy", control: "text" },
      { key: "proxy_auth_enabled", value: false, tab: "connection", section: "proxy", control: "switch" },
      { key: "proxy_password", value: "", tab: "connection", section: "proxy", control: "password", dependsOn: "proxy_auth_enabled" },
    ])
    renderDialog("connection")

    expect((await fieldFor("preferences.fields.proxyType")).textContent).toBe("preferences.fields.proxyTypeSocks5")
    const password = await fieldFor("preferences.fields.proxyPassword")
    expect(password.type).toBe("password")
    expect(password.disabled).toBe(true)
    fireEvent.click(await fieldFor("preferences.fields.proxyAuthEnabled"))
    fireEvent.change(password, { target: { value: "hunter2" } })
    expect(screen.getAllByRole("heading", { level: 3 }).map(heading => heading.textContent)).toEqual([
      "preferences.sections.i2p",
      "preferences.sections.proxy",
    ])
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([{ proxy_auth_enabled: true, proxy_password: "hunter2" }])
  })

  it("names the proxy types with authentication of qBittorrent 4.5 and earlier", async () => {
    serveView([
      { key: "proxy_type", value: 4, tab: "connection", section: "proxy", control: "select", optionSource: "fixed", options: [0, 5, 2, 4, 1, 3] },
      { key: "proxy_torrents_only", value: false, tab: "connection", section: "proxy", control: "switch" },
    ])
    renderDialog("connection")

    expect((await fieldFor("preferences.fields.proxyType")).textContent).toBe("preferences.fields.proxyTypeSocks5Auth")
    expect(await fieldFor("preferences.fields.proxyTorrentsOnly")).not.toBeNull()
  })

  it("shows the connection settings of the WebUI read-only, with one note on the username", async () => {
    const bodies = recordSaves()
    serveView([
      { key: "web_ui_port", value: 8080, tab: "webui", section: "webUI", control: "number", readOnly: true, reason: "qui connects to qBittorrent with this setting" },
      { key: "web_ui_username", value: "admin", tab: "webui", section: "authentication", control: "text", readOnly: true, reason: "qui connects to qBittorrent with this setting" },
      { key: "bypass_local_auth", value: false, tab: "webui", section: "authentication", control: "switch", readOnly: true, reason: "qui connects to qBittorrent with this setting" },
      { key: "web_ui_session_timeout", value: 3600, tab: "webui", section: "authentication", control: "number", wireUnit: "s", displayUnit: "s" },
      { key: "web_ui_sessions_count_limit", value: 0, tab: "webui", section: "authentication", control: "number" },
    ])
    renderDialog("webui")

    expect((await fieldFor("preferences.fields.webUIPort")).disabled).toBe(true)
    expect((await fieldFor("preferences.fields.webUIUsername")).disabled).toBe(true)
    expect((await fieldFor("preferences.fields.bypassLocalAuth")).disabled).toBe(true)
    expect(screen.getAllByText("preferences.fields.webUIConnectionNote")).toHaveLength(1)
    expect((await fieldFor("preferences.fields.webUISessionsCountLimit")).value).toBe("")
    fireEvent.change(await fieldFor("preferences.fields.webUISessionTimeout"), { target: { value: "7200" } })
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([{ web_ui_session_timeout: 7200 }])
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

// The network binding fields of qBittorrent 4.6 and later, with the saved values.
function bindingView(iface: string, address: string, name = ""): PreferenceField[] {
  return [
    { key: "current_network_interface", value: iface, tab: "advanced", section: "qbittorrent", control: "select", optionSource: "networkInterfaces" },
    { key: "current_interface_address", value: address, tab: "advanced", section: "qbittorrent", control: "select", optionSource: "interfaceAddresses" },
    { key: "current_interface_name", value: name, tab: "advanced", section: "qbittorrent", control: "text", readOnly: true, reason: "qBittorrent sets it from the network interface" },
    { key: "save_statistics_interval", value: 15, tab: "advanced", section: "qbittorrent", control: "number", wireUnit: "min", displayUnit: "min" },
  ]
}

const anyInterface: NetworkBindingInterface = {
  name: "", value: "", available: true,
  addresses: [{ address: "192.168.1.10", available: true }, { address: "10.8.0.2", available: true }],
}
const ethernet: NetworkBindingInterface = { name: "Ethernet", value: "eth0", available: true, addresses: [{ address: "192.168.1.10", available: true }] }
const wireGuard: NetworkBindingInterface = { name: "WireGuard", value: "wg0", available: true, addresses: [{ address: "10.8.0.2", available: true }] }

// Serves the interfaces, or a failure for null, and counts the requests.
function serveBinding(interfaces: NetworkBindingInterface[] | null) {
  const fetched = { count: 0 }
  server.use(http.get("*/api/instances/1/network-binding", () => {
    fetched.count++
    return interfaces ? HttpResponse.json(interfaces) : HttpResponse.text("qBittorrent is down", { status: 500 })
  }))
  return fetched
}

// Opens a select and returns the text of its options.
async function openSelect(trigger: HTMLElement) {
  fireEvent.keyDown(trigger, { key: "ArrowDown" })
  return (await screen.findAllByRole("option")).map(option => option.textContent)
}

const interfaceLabel = "preferences.connectionSettings.networkInterface"
const addressLabel = "preferences.fields.interfaceAddress"

describe("InstancePreferencesDialog network binding", () => {
  const scrollIntoView = Object.getOwnPropertyDescriptor(Element.prototype, "scrollIntoView")
  beforeEach(() => {
    // Radix scrolls focused select options; jsdom has no layout.
    Object.defineProperty(Element.prototype, "scrollIntoView", { configurable: true, value: vi.fn() })
  })
  afterEach(() => {
    if (scrollIntoView) Object.defineProperty(Element.prototype, "scrollIntoView", scrollIntoView)
    else Reflect.deleteProperty(Element.prototype, "scrollIntoView")
  })

  it("lists the addresses of the selected interface, and resets the address when the interface changes", async () => {
    const bodies = recordSaves()
    serveView(bindingView("eth0", "192.168.1.10", "Ethernet"))
    const fetched = serveBinding([anyInterface, ethernet, wireGuard])
    renderDialog("advanced")

    await waitFor(async () => expect((await fieldFor(interfaceLabel)).textContent).toBe("Ethernet"))
    expect((await fieldFor(addressLabel)).textContent).toBe("192.168.1.10")
    // current_interface_name shares the label of the interface, and has no row of its own.
    expect(screen.getAllByText(interfaceLabel)).toHaveLength(1)

    expect(await openSelect(await fieldFor(interfaceLabel))).toEqual(["preferences.networkBinding.anyInterface", "Ethernet", "WireGuard"])
    fireEvent.click(screen.getByRole("option", { name: "WireGuard" }))

    expect((await fieldFor(addressLabel)).textContent).toBe("preferences.networkBinding.allAddresses")
    expect(await openSelect(await fieldFor(addressLabel))).toEqual([
      "preferences.networkBinding.allAddresses",
      "preferences.networkBinding.allIPv4Addresses",
      "preferences.networkBinding.allIPv6Addresses",
      "10.8.0.2",
    ])
    fireEvent.click(screen.getByRole("option", { name: "10.8.0.2" }))
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([{ current_network_interface: "wg0", current_interface_address: "10.8.0.2" }])
    // The saved binding decides which entries are "not available", so the list loads again.
    await waitFor(() => expect(fetched.count).toBe(2))
  })

  it("keeps a saved interface and address that are not available, marked, when another setting saves", async () => {
    const bodies = recordSaves()
    serveView(bindingView("tun0", "10.9.0.2", "OpenVPN"))
    serveBinding([anyInterface, ethernet, wireGuard, {
      name: "OpenVPN", value: "tun0", available: false, addresses: [{ address: "10.9.0.2", available: false }],
    }])
    renderDialog("advanced")

    await waitFor(async () => expect((await fieldFor(interfaceLabel)).textContent).toBe("OpenVPN preferences.networkBinding.notAvailable"))
    expect((await fieldFor(addressLabel)).textContent).toBe("10.9.0.2 preferences.networkBinding.notAvailable")

    fireEvent.change(await fieldFor("preferences.fields.saveStatisticsInterval"), { target: { value: "30" } })
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([{ save_statistics_interval: 30 }])
  })

  it("shows the binding read-only when qui cannot list the interfaces, and still saves the other settings", async () => {
    const bodies = recordSaves()
    serveView(bindingView("tun0", "0.0.0.0", "OpenVPN"))
    serveBinding(null)
    renderDialog("advanced")

    const iface = await fieldFor(interfaceLabel)
    expect(iface.disabled).toBe(true)
    expect(iface.value).toBe("OpenVPN")
    const address = await fieldFor(addressLabel)
    expect(address.disabled).toBe(true)
    expect(address.value).toBe("preferences.networkBinding.allIPv4Addresses")

    fireEvent.change(await fieldFor("preferences.fields.saveStatisticsInterval"), { target: { value: "30" } })
    save()

    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1))
    expect(bodies).toEqual([{ save_statistics_interval: 30 }])
    // The fetch failed, so the read-only boxes stay.
    expect((await fieldFor(interfaceLabel)).disabled).toBe(true)
  })

  it("shows the interface selected in the form when the list loads again and fails", async () => {
    serveView(bindingView("eth0", "192.168.1.10", "Ethernet"))
    serveBinding([anyInterface, ethernet, wireGuard])
    renderDialog("advanced")

    await waitFor(async () => expect((await fieldFor(interfaceLabel)).textContent).toBe("Ethernet"))
    await openSelect(await fieldFor(interfaceLabel))
    fireEvent.click(screen.getByRole("option", { name: "WireGuard" }))

    // Another tab unmounts the widget, so the list loads again on the way back.
    serveBinding(null)
    fireEvent.mouseDown(screen.getByRole("tab", { name: "preferences.dialog.tabs.instance" }), { button: 0 })
    await waitFor(() => expect(screen.queryByText(interfaceLabel)).toBeNull())
    fireEvent.mouseDown(screen.getByRole("tab", { name: "preferences.dialog.tabs.advanced" }), { button: 0 })
    const iface = await fieldFor(interfaceLabel)
    await waitFor(() => expect(iface.disabled).toBe(true))
    // "Ethernet" is the name of the saved interface, so it does not apply to wg0.
    expect(iface.value).toBe("wg0")
  })

  it("shows the binding read-only when the list fails to load again after a save", async () => {
    recordSaves()
    serveView(bindingView("eth0", "192.168.1.10", "Ethernet"))
    serveBinding([anyInterface, ethernet, wireGuard])
    renderDialog("advanced")

    await waitFor(async () => expect((await fieldFor(interfaceLabel)).textContent).toBe("Ethernet"))
    const failed = serveBinding(null)
    fireEvent.change(await fieldFor("preferences.fields.saveStatisticsInterval"), { target: { value: "30" } })
    save()

    await waitFor(() => expect(failed.count).toBe(1))
    await waitFor(async () => expect((await fieldFor(interfaceLabel)).disabled).toBe(true))
    expect((await fieldFor(interfaceLabel)).value).toBe("Ethernet")
  })
})
