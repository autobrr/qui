/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { TorrentFileSortBar } from "./TorrentFileTree"

vi.mock("react-i18next", () => ({
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
