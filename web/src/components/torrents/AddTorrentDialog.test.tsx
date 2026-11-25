/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TooltipProvider } from "@/components/ui/tooltip"
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
  Element.prototype.scrollIntoView = vi.fn()
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

function renderColdDialog() {
  vi.mocked(api.checkTorrentDuplicates).mockResolvedValue({ duplicates: [] })
  vi.mocked(api.addTorrent).mockResolvedValue({ added: 1, failed: 0, message: "" })
  vi.mocked(api.getInstancePreferenceView).mockReturnValue(new Promise(() => {}))

  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  const dialog = (dropPayload: AddTorrentDropPayload | null) => (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <AddTorrentDialog instanceId={1} open onOpenChange={() => {}} dropPayload={dropPayload} />
      </TooltipProvider>
    </QueryClientProvider>
  )

  const ui = render(dialog({ type: "url", urls: [magnet] }))
  return {
    ...ui,
    resolvePreferences(resolved: AppPreferences) {
      client.setQueryData(["instance-preferences", 1], resolved)
      ui.rerender(dialog(null))
    },
    openMoreOptions() {
      fireEvent.click(ui.getByRole("button", { name: /addTorrentDialog\.options\.moreOptions/ }))
    },
  }
}

it("seeds the option defaults from preferences that resolve after a cold mount", async () => {
  const ui = renderColdDialog()

  ui.resolvePreferences(preferences)
  ui.openMoreOptions()

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
    urls: [magnet],
  })
})

it("keeps a change made before preferences resolve", async () => {
  const ui = renderColdDialog()
  ui.openMoreOptions()

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

  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  client.setQueryData(["instance-preferences", 1], preferences)
  const dialog = (open: boolean, dropPayload: AddTorrentDropPayload | null) => (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <AddTorrentDialog instanceId={1} open={open} onOpenChange={() => {}} dropPayload={dropPayload} />
      </TooltipProvider>
    </QueryClientProvider>
  )

  const ui = render(dialog(false, null))
  ui.rerender(dialog(true, { type: "url", urls: [magnet] }))
  await waitFor(() => expect(api.getInstancePreferenceView).toHaveBeenCalled())
  resolveView([{ key: "auto_tmm_enabled", value: false }, { key: "save_path", value: "/data/changed" }] as PreferenceField[])
  ui.rerender(dialog(true, null))
  fireEvent.click(ui.getByRole("button", { name: /addTorrentDialog\.options\.moreOptions/ }))

  await waitFor(() => expect((ui.getByLabelText("addTorrentDialog.options.savePath") as HTMLInputElement).value).toBe("/data/changed"))
})

it("sends the category and tags picked in the pickers, a new tag included", async () => {
  metadataResult.data = {
    categories: { "tv/x265": { name: "tv/x265", savePath: "/data/tv" } } as never,
    tags: ["batch-2", "batch-10"],
  }
  const ui = renderColdDialog()

  const categoryTrigger = ui.getByRole("combobox", { name: "addTorrentDialog.options.category" })
  fireEvent.click(categoryTrigger)
  fireEvent.click(await waitFor(() => ui.getByRole("option", { name: /tv\/x265/ })))
  await waitFor(() => expect(document.activeElement).toBe(categoryTrigger))

  fireEvent.click(ui.getByRole("button", { name: /addTorrentDialog\.options\.addTags/ }))
  const options = await waitFor(() => ui.getAllByRole("option"))
  expect(options.map(option => option.textContent)).toEqual(["batch-2", "batch-10"])
  fireEvent.click(options[1])
  fireEvent.change(ui.getByPlaceholderText("addTorrentDialog.options.tagsPlaceholder"), { target: { value: "fresh" } })
  fireEvent.click(ui.getByRole("button", { name: "addTorrentDialog.options.createTag" }))

  fireEvent.click(ui.getByRole("button", { name: "addTorrentDialog.footer.add" }))
  await waitFor(() => expect(api.addTorrent).toHaveBeenCalled())
  expect(vi.mocked(api.addTorrent).mock.calls[0][1]).toMatchObject({
    category: "tv/x265",
    tags: ["batch-10", "fresh"],
    urls: [magnet],
  })
})

