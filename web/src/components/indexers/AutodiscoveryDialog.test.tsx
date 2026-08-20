/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { AutodiscoveryDialog } from "@/components/indexers/AutodiscoveryDialog"
import { api } from "@/lib/api"
import type { JackettIndexer, TorznabIndexer } from "@/types"
import { fireEvent, render, screen } from "@testing-library/react"
import { expect, it, vi } from "vitest"

vi.mock("@/lib/api")
vi.mock("@/components/ui/scroll-area", () => ({ ScrollArea: "div" }))
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }))

it("imports only the selected usable row into its matching connection", async () => {
  vi.mocked(api.discoverJackettIndexers).mockResolvedValue({
    indexers: [
      { id: "shared", name: "Target", backend: "prowlarr" },
      { id: "shared", name: "Other", backend: "prowlarr" },
      { id: " ", name: "Missing", backend: "prowlarr" },
    ] as JackettIndexer[],
  })
  const existing = [
    { id: 9, name: "Wrong", base_url: "http://localhost:9696/", backend: "prowlarr", indexer_id: "shared" },
    { id: 2, name: "Target", base_url: "http://localhost:9696/", backend: "prowlarr", indexer_id: "shared" },
    { id: 4, name: "Target", base_url: "http://other:9696", backend: "prowlarr", indexer_id: "shared" },
    { id: 3, name: "Target", base_url: "http://localhost:9696", backend: "jackett", indexer_id: "shared" },
    { id: 1, name: "Target", base_url: "http://localhost:9696", backend: "prowlarr", indexer_id: "other" },
  ] as TorznabIndexer[]
  vi.mocked(api.listTorznabIndexers).mockResolvedValue(existing)
  vi.mocked(api.updateTorznabIndexer).mockResolvedValue(existing[1])

  render(<AutodiscoveryDialog open onClose={vi.fn()} indexers={[existing[0]]} />)
  fireEvent.click(screen.getByText("http://localhost:9696/"))
  fireEvent.click(screen.getByText("indexers.autodiscovery.buttons.discover"))

  const checkboxes = await screen.findAllByRole("checkbox")
  expect(screen.queryByText("Missing")).toBeNull()
  expect(checkboxes).toHaveLength(2)
  fireEvent.click(checkboxes[0])
  fireEvent.click(screen.getByText("indexers.autodiscovery.buttons.import"))

  await screen.findByText("indexers.autodiscovery.buttons.discover")
  expect(api.updateTorznabIndexer).toHaveBeenCalledExactlyOnceWith(2, expect.objectContaining({
    backend: "prowlarr",
    base_url: "http://localhost:9696",
    indexer_id: "shared",
  }))
  expect(api.createTorznabIndexer).not.toHaveBeenCalled()
})
