/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { DashboardTotalSize } from "@/components/dashboard/UniqueSize"
import { TooltipProvider } from "@/components/ui/tooltip"
import { formatBytes } from "@/lib/utils"
import type { TorrentStats } from "@/types"

import "@/i18n"

const i18n = vi.hoisted(() => ({ t: (key: string) => key }))
vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => i18n,
}))

afterEach(cleanup)

const GiB = 1024 ** 3

function renderSize(stats: TorrentStats) {
  return render(
    <TooltipProvider>
      <DashboardTotalSize stats={stats} />
    </TooltipProvider>
  )
}

describe("DashboardTotalSize", () => {
  const stats: TorrentStats = { total: 2, downloading: 0, seeding: 2, paused: 0, error: 0, totalSize: 20 * GiB, totalSeedingSize: 20 * GiB }

  it("shows the old size with a calculating mark until the first Copy groups build ends", () => {
    const { container } = renderSize(stats)

    expect(container.textContent).toContain(formatBytes(20 * GiB))
    expect(container.textContent).toContain("uniqueSize.calculating")
  })

  it("shows the Unique size without the mark after the build", () => {
    const { container } = renderSize({ ...stats, uniqueSize: 10 * GiB, uniqueSeedingSize: 10 * GiB })

    expect(container.textContent).toContain(formatBytes(10 * GiB))
    expect(container.textContent).not.toContain(formatBytes(20 * GiB))
    expect(container.textContent).not.toContain("uniqueSize.calculating")
  })
})
