/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { isRedirect } from "@tanstack/react-router"
import { describe, expect, it } from "vitest"

import { Route } from "./__root"

function runBeforeLoad(pathname: string, search: Record<string, unknown> = {}) {
  try {
    Route.options.beforeLoad?.({ location: { pathname, search } } as never)
  } catch (thrown) {
    return thrown
  }
  return undefined
}

describe("root beforeLoad", () => {
  it("redirects /index.html to the SPA root and keeps the search", () => {
    const thrown = runBeforeLoad("/qui/index.html", { returnTo: "/x", tab: "peers" })

    expect(isRedirect(thrown)).toBe(true)
    if (isRedirect(thrown)) {
      expect(thrown.options.to).toBe("/")
      expect(thrown.options.search).toEqual({ returnTo: "/x", tab: "peers" })
    }
  })

  it("leaves normal routes alone", () => {
    expect(runBeforeLoad("/qui/instances/1")).toBeUndefined()
    expect(runBeforeLoad("/")).toBeUndefined()
    expect(runBeforeLoad("/qui/")).toBeUndefined()
  })
})
