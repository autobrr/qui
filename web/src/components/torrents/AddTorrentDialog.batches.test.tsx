/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TooltipProvider } from "@/components/ui/tooltip"
import { addFilesInBatches, BatchAddError } from "@/lib/add-torrent-files"
import { api } from "@/lib/api"
import type { AddTorrentResponse, AppPreferences } from "@/types"
import { onlineManager, QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react"
import { toast } from "sonner"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { AddTorrentDialog, type AddTorrentDropPayload } from "./AddTorrentDialog"

vi.mock("@/lib/api", () => ({ api: {
  addTorrent: vi.fn(),
  checkTorrentDuplicates: vi.fn(),
  getInstancePreferenceView: vi.fn(),
} }))
vi.mock("@/lib/add-torrent-files", async importOriginal => ({ ...await importOriginal<typeof import("@/lib/add-torrent-files")>(), addFilesInBatches: vi.fn() }))
vi.mock("@/hooks/useInstanceMetadata", () => ({ useInstanceMetadata: () => ({ data: undefined }) }))
vi.mock("@/hooks/useInstanceCapabilities", () => ({ useInstanceCapabilities: () => ({ data: { supportsTorrentTmpPath: true, supportsPathAutocomplete: false } }) }))
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string, values?: { current?: number; total?: number }) => values?.current ? `${key} ${values.current}/${values.total}` : key }) }))
vi.mock("sonner", () => ({ toast: { success: vi.fn(), warning: vi.fn(), error: vi.fn() } }))

beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn()
})

afterEach(() => {
  cleanup()
  onlineManager.setOnline(true)
  vi.resetAllMocks()
})

const preferences = {
  auto_tmm_enabled: false,
  save_path: "/data/complete",
  torrent_content_layout: "Subfolder",
  temp_path_enabled: true,
  temp_path: "/data/incomplete",
} as AppPreferences

it("shows which batch it adds while it sends the dropped files in batches", async () => {
  vi.mocked(api.checkTorrentDuplicates).mockResolvedValue({ duplicates: [] })
  vi.mocked(api.getInstancePreferenceView).mockReturnValue(new Promise(() => {}))
  let finish: (response: AddTorrentResponse) => void = () => {}
  vi.mocked(addFilesInBatches).mockImplementation((_add, _base, _files, onBatch) => {
    onBatch(2, 3)
    return new Promise(resolve => { finish = resolve })
  })

  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  client.setQueryData(["instance-preferences", 1], preferences)
  const files = [new File(["x"], "Example.Show.S01E01.torrent"), new File(["x"], "Example.Show.S01E02.torrent")]
  const ui = render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <AddTorrentDialog instanceId={1} open onOpenChange={() => {}} dropPayload={{ type: "file", files }} />
      </TooltipProvider>
    </QueryClientProvider>
  )

  fireEvent.click(await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.addCount" })))

  await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.addingBatch 2/3" }))
  expect(vi.mocked(addFilesInBatches).mock.calls[0][2]).toEqual(files)
  finish({ added: 2, failed: 0, message: "" })
  await waitFor(() => expect(toast.success).toHaveBeenCalled())
})

