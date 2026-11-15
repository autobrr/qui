/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { TorrentProperties } from "@/types"
import { makeTorrent } from "@/test/mockTorrent"
import { TooltipProvider } from "@/components/ui/tooltip"
import { GeneralTabHorizontal } from "./GeneralTabHorizontal"
import "@/i18n"


vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock("@/hooks/useTorrentDetailTab", () => ({
  useTorrentDetailTab: () => ({
    data: undefined,
    isLoading: false,
    error: null,
  }),
}))

vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => ({
    formatTimestamp: () => "—",
  }),
}))

vi.stubGlobal("ResizeObserver", class {
  observe() {}
  unobserve() {}
  disconnect() {}
})

afterEach(() => {
  cleanup()
})

function renderGeneralTab(displayComment?: string, displayCreatedBy?: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider delayDuration={0}>
        <GeneralTabHorizontal
          instanceId={1}
          torrent={makeTorrent({ progress: 1, state: "uploading" })}
          properties={{} as TorrentProperties}
          loading={false}
          speedUnit="bytes"
          downloadLimit={0}
          uploadLimit={0}
          displaySavePath="/downloads"
          tempPathEnabled={false}
          displayInfohashV1="abcdef"
          displayComment={displayComment}
          displayCreatedBy={displayCreatedBy}
        />
      </TooltipProvider>
    </QueryClientProvider>
  )
}


describe("GeneralTabHorizontal comment and creator", () => {
  // regression: #3916
  it("shows Created By without a comment", () => {
    renderGeneralTab(undefined, "Torrent Creator")

    expect(screen.getByText("Torrent Creator")).toBeTruthy()
    expect(screen.queryByText("generalTab.comment")).toBeNull()
  })

  it("shows Comment without Created By", () => {
    renderGeneralTab("Torrent Comment")

    expect(screen.getByText("Torrent Comment")).toBeTruthy()
    expect(screen.queryByText("generalTab.createdBy")).toBeNull()
  })

  it("shows both when both are present", () => {
    renderGeneralTab("Torrent Comment", "Torrent Creator")

    expect(screen.getByText("Torrent Comment")).toBeTruthy()
    expect(screen.getByText("Torrent Creator")).toBeTruthy()
  })

  it("hides both when neither is present", () => {
    renderGeneralTab()

    expect(screen.queryByText("generalTab.comment")).toBeNull()
    expect(screen.queryByText("generalTab.createdBy")).toBeNull()
  })
})
