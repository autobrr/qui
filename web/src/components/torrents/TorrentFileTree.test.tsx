/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { TorrentFile } from "@/types"
import { TorrentFileSortBar, TorrentFileTree } from "./TorrentFileTree"
import "@/i18n"

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))

afterEach(cleanup)

describe("TorrentFileSortBar", () => {
  it("renders one button per column and toggles direction on the active one", () => {
    const onSortChange = vi.fn()
    const { getAllByRole, getByText } = render(
      <TorrentFileSortBar sort={{ column: "size", direction: "asc" }} supportsFilePriority onSortChange={onSortChange} />
    )
    expect(getAllByRole("button").map(b => b.textContent)).toEqual([
      "fileTable.headers.name",
      "fileTable.headers.progress",
      "fileTable.headers.size",
      "filePriority.header",
    ])

    const sizeButton = getByText("fileTable.headers.size").closest("button")!
    expect(sizeButton.getAttribute("aria-pressed")).toBe("true")
    expect(sizeButton.getAttribute("aria-label")).toBe("fileTable.headers.size, sort.ascending")
    expect(getByText("fileTable.headers.name").closest("button")!.getAttribute("aria-pressed")).toBe("false")

    fireEvent.click(getByText("fileTable.headers.size"))
    expect(onSortChange).toHaveBeenLastCalledWith({ column: "size", direction: "desc" })

    fireEvent.click(getByText("fileTable.headers.name"))
    expect(onSortChange).toHaveBeenLastCalledWith({ column: "name", direction: "asc" })
  })

  it("hides the priority button when the instance lacks per-file priority", () => {
    const { queryByText } = render(
      <TorrentFileSortBar sort={{ column: "name", direction: "asc" }} supportsFilePriority={false} onSortChange={() => {}} />
    )
    expect(queryByText("filePriority.header")).toBeNull()
  })
})

describe("TorrentFileTree", () => {
  // regression: #4358
  it("shows the label of a disabled rename button on a tap in incognito mode, and the folder stays open", () => {
    const touchWindow = window as Window & { ontouchstart?: unknown }
    touchWindow.ontouchstart = null
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(500)
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(500)
    try {
      const files = [{ index: 0, name: "Cedar.Harbor/Cedar.Harbor.mkv", size: 1024, progress: 0.5, priority: 1 }] as TorrentFile[]
      const onRenameFile = vi.fn()
      const onRenameFolder = vi.fn()
      const { getByRole, getAllByRole } = render(
        <TooltipProvider>
          <TorrentFileTree
            files={files}
            sort={{ column: "name", direction: "asc" }}
            supportsFilePriority={false}
            pendingFileIndices={new Set()}
            incognitoMode
            torrentHash="abc"
            onToggleFile={() => {}}
            onToggleFileRange={() => {}}
            onToggleFolder={() => {}}
            onSetFilePriority={() => {}}
            onSetFolderPriority={() => {}}
            onRenameFile={onRenameFile}
            onRenameFolder={onRenameFolder}
          />
        </TooltipProvider>
      )

      for (const label of ["fileTree.renameFolder", "fileTree.renameFile"]) {
        const wrapper = getByRole("button", { name: label }).parentElement!
        fireEvent.pointerDown(wrapper, { pointerType: "touch" })
        fireEvent.click(wrapper)

        expect(getByRole("tooltip").textContent).toContain(label)
        fireEvent.click(wrapper)
      }
      expect(getAllByRole("button", { name: "fileTree.renameFile" })).toHaveLength(1)
      expect(onRenameFile).not.toHaveBeenCalled()
      expect(onRenameFolder).not.toHaveBeenCalled()
    } finally {
      vi.unstubAllGlobals()
      vi.restoreAllMocks()
      delete touchWindow.ontouchstart
    }
  })
})
