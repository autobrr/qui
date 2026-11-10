/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TorrentTableDialogs, type TorrentTableDialogsProps } from "@/components/torrents/table/TorrentTableDialogs"
import { TooltipProvider } from "@/components/ui/tooltip"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

// Network boundary for the moved rename useQuery.
vi.mock("@/lib/api", () => ({ api: new Proxy({}, { get: () => vi.fn(() => Promise.resolve(undefined)) }) }))

// Prop-capturing stubs for the dialog components — assert the EXTRACTION wiring,
// not each dialog's internals.
type StubProps = { open?: boolean; onConfirm?: (...a: unknown[]) => void; onOpenChange?: (open: boolean) => void; count?: number; hashCount?: number }
function stub(name: string) {
  return ({ open, onConfirm, onOpenChange, count, hashCount }: StubProps) => (
    <div data-testid={name} data-open={String(Boolean(open))} data-count={String(count ?? hashCount ?? "")}>
      <button data-testid={`${name}-confirm`} onClick={() => onConfirm?.()}>confirm</button>
      <button data-testid={`${name}-close`} onClick={() => onOpenChange?.(false)}>close</button>
    </div>
  )
}

vi.mock("@/components/torrents/TorrentDialogs", () => ({
  CreateAndAssignCategoryDialog: stub("CreateAndAssignCategoryDialog"),
  LocationWarningDialog: stub("LocationWarningDialog"),
  RenameTorrentDialog: stub("RenameTorrentDialog"),
  RenameTorrentFileDialog: stub("RenameTorrentFileDialog"),
  RenameTorrentFolderDialog: stub("RenameTorrentFolderDialog"),
  SetCategoryDialog: stub("SetCategoryDialog"),
  SetLocationDialog: stub("SetLocationDialog"),
  TagEditorDialog: stub("TagEditorDialog"),
  SetCommentDialog: stub("SetCommentDialog"),
  ShareLimitDialog: stub("ShareLimitDialog"),
  SpeedLimitsDialog: stub("SpeedLimitsDialog"),
}))

afterEach(cleanup)

function makeProps(overrides: Partial<TorrentTableDialogsProps> = {}): TorrentTableDialogsProps {
  const noop = vi.fn()
  const falses = Object.fromEntries(
    [
      "showCommentDialog", "showTagsDialog", "showCategoryDialog", "showCreateCategoryDialog",
      "showShareLimitDialog", "showSpeedLimitDialog", "showLocationDialog", "showRenameTorrentDialog",
      "showRenameFileDialog", "showRenameFolderDialog",
      "showLocationWarningDialog", "isPending", "isLoadingTags", "isLoadingCategories",
      "allowSubcategories",
    ].map(k => [k, false])
  )
  return {
    instanceId: 1,
    actionTarget: { kind: "ticks", hashes: ["h0"], count: 1, previewTorrents: [] },
    renameHash: "h0",
    setShowCommentDialog: noop, setShowTagsDialog: noop, setShowCategoryDialog: noop, setShowCreateCategoryDialog: noop,
    setShowShareLimitDialog: noop, setShowSpeedLimitDialog: noop, setShowLocationDialog: noop,
    setShowRenameTorrentDialog: noop, setShowRenameFileDialog: noop, setShowRenameFolderDialog: noop,
    setShowLocationWarningDialog: noop,
    handleSetCommentWrapper: vi.fn(), handleTagsWrapper: vi.fn(),
    handleSetCategoryWrapper: vi.fn(), handleSetShareLimitWrapper: vi.fn(), handleSetSpeedLimitsWrapper: vi.fn(),
    handleSetLocationWrapper: vi.fn(), handleRenameTorrentWrapper: vi.fn(), handleRenameFileWrapper: vi.fn(),
    handleRenameFolderWrapper: vi.fn(), proceedToLocationDialog: vi.fn(),
    availableTags: [], availableCategories: {}, capabilities: undefined,
    ...(falses as Record<string, boolean>),
    ...overrides,
  } as TorrentTableDialogsProps
}

function renderDialogs(props: TorrentTableDialogsProps) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}><TooltipProvider>{children}</TooltipProvider></QueryClientProvider>
  )
  return render(<TorrentTableDialogs {...props} />, { wrapper })
}

describe("TorrentTableDialogs", () => {
  it("wires a standard setter dialog (comment) to its wrapper", () => {
    const props = makeProps({ showCommentDialog: true })
    const { getByTestId } = renderDialogs(props)
    expect(getByTestId("SetCommentDialog").getAttribute("data-open")).toBe("true")
    fireEvent.click(getByTestId("SetCommentDialog-confirm"))
    expect(vi.mocked(props.handleSetCommentWrapper)).toHaveBeenCalled()
  })

  it("passes the select-all count (not the loaded length) in Select all mode", () => {
    const actionTarget: TorrentTableDialogsProps["actionTarget"] = { kind: "selectAll", selectAll: { filters: {} as never, search: "" }, count: 5, previewTorrents: [] }
    const { getByTestId } = renderDialogs(makeProps({ showLocationWarningDialog: true, actionTarget }))
    expect(getByTestId("LocationWarningDialog").getAttribute("data-count")).toBe("5")
  })

  it("renders the rename-file dialog (the moved useQuery wiring) without throwing", () => {
    const { getByTestId } = renderDialogs(makeProps({ showRenameFileDialog: true }))
    expect(getByTestId("RenameTorrentFileDialog").getAttribute("data-open")).toBe("true")
  })
})
