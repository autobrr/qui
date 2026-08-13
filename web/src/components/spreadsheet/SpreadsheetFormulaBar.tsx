/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTorrentSelection } from "@/contexts/TorrentSelectionContext"
import { navigateWithSearch } from "@/lib/router-search"
import { useNavigate, useSearch } from "@tanstack/react-router"
import { useEffect, useState } from "react"

// Disguise vocabulary, not UI copy: deliberately English and not translated.
const FX_MARK = "fx"
const EMPTY_CELL_REF = "A1"
const FORMULA_ARIA = "Filter"

// Formula bar: the name box mirrors the selection, the fx input drives the
// same `q` route param as the header search, so it is a real second entry
// point for filtering, not decoration.
export function SpreadsheetFormulaBar() {
  const navigate = useNavigate()
  const routeSearch = useSearch({ strict: false }) as { q?: string;[key: string]: unknown }
  const [value, setValue] = useState(routeSearch?.q ?? "")
  const { totalSelectionCount } = useTorrentSelection()

  useEffect(() => {
    setValue(routeSearch?.q ?? "")
  }, [routeSearch?.q])

  const nameBox = totalSelectionCount > 0 ? `${totalSelectionCount}R` : EMPTY_CELL_REF

  return (
    <div className="ss-formula-bar hidden md:flex">
      <div className="ss-name-box">{nameBox}</div>
      <div className="ss-fx" aria-hidden="true">{FX_MARK}</div>
      <input
        className="ss-formula-input"
        type="text"
        value={value}
        spellCheck={false}
        aria-label={FORMULA_ARIA}
        onChange={(event) => setValue(event.target.value)}
        onKeyDown={(event) => {
          if (event.key !== "Enter") return
          const next = { ...(routeSearch || {}) }
          const trimmed = value.trim()
          if (trimmed) next.q = trimmed
          else delete next.q
          navigateWithSearch({ navigate, search: next, replace: true })
        }}
      />
    </div>
  )
}
