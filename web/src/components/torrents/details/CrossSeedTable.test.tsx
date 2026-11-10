/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock("@/hooks/useTrackerCustomizations", () => ({ useTrackerCustomizations: () => ({ data: [] }) }))
vi.mock("@/hooks/useTrackerIcons", () => ({ useTrackerIcons: () => ({ data: {} }) }))

import { CrossSeedTable } from "./CrossSeedTable"

describe("CrossSeedTable", () => {
  it("shows a failed lookup rather than no matches", () => {
    render(
      <CrossSeedTable
        matches={[]}
        loading={false}
        loadFailed
        incognitoMode={false}
        selectedTorrents={new Set()}
        onToggleSelection={vi.fn()}
        onSelectAll={vi.fn()}
        onDeselectAll={vi.fn()}
        onDeleteMatches={vi.fn()}
        onDeleteCurrent={vi.fn()}
        instanceById={new Map()}
      />
    )

    expect(screen.getByText("crossSeedTable.loadFailed")).toBeTruthy()
    expect(screen.queryByText("crossSeedTable.noMatches")).toBeNull()
  })
})