it("keeps only the unconfirmed files in the dialog when a batch fails, so Add sends just those again", async () => {
  vi.mocked(api.checkTorrentDuplicates).mockResolvedValue({ duplicates: [] })
  vi.mocked(api.getInstancePreferenceView).mockReturnValue(new Promise(() => {}))
  const files = ["Example.Show.S01E01.torrent", "Example.Show.S01E02.torrent", "Example.Show.S01E03.torrent"].map(name => new File(["x"], name))
  vi.mocked(addFilesInBatches)
    .mockRejectedValueOnce(new BatchAddError("HTTP error! status: 504", { added: 1, total: 3, batch: 2, batches: 3, unconfirmed: files.slice(1), failedFiles: [{ filename: "Example.Show.S01E02.torrent", error: "torrent file is not valid" }] }))
    .mockResolvedValueOnce({ added: 2, failed: 0, message: "" })

  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  client.setQueryData(["instance-preferences", 1], preferences)
  const dialog = (dropPayload: AddTorrentDropPayload | null) => (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <AddTorrentDialog instanceId={1} open onOpenChange={() => {}} dropPayload={dropPayload} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  const ui = render(dialog({ type: "file", files }))
  ui.rerender(dialog(null))

  fireEvent.click(await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.addCount" })))

  await waitFor(() => expect(toast.error).toHaveBeenCalledWith("addTorrentDialog.toast.batchFailed", expect.objectContaining({ description: expect.stringContaining("Example.Show.S01E02.torrent: torrent file is not valid") })))
  await waitFor(() => expect(ui.queryByText("Example.Show.S01E01.torrent")).toBeNull())
  expect(ui.getByText("Example.Show.S01E02.torrent")).not.toBeNull()

  fireEvent.click(ui.getByRole("button", { name: "addTorrentDialog.footer.addCount" }))
  await waitFor(() => expect(addFilesInBatches).toHaveBeenCalledTimes(2))
  expect(vi.mocked(addFilesInBatches).mock.calls[1][2]).toEqual(files.slice(1))
})

it("keeps a file added while the batches run when a batch fails", async () => {
  vi.mocked(api.checkTorrentDuplicates).mockResolvedValue({ duplicates: [] })
  vi.mocked(api.getInstancePreferenceView).mockReturnValue(new Promise(() => {}))
  const files = ["Example.Show.S01E01.torrent", "Example.Show.S01E02.torrent"].map(name => new File(["x"], name))
  const late = new File(["x"], "Example.Show.S01E09.torrent")
  let fail: (error: unknown) => void = () => {}
  vi.mocked(addFilesInBatches).mockReturnValueOnce(new Promise((_, reject) => { fail = reject }))

  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  client.setQueryData(["instance-preferences", 1], preferences)
  const dialog = (dropPayload: AddTorrentDropPayload | null) => (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <AddTorrentDialog instanceId={1} open onOpenChange={() => {}} dropPayload={dropPayload} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  const ui = render(dialog({ type: "file", files }))
  ui.rerender(dialog(null))

  fireEvent.click(await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.addCount" })))
  await waitFor(() => expect(addFilesInBatches).toHaveBeenCalled())
  const input = ui.baseElement.querySelector<HTMLInputElement>("input#torrentFiles")
  if (!input) throw new Error("file input missing")
  fireEvent.change(input, { target: { files: [late] } })
  await waitFor(() => ui.getByText("Example.Show.S01E09.torrent"))

  fail(new BatchAddError("HTTP error! status: 504", { added: 1, total: 2, batch: 2, batches: 2, unconfirmed: files.slice(1), failedFiles: [] }))

  await waitFor(() => expect(toast.error).toHaveBeenCalled())
  await waitFor(() => expect(ui.queryByText("Example.Show.S01E01.torrent")).toBeNull())
  expect(ui.getByText("Example.Show.S01E02.torrent")).not.toBeNull()
  expect(ui.getByText("Example.Show.S01E09.torrent")).not.toBeNull()
})

it("leaves the files of a dialog opened again alone when the batch of the closed one fails", async () => {
  vi.mocked(api.checkTorrentDuplicates).mockResolvedValue({ duplicates: [] })
  vi.mocked(api.getInstancePreferenceView).mockReturnValue(new Promise(() => {}))
  const files = ["Example.Show.S01E01.torrent", "Example.Show.S01E02.torrent"].map(name => new File(["x"], name))
  const next = new File(["x"], "Other.Show.S02E01.torrent")
  let fail: (error: unknown) => void = () => {}
  vi.mocked(addFilesInBatches).mockReturnValueOnce(new Promise((_, reject) => { fail = reject }))

  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  client.setQueryData(["instance-preferences", 1], preferences)
  const dialog = (open: boolean, dropPayload: AddTorrentDropPayload | null) => (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <AddTorrentDialog instanceId={1} open={open} onOpenChange={() => {}} dropPayload={dropPayload} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  const ui = render(dialog(true, { type: "file", files }))
  ui.rerender(dialog(true, null))

  fireEvent.click(await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.addCount" })))
  await waitFor(() => expect(addFilesInBatches).toHaveBeenCalled())
  ui.rerender(dialog(false, null))
  ui.rerender(dialog(true, { type: "file", files: [next] }))
  ui.rerender(dialog(true, null))
  await waitFor(() => ui.getByText("Other.Show.S02E01.torrent"))

  fail(new BatchAddError("HTTP error! status: 504", { added: 1, total: 2, batch: 2, batches: 2, unconfirmed: files.slice(1), failedFiles: [] }))

  await waitFor(() => expect(toast.error).toHaveBeenCalled())
  expect(ui.queryByText("Example.Show.S01E02.torrent")).toBeNull()
  expect(ui.getByText("Other.Show.S02E01.torrent")).not.toBeNull()
})

it("opens a fresh dialog while the add of the closed one still runs, and keeps it open when that add ends", async () => {
  vi.mocked(api.checkTorrentDuplicates).mockResolvedValue({ duplicates: [] })
  vi.mocked(api.getInstancePreferenceView).mockReturnValue(new Promise(() => {}))
  const files = ["Example.Show.S01E01.torrent", "Example.Show.S01E02.torrent"].map(name => new File(["x"], name))
  const next = new File(["x"], "Other.Show.S02E01.torrent")
  let finish: (response: AddTorrentResponse) => void = () => {}
  let batch: (current: number, total: number) => void = () => {}
  vi.mocked(addFilesInBatches).mockImplementationOnce((_add, _base, _files, onBatch) => {
    batch = onBatch
    onBatch(2, 3)
    return new Promise(resolve => { finish = resolve })
  })
  const onOpenChange = vi.fn()

  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  client.setQueryData(["instance-preferences", 1], preferences)
  const dialog = (open: boolean, dropPayload: AddTorrentDropPayload | null) => (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <AddTorrentDialog instanceId={1} open={open} onOpenChange={onOpenChange} dropPayload={dropPayload} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  const ui = render(dialog(true, { type: "file", files }))
  ui.rerender(dialog(true, null))

  fireEvent.click(await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.addCount" })))
  await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.addingBatch 2/3" }))
  ui.rerender(dialog(false, null))
  ui.rerender(dialog(true, { type: "file", files: [next] }))
  ui.rerender(dialog(true, null))
  await waitFor(() => ui.getByText("Other.Show.S02E01.torrent"))

  act(() => batch(3, 3))
  const add = ui.getByRole("button", { name: "addTorrentDialog.footer.add" })
  expect(add.hasAttribute("disabled")).toBe(false)

  finish({ added: 2, failed: 0, message: "" })

  await waitFor(() => expect(toast.success).toHaveBeenCalled())
  expect(onOpenChange).not.toHaveBeenCalledWith(false)
  expect(ui.getByText("Other.Show.S02E01.torrent")).not.toBeNull()
  expect(ui.getByRole("button", { name: "addTorrentDialog.footer.add" }).hasAttribute("disabled")).toBe(false)
})

it("shows only the batches of its own add while the add of the closed dialog moves on and ends", async () => {
  vi.mocked(api.checkTorrentDuplicates).mockResolvedValue({ duplicates: [] })
  vi.mocked(api.getInstancePreferenceView).mockReturnValue(new Promise(() => {}))
  const files = ["Example.Show.S01E01.torrent", "Example.Show.S01E02.torrent"].map(name => new File(["x"], name))
  const nextFiles = ["Other.Show.S02E01.torrent", "Other.Show.S02E02.torrent"].map(name => new File(["x"], name))
  let finish: (response: AddTorrentResponse) => void = () => {}
  let batch: (current: number, total: number) => void = () => {}
  vi.mocked(addFilesInBatches)
    .mockImplementationOnce((_add, _base, _files, onBatch) => {
      batch = onBatch
      onBatch(2, 3)
      return new Promise(resolve => { finish = resolve })
    })
    .mockImplementationOnce((_add, _base, _files, onBatch) => {
      onBatch(1, 2)
      return new Promise(() => {})
    })

  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  client.setQueryData(["instance-preferences", 1], preferences)
  const dialog = (open: boolean, dropPayload: AddTorrentDropPayload | null) => (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <AddTorrentDialog instanceId={1} open={open} onOpenChange={() => {}} dropPayload={dropPayload} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  const ui = render(dialog(true, { type: "file", files }))
  ui.rerender(dialog(true, null))

  fireEvent.click(await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.addCount" })))
  await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.addingBatch 2/3" }))
  ui.rerender(dialog(false, null))
  ui.rerender(dialog(true, { type: "file", files: nextFiles }))
  ui.rerender(dialog(true, null))

  fireEvent.click(await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.addCount" })))
  await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.addingBatch 1/2" }))

  act(() => batch(3, 3))
  expect(ui.getByRole("button", { name: "addTorrentDialog.footer.addingBatch 1/2" })).not.toBeNull()

  finish({ added: 2, failed: 0, message: "" })
  await waitFor(() => expect(toast.success).toHaveBeenCalled())
  expect(ui.getByRole("button", { name: "addTorrentDialog.footer.addingBatch 1/2" })).not.toBeNull()
})

it("shows no batch of the closed dialog while a reopened dialog adds a link", async () => {
  vi.mocked(api.checkTorrentDuplicates).mockResolvedValue({ duplicates: [] })
  vi.mocked(api.getInstancePreferenceView).mockReturnValue(new Promise(() => {}))
  vi.mocked(api.addTorrent).mockReturnValue(new Promise(() => {}))
  const files = ["Example.Show.S01E01.torrent", "Example.Show.S01E02.torrent"].map(name => new File(["x"], name))
  vi.mocked(addFilesInBatches).mockImplementationOnce((_add, _base, _files, onBatch) => {
    onBatch(2, 3)
    return new Promise(() => {})
  })

  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  client.setQueryData(["instance-preferences", 1], preferences)
  const dialog = (open: boolean, dropPayload: AddTorrentDropPayload | null) => (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <AddTorrentDialog instanceId={1} open={open} onOpenChange={() => {}} dropPayload={dropPayload} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  const ui = render(dialog(true, { type: "file", files }))
  ui.rerender(dialog(true, null))

  fireEvent.click(await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.addCount" })))
  await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.addingBatch 2/3" }))
  ui.rerender(dialog(false, null))
  ui.rerender(dialog(true, { type: "url", urls: ["magnet:?xt=urn:btih:0000000000000000000000000000000000000001"] }))
  ui.rerender(dialog(true, null))

  fireEvent.click(await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.add" })))

  await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.adding" }))
  expect(ui.queryByRole("button", { name: "addTorrentDialog.footer.addingBatch 2/3" })).toBeNull()
})

it("shows no batch of an add that waited for the network while a reopened dialog adds a link", async () => {
  vi.mocked(api.checkTorrentDuplicates).mockResolvedValue({ duplicates: [] })
  vi.mocked(api.getInstancePreferenceView).mockReturnValue(new Promise(() => {}))
  vi.mocked(api.addTorrent).mockReturnValue(new Promise(() => {}))
  const files = ["Example.Show.S01E01.torrent", "Example.Show.S01E02.torrent"].map(name => new File(["x"], name))
  vi.mocked(addFilesInBatches).mockImplementationOnce((_add, _base, _files, onBatch) => {
    onBatch(2, 3)
    return new Promise(() => {})
  })

  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  client.setQueryData(["instance-preferences", 1], preferences)
  const dialog = (open: boolean, dropPayload: AddTorrentDropPayload | null) => (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <AddTorrentDialog instanceId={1} open={open} onOpenChange={() => {}} dropPayload={dropPayload} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  const ui = render(dialog(true, { type: "file", files }))
  ui.rerender(dialog(true, null))

  onlineManager.setOnline(false)
  fireEvent.click(await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.addCount" })))
  await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.adding" }))
  ui.rerender(dialog(false, null))
  ui.rerender(dialog(true, { type: "url", urls: ["magnet:?xt=urn:btih:0000000000000000000000000000000000000001"] }))
  ui.rerender(dialog(true, null))

  act(() => onlineManager.setOnline(true))
  await waitFor(() => expect(addFilesInBatches).toHaveBeenCalled())

  fireEvent.click(await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.add" })))

  await waitFor(() => ui.getByRole("button", { name: "addTorrentDialog.footer.adding" }))
  expect(ui.queryByRole("button", { name: "addTorrentDialog.footer.addingBatch 2/3" })).toBeNull()
})