it("picks the matching tag on Enter and adds typed names and comma lists", async () => {
  metadataResult.data = { categories: {}, tags: ["autobrr", "radarr", "sonarr"] }
  const ui = renderColdDialog()

  fireEvent.click(ui.getByRole("button", { name: /addTorrentDialog\.options\.addTags/ }))
  const search = await waitFor(() => ui.getByPlaceholderText("addTorrentDialog.options.tagsPlaceholder"))
  fireEvent.change(search, { target: { value: "aut" } })
  await waitFor(() => expect(ui.getByRole("option", { name: "autobrr" }).getAttribute("aria-selected")).toBe("true"))
  fireEvent.keyDown(search, { key: "Enter" })
  fireEvent.change(search, { target: { value: "radarr, sonarr ,sonarr" } })
  fireEvent.keyDown(search, { key: "Enter" })
  fireEvent.change(search, { target: { value: "fresh" } })
  fireEvent.keyDown(search, { key: "Enter" })
  fireEvent.change(search, { target: { value: "auto" } })
  await waitFor(() => expect(ui.getByRole("option", { name: /^autobrr/ }).getAttribute("aria-selected")).toBe("true"))
  fireEvent.keyDown(search, { key: "Enter" })

  fireEvent.click(ui.getByRole("button", { name: "addTorrentDialog.footer.add" }))
  await waitFor(() => expect(api.addTorrent).toHaveBeenCalled())
  expect(vi.mocked(api.addTorrent).mock.calls[0][1]).toMatchObject({ tags: ["autobrr", "radarr", "sonarr", "fresh"] })
})

it("presses a focused button on Enter instead of the highlighted tag", async () => {
  metadataResult.data = { categories: {}, tags: ["rule-1"] }
  const ui = renderColdDialog()

  fireEvent.click(ui.getByRole("button", { name: /addTorrentDialog\.options\.addTags/ }))
  const search = await waitFor(() => ui.getByPlaceholderText("addTorrentDialog.options.tagsPlaceholder"))
  fireEvent.change(search, { target: { value: "rule" } })
  await waitFor(() => expect(ui.getByRole("option", { name: /rule-1/ }).getAttribute("aria-selected")).toBe("true"))
  const createButton = ui.getByRole("button", { name: "addTorrentDialog.options.createTag" })
  createButton.focus()
  fireEvent.keyDown(createButton, { key: "Enter" })
  fireEvent.click(createButton)

  fireEvent.click(ui.getByRole("button", { name: "addTorrentDialog.footer.add" }))
  await waitFor(() => expect(api.addTorrent).toHaveBeenCalled())
  expect(vi.mocked(api.addTorrent).mock.calls[0][1]).toMatchObject({ tags: ["rule"] })
})

it("adds the tag picked with the arrow keys and Enter", async () => {
  metadataResult.data = { categories: {}, tags: ["autobrr", "radarr"] }
  const ui = renderColdDialog()

  fireEvent.click(ui.getByRole("button", { name: /addTorrentDialog\.options\.addTags/ }))
  const search = await waitFor(() => ui.getByPlaceholderText("addTorrentDialog.options.tagsPlaceholder"))
  fireEvent.keyDown(search, { key: "ArrowDown" })
  await waitFor(() => expect(ui.getByRole("option", { name: "radarr" }).getAttribute("aria-selected")).toBe("true"))
  fireEvent.keyDown(search, { key: "Enter" })
  expect(ui.getByRole("option", { name: /^radarr\s*addTorrentDialog\.options\.selectedState$/ })).toBeTruthy()

  fireEvent.click(ui.getByRole("button", { name: "addTorrentDialog.footer.add" }))
  await waitFor(() => expect(api.addTorrent).toHaveBeenCalled())
  expect(vi.mocked(api.addTorrent).mock.calls[0][1]).toMatchObject({ tags: ["radarr"] })
})

