/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { ManualAssembleResponse, ManualCrossSeedProposalsResponse } from "@/types"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import { ManualCrossSeedDialog } from "./ManualCrossSeedDialog"

vi.mock("@/lib/api", () => ({ api: {
  getManualCrossSeedProposals: vi.fn(), checkManualAssemble: vi.fn(),
  applyManualAssemble: vi.fn(), applyManualCrossSeed: vi.fn(),
} }))
vi.mock("@/hooks/useInstanceMetadata", () => ({ useInstanceMetadata: () => ({ data: undefined }) }))
vi.mock("@/lib/manual-cross-seed", () => ({ fileToBase64: async () => "torrent", overlapPercent: (fraction: number) => Math.round(fraction * 100) }))
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }))
vi.mock("sonner", () => ({ toast: { success: vi.fn(), warning: vi.fn(), error: vi.fn() } }))

afterEach(() => { cleanup(); vi.resetAllMocks() })

const proposals: ManualCrossSeedProposalsResponse = {
  packMode: true, packEpisodeCount: 4, assemblyUnavailableReason: "", proposalLimit: 14, proposalsTruncated: false,
  sourceName: "Cedar.Harbor.S01", sourceSize: 400, sourceFileCount: 4, defaultTags: ["cross-seed"], pinnedCategory: "pinned",
  proposals: [1, 2].map(i => ({ hash: `e${i}`, name: `Cedar Harbor episode ${i}`, size: 100, category: "tv", effectiveSavePath: "/old-path", overlapBytes: 100, overlapFraction: 0.25 })),
}
const preview: ManualAssembleResponse = {
  ready: true, applied: false, reason: "", message: "", targets: proposals.proposals.map(p => ({ hash: p.hash, name: p.name, reason: "" })),
  matchedEpisodes: 2, totalEpisodes: 4, coverage: 0.5, linkedBytes: 200, missingBytes: 200, destination: "/links/local", defaultCategory: "pack", linkMode: "hardlink",
}

function openDialog() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  return render(<QueryClientProvider client={client}><ManualCrossSeedDialog instanceId={1} open onOpenChange={() => {}} initialFile={new File(["torrent"], "pack.torrent")} /></QueryClientProvider>)
}

it("waits for the current preview and applies the selected set with editable category and tags", async () => {
  vi.mocked(api.getManualCrossSeedProposals).mockResolvedValue(proposals)
  let finishCheck: (response: ManualAssembleResponse) => void = () => {}
  vi.mocked(api.checkManualAssemble).mockImplementation(req => req.targetHashes.length === 0
    ? Promise.resolve(preview)
    : new Promise(resolve => { finishCheck = resolve }))
  vi.mocked(api.applyManualAssemble).mockResolvedValue({ ...preview, applied: true })
  const ui = openDialog()
  await waitFor(() => expect(api.checkManualAssemble).toHaveBeenCalledTimes(2))
  expect(ui.getByRole("button", { name: "manualCrossSeed.apply" }).hasAttribute("disabled")).toBe(true)
  finishCheck(preview)
  await waitFor(() => expect(ui.getByText("/links/local")).toBeTruthy())
  expect(ui.queryByText("/old-path")).toBeNull()
  expect(ui.getByRole("combobox").hasAttribute("disabled")).toBe(false)
  fireEvent.change(ui.getByLabelText("manualCrossSeed.tags"), { target: { value: "chosen" } })
  fireEvent.keyDown(ui.getByLabelText("manualCrossSeed.tags"), { key: "Enter" })
  fireEvent.click(ui.getByRole("button", { name: "manualCrossSeed.apply" }))
  await waitFor(() => expect(api.applyManualAssemble).toHaveBeenCalledWith({ instanceId: 1, torrentData: "torrent", targetHashes: ["e1", "e2"], category: "pack", tags: ["cross-seed", "chosen"] }))
  expect(api.applyManualCrossSeed).not.toHaveBeenCalled()
})

it("uses the original apply for one target and permits clearing a pack selection", async () => {
  vi.mocked(api.getManualCrossSeedProposals).mockResolvedValue(proposals)
  vi.mocked(api.checkManualAssemble).mockResolvedValue({ ...preview, targets: [preview.targets[0]] })
  vi.mocked(api.applyManualCrossSeed).mockResolvedValue({ success: true, results: [] })
  const ui = openDialog()
  await waitFor(() => expect((ui.getByRole("checkbox", { name: proposals.proposals[0].name }) as HTMLInputElement).checked).toBe(true))
  expect(ui.getByText("/old-path")).toBeTruthy()
  fireEvent.click(ui.getByRole("checkbox", { name: proposals.proposals[0].name }))
  expect(ui.getByRole("button", { name: "manualCrossSeed.apply" }).hasAttribute("disabled")).toBe(true)
  fireEvent.click(ui.getByRole("checkbox", { name: proposals.proposals[1].name }))
  fireEvent.click(ui.getByRole("button", { name: "manualCrossSeed.apply" }))
  await waitFor(() => expect(api.applyManualCrossSeed).toHaveBeenCalledWith({ instanceId: 1, torrentData: "torrent", targetHash: "e2", category: "pinned", tags: ["cross-seed"] }))
  expect(api.applyManualAssemble).not.toHaveBeenCalled()
})

it("shows disabled pack checkboxes with a reason while retaining single-target selection", async () => {
  vi.mocked(api.getManualCrossSeedProposals).mockResolvedValue({ ...proposals, assemblyUnavailableReason: "no_link_mode" })
  vi.mocked(api.checkManualAssemble).mockResolvedValue({ ...preview, ready: false, targets: [], reason: "no_link_mode" })
  const ui = openDialog()
  await waitFor(() => expect(ui.getAllByRole("checkbox")).toHaveLength(2))
  expect(ui.getAllByRole("checkbox").every(box => box.hasAttribute("disabled"))).toBe(true)
  expect(ui.getByText("manualCrossSeed.pack.reasons.no_link_mode")).toBeTruthy()
  expect(ui.getByRole("button", { name: "manualCrossSeed.apply" }).hasAttribute("disabled")).toBe(false)
})
