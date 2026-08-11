/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { navigateWithSearch } from "@/lib/router-search"
import { describe, expect, it, vi } from "vitest"

describe("navigateWithSearch", () => {
  it("forwards to, search, and replace to navigate", () => {
    const navigate = vi.fn()
    const search = { q: "foo", page: 2 }

    navigateWithSearch({ navigate, to: "/torrents", search, replace: true })

    expect(navigate).toHaveBeenCalledTimes(1)
    expect(navigate).toHaveBeenCalledWith({ to: "/torrents", search, replace: true })
  })

  it("omits to and replace when not provided", () => {
    const navigate = vi.fn()
    const search = { tab: "files" }

    navigateWithSearch({ navigate, search })

    expect(navigate).toHaveBeenCalledTimes(1)
    const arg = navigate.mock.calls[0][0]
    expect(arg).toEqual({ search })
    expect("to" in arg).toBe(false)
    expect("replace" in arg).toBe(false)
  })

  it("passes the search object through unchanged", () => {
    const navigate = vi.fn()
    const search = { a: 1, b: undefined, c: "x" }

    navigateWithSearch({ navigate, to: "/instances", search })

    expect(navigate.mock.calls[0][0].search).toBe(search)
  })
})