it("ignores Return on an empty tag search and matches every word of a category search", async () => {
  metadataResult.data = {
    categories: { "movies/x265": { name: "movies/x265", savePath: "" }, "tv/x265": { name: "tv/x265", savePath: "" } } as never,
    tags: ["autobrr"],
  }
  const ui = renderColdDialog()

  fireEvent.click(ui.getByRole("button", { name: /addTorrentDialog\.options\.addTags/ }))
  const search = await waitFor(() => ui.getByPlaceholderText("addTorrentDialog.options.tagsPlaceholder"))
  fireEvent.keyDown(search, { key: "Enter" })
  fireEvent.keyDown(search, { key: "Escape" })

  const categoryTrigger = ui.getByRole("combobox", { name: "addTorrentDialog.options.category" })
  await waitFor(() => expect(document.activeElement).not.toBe(search))
  fireEvent.click(categoryTrigger)
  fireEvent.change(await waitFor(() => ui.getByPlaceholderText("addTorrentDialog.options.searchCategories")), { target: { value: "tv x265" } })
  const options = ui.getAllByRole("option")
  expect(options).toHaveLength(1)
  expect(options[0].textContent).toMatch(/^tv\/x265/)
  fireEvent.click(ui.getByRole("option", { name: /tv\/x265/ }))

  fireEvent.click(ui.getByRole("button", { name: "addTorrentDialog.footer.add" }))
  await waitFor(() => expect(api.addTorrent).toHaveBeenCalled())
  const sent = vi.mocked(api.addTorrent).mock.calls[0][1]
  expect(sent.tags).toBeUndefined()
  expect(sent.category).toBe("tv/x265")
})

it("leaves Return alone while an input method composes text", async () => {
  metadataResult.data = { categories: {}, tags: ["autobrr"] }
  const ui = renderColdDialog()

  fireEvent.click(ui.getByRole("button", { name: /addTorrentDialog\.options\.addTags/ }))
  const search = await waitFor(() => ui.getByPlaceholderText("addTorrentDialog.options.tagsPlaceholder"))
  fireEvent.change(search, { target: { value: "にほん" } })
  fireEvent.keyDown(search, { key: "Enter", isComposing: true })

  fireEvent.click(ui.getByRole("button", { name: "addTorrentDialog.footer.add" }))
  await waitFor(() => expect(api.addTorrent).toHaveBeenCalled())
  expect(vi.mocked(api.addTorrent).mock.calls[0][1].tags).toBeUndefined()
})

it("keeps a tag picked with the arrow keys when Return is pressed again", async () => {
  metadataResult.data = { categories: {}, tags: ["autobrr"] }
  const ui = renderColdDialog()

  fireEvent.click(ui.getByRole("button", { name: /addTorrentDialog\.options\.addTags/ }))
  const search = await waitFor(() => ui.getByPlaceholderText("addTorrentDialog.options.tagsPlaceholder"))
  fireEvent.change(search, { target: { value: "auto" } })
  fireEvent.keyDown(search, { key: "ArrowDown" })
  fireEvent.keyDown(search, { key: "Enter" })
  fireEvent.keyDown(search, { key: "Enter" })

  fireEvent.click(ui.getByRole("button", { name: "addTorrentDialog.footer.add" }))
  await waitFor(() => expect(api.addTorrent).toHaveBeenCalled())
  expect(vi.mocked(api.addTorrent).mock.calls[0][1]).toMatchObject({ tags: ["autobrr"] })
})

it("starts with More options closed on each open", async () => {
  vi.mocked(api.getInstancePreferenceView).mockReturnValue(new Promise(() => {}))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  const dialog = (open: boolean) => (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <AddTorrentDialog instanceId={1} open={open} onOpenChange={() => {}} />
      </TooltipProvider>
    </QueryClientProvider>
  )

  const ui = render(dialog(true))
  fireEvent.click(ui.getByRole("button", { name: /addTorrentDialog\.options\.moreOptions/ }))
  await waitFor(() => ui.getByLabelText("addTorrentDialog.options.autoTmm"))
  ui.rerender(dialog(false))
  ui.rerender(dialog(true))

  expect(ui.queryByLabelText("addTorrentDialog.options.autoTmm")).toBeNull()
})
