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

// i18n passthrough (the inline recheck/reannounce dialogs call t()).
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string, opts?: { defaultValue?: string }) => opts?.defaultValue ?? key }),
}))

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

vi.mock("@/components/torrents/DeleteTorrentDialog", () => ({ DeleteTorrentDialog: stub("DeleteTorrentDialog") }))
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
  TmmConfirmDialog: stub("TmmConfirmDialog"),
}))

afterEach(cleanup)

function makeProps(overrides: Partial<TorrentTableDialogsProps> = {}): TorrentTableDialogsProps {
  const noop = vi.fn()
  const falses = Object.fromEntries(
    [
      "showDeleteDialog", "showCommentDialog", "showTagsDialog", "showCategoryDialog", "showCreateCategoryDialog",
      "showShareLimitDialog", "showSpeedLimitDialog", "showLocationDialog", "showRenameTorrentDialog",
      "showRenameFileDialog", "showRenameFolderDialog", "showRecheckDialog", "showReannounceDialog", "showTmmDialog",
      "showLocationWarningDialog", "pendingTmmEnable", "deleteFiles", "isDeleteFilesLocked", "blockCrossSeeds",
      "deleteCrossSeeds", "isPending", "isAllSelected", "hasCrossSeedTag", "isLoadingTags", "isLoadingCategories",
      "allowSubcategories", "isCrossInstanceEndpoint",
    ].map(k => [k, false])
  )
  return {
    instanceId: 1,
    instanceIds: undefined,
    contextHashes: ["h0"],
    contextTorrents: [],
    closeDeleteDialog: noop,
    setShowCommentDialog: noop, setShowTagsDialog: noop, setShowCategoryDialog: noop, setShowCreateCategoryDialog: noop,
    setShowShareLimitDialog: noop, setShowSpeedLimitDialog: noop, setShowLocationDialog: noop,
    setShowRenameTorrentDialog: noop, setShowRenameFileDialog: noop, setShowRenameFolderDialog: noop,
    setShowRecheckDialog: vi.fn(), setShowReannounceDialog: noop, setShowTmmDialog: noop, setShowLocationWarningDialog: noop,
    setDeleteFiles: noop, toggleDeleteFilesLock: noop, setBlockCrossSeeds: noop, setDeleteCrossSeeds: noop,
    handleDeleteWrapper: vi.fn(), handleSetCommentWrapper: vi.fn(), handleTagsWrapper: vi.fn(),
    handleSetCategoryWrapper: vi.fn(), handleSetShareLimitWrapper: vi.fn(), handleSetSpeedLimitsWrapper: vi.fn(),
    handleSetLocationWrapper: vi.fn(), handleRenameTorrentWrapper: vi.fn(), handleRenameFileWrapper: vi.fn(),
    handleRenameFolderWrapper: vi.fn(), handleRecheckWrapper: vi.fn(), handleReannounceWrapper: vi.fn(),
    handleTmmConfirmWrapper: vi.fn(), proceedToLocationDialog: vi.fn(),
    normalizedSelectionFilters: undefined,
    contextClientMeta: { clientHashes: [], totalSelected: 0, actionTargets: [], excludeTargets: undefined },
    effectiveSelectionCount: 0, deleteDialogTotalSize: 0, deleteDialogFormattedSize: "0 B",
    selectAllExcludeHashes: undefined, selectAllExcludedTargets: [],
    crossSeedWarning: { affectedTorrents: [], reset: vi.fn() } as unknown as TorrentTableDialogsProps["crossSeedWarning"],
    availableTags: [], availableCategories: {}, capabilities: undefined, effectiveSearch: "",
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
  it("renders every dialog closed by default", () => {
    const { getByTestId } = renderDialogs(makeProps())
    expect(getByTestId("DeleteTorrentDialog").getAttribute("data-open")).toBe("false")
    expect(getByTestId("TmmConfirmDialog").getAttribute("data-open")).toBe("false")
  })

  it("opens the delete dialog, fires the wrapper on confirm, and chains close + crossSeedWarning.reset", () => {
    const props = makeProps({ showDeleteDialog: true })
    const { getByTestId } = renderDialogs(props)
    expect(getByTestId("DeleteTorrentDialog").getAttribute("data-open")).toBe("true")

    fireEvent.click(getByTestId("DeleteTorrentDialog-confirm"))
    expect(vi.mocked(props.handleDeleteWrapper)).toHaveBeenCalled()

    fireEvent.click(getByTestId("DeleteTorrentDialog-close"))
    expect(vi.mocked(props.closeDeleteDialog)).toHaveBeenCalled()
    expect(vi.mocked(props.crossSeedWarning.reset)).toHaveBeenCalled()
  })

  it("wires a standard setter dialog (comment) to its wrapper", () => {
    const props = makeProps({ showCommentDialog: true })
    const { getByTestId } = renderDialogs(props)
    expect(getByTestId("SetCommentDialog").getAttribute("data-open")).toBe("true")
    fireEvent.click(getByTestId("SetCommentDialog-confirm"))
    expect(vi.mocked(props.handleSetCommentWrapper)).toHaveBeenCalled()
  })

  it("passes the select-all count (not the context length) when isAllSelected", () => {
    const { getByTestId } = renderDialogs(makeProps({ showTmmDialog: true, isAllSelected: true, effectiveSelectionCount: 5, contextHashes: ["a"] }))
    expect(getByTestId("TmmConfirmDialog").getAttribute("data-count")).toBe("5")
  })

  it("renders the rename-file dialog (the moved useQuery wiring) without throwing", () => {
    const { getByTestId } = renderDialogs(makeProps({ showRenameFileDialog: true }))
    expect(getByTestId("RenameTorrentFileDialog").getAttribute("data-open")).toBe("true")
  })

  it("renders the inline recheck confirm dialog and fires the wrapper", () => {
    const props = makeProps({ showRecheckDialog: true })
    renderDialogs(props)
    // The inline Dialog portals into document.body; its confirm button carries the (passthrough) key text.
    const confirm = Array.from(document.body.querySelectorAll("button")).find(b => b.textContent === "recheckDialog.confirm")
    expect(confirm).toBeTruthy()
    fireEvent.click(confirm!)
    expect(vi.mocked(props.handleRecheckWrapper)).toHaveBeenCalled()
  })
})
