/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { Torrent, TorrentProperties } from "@/types"
import { getLinuxCategory, getLinuxTags } from "@/lib/incognito"
import { makeTorrent } from "@/test/mockTorrent"
import { TooltipProvider } from "@/components/ui/tooltip"
import { GeneralTabHorizontal } from "./GeneralTabHorizontal"
import "@/i18n"


vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))

const incognito = vi.hoisted(() => ({ on: false }))

vi.mock("@/lib/incognito", async (importOriginal) => {
  const off: [boolean, (value: boolean) => void] = [false, () => {}]
  const on: [boolean, (value: boolean) => void] = [true, () => {}]
  return { ...await importOriginal<typeof import("@/lib/incognito")>(), useIncognitoMode: () => incognito.on ? on : off }
})

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


afterEach(() => {
  cleanup()
  incognito.on = false
})

function renderGeneralTab(displayComment?: string, displayCreatedBy?: string, torrent: Partial<Torrent> = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider delayDuration={0}>
        <GeneralTabHorizontal
          instanceId={1}
          torrent={makeTorrent({ progress: 1, state: "uploading", ...torrent })}
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

describe("GeneralTabHorizontal category and tags", () => {
  const hash = "9f8e7d6c5b4a3210"

  it("shows the category and each tag", () => {
    renderGeneralTab(undefined, undefined, { hash, category: "tv-shows", tags: "seed, keep" })

    expect(screen.getByText("tv-shows")).toBeTruthy()
    expect(screen.getByText("seed")).toBeTruthy()
    expect(screen.getByText("keep")).toBeTruthy()
  })

  it("shows the fake category and tags in incognito mode", () => {
    incognito.on = true
    renderGeneralTab(undefined, undefined, { hash, category: "tv-shows", tags: "seed, keep" })

    expect(screen.queryByText("tv-shows")).toBeNull()
    expect(screen.queryByText("seed")).toBeNull()
    expect(screen.getByText(getLinuxCategory(hash))).toBeTruthy()
    for (const tag of getLinuxTags(hash).split(", ")) {
      expect(screen.getByText(tag)).toBeTruthy()
    }
  })

  it("shows no badges without a category or tags", () => {
    const { container } = renderGeneralTab(undefined, undefined, { hash, category: "", tags: "" })

    expect(container.querySelector("[data-slot=badge]")).toBeNull()
  })
})
