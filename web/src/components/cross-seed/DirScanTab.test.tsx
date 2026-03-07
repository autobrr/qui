/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { fireEvent, render, screen } from "@testing-library/react"
import { describe, beforeEach, expect, it, vi } from "vitest"

import { DirScanTab } from "@/components/cross-seed/DirScanTab"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { DirScanDirectory, DirScanRun, DirScanSettings, Instance } from "@/types"

const {
  toastSuccess,
  toastError,
  cancelMutate,
  resetMutate,
} = vi.hoisted(() => ({
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  cancelMutate: vi.fn((_: void, options?: { onSuccess?: () => void }) => {
    options?.onSuccess?.()
  }),
  resetMutate: vi.fn((_: void, options?: { onSuccess?: () => void }) => {
    options?.onSuccess?.()
  }),
}))

vi.mock("sonner", () => ({
  toast: {
    success: toastSuccess,
    error: toastError,
  },
}))

vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => ({
    formatISOTimestamp: (value: string) => value,
  }),
}))

vi.mock("@/hooks/useInstanceMetadata", () => ({
  useInstanceMetadata: () => ({
    data: {
      categories: {},
      tags: [],
    },
    isError: false,
  }),
}))

vi.mock("@/lib/dateTimeUtils", () => ({
  formatRelativeTime: () => "just now",
}))

vi.mock("@/lib/category-utils", () => ({
  buildCategorySelectOptions: () => [],
  buildTagSelectOptions: () => [],
}))

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>("@tanstack/react-query")
  return {
    ...actual,
    useQueries: vi.fn(() => []),
  }
})

vi.mock("@/hooks/useDirScan", () => ({
  isRunActive: (run: DirScanRun) => ["queued", "scanning", "searching", "injecting"].includes(run.status),
  useCancelDirScan: () => ({
    mutate: cancelMutate,
    isPending: false,
  }),
  useCreateDirScanDirectory: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
  useDeleteDirScanDirectory: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
  useDirScanDirectories: () => ({
    data: [
      {
        id: 1,
        path: "/data/media",
        qbitPathPrefix: "",
        category: "",
        tags: [],
        enabled: true,
        targetInstanceId: 1,
        scanIntervalMinutes: 60,
        createdAt: "2026-03-07T20:00:00Z",
        updatedAt: "2026-03-07T20:00:00Z",
      } satisfies Partial<DirScanDirectory>,
    ] as DirScanDirectory[],
    isLoading: false,
  }),
  useDirScanRunInjections: () => ({
    data: [],
    isLoading: false,
  }),
  useDirScanRuns: () => ({
    data: [],
    isLoading: false,
  }),
  useDirScanSettings: () => ({
    data: {
      enabled: true,
      matchMode: "strict",
      sizeTolerancePercent: 2,
      minPieceRatio: 98,
      maxSearcheesPerRun: 0,
      maxSearcheeAgeDays: 0,
      allowPartial: false,
      skipPieceBoundarySafetyCheck: true,
      startPaused: false,
      category: "",
      tags: [],
    } satisfies Partial<DirScanSettings>,
    isLoading: false,
  }),
  useDirScanStatus: () => ({
    data: {
      id: 10,
      directoryId: 1,
      status: "scanning",
      triggeredBy: "manual",
      filesFound: 1,
      filesSkipped: 0,
      matchesFound: 0,
      torrentsAdded: 0,
      startedAt: "2026-03-07T20:00:00Z",
    } satisfies Partial<DirScanRun>,
  }),
  useResetDirScanFiles: () => ({
    mutate: resetMutate,
    isPending: false,
  }),
  useTriggerDirScan: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
  useUpdateDirScanDirectory: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
  useUpdateDirScanSettings: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}))

const instances = [
  {
    id: 1,
    name: "Main",
    host: "http://localhost:8080",
    username: "user",
    tlsSkipVerify: false,
    hasLocalFilesystemAccess: true,
    useHardlinks: false,
    hardlinkBaseDir: "",
    hardlinkDirPreset: "flat",
    useReflinks: false,
    fallbackToRegularMode: false,
    sortOrder: 0,
    isActive: true,
    reannounceSettings: {},
  } as Instance,
]

function renderDirScanTab() {
  return render(
    <TooltipProvider>
      <DirScanTab instances={instances} />
    </TooltipProvider>
  )
}

describe("DirScanTab", () => {
  beforeEach(() => {
    toastSuccess.mockReset()
    toastError.mockReset()
    cancelMutate.mockClear()
    resetMutate.mockClear()
  })

  it("shows the retained-runs copy and reset warning", () => {
    renderDirScanTab()

    expect(screen.queryByText("Last 10 runs retained for this directory.")).toBeNull()

    fireEvent.click(screen.getByText("/data/media"))

    expect(screen.getByText("Last 10 runs retained for this directory.")).toBeTruthy()

    fireEvent.click(screen.getByRole("button", { name: "Reset Scan Progress" }))

    expect(
      screen.getByText(
        "This deletes tracked dir-scan progress for this directory. The next scan will recheck the directory and retry all items, including ones that were already finished."
      )
    ).toBeTruthy()
  })

  it("shows the incremental progress help text in settings", () => {
    renderDirScanTab()

    expect(
      screen.queryByText(
        "0 = unlimited. Useful for incremental progress: the next run rechecks the directory, skips finished items, and retries unfinished ones."
      )
    ).toBeNull()

    fireEvent.click(screen.getByRole("button", { name: "Settings" }))

    expect(
      screen.getByText(
        "0 = unlimited. Useful for incremental progress: the next run rechecks the directory, skips finished items, and retries unfinished ones."
      )
    ).toBeTruthy()
  })

  it("shows the updated cancel success toast", () => {
    renderDirScanTab()

    const pauseButton = screen.getAllByRole("button").find((button) =>
      button.querySelector("svg.lucide-pause")
    )

    expect(pauseButton).toBeTruthy()

    fireEvent.click(pauseButton!)

    expect(cancelMutate).toHaveBeenCalled()
    expect(toastSuccess).toHaveBeenCalledWith(
      "Scan canceled. Next run will recheck the directory and continue with unfinished items."
    )
  })
})
