/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { isMovePathServerError, isVisiblyRelativeMovePath } from "./move-path"

describe("isVisiblyRelativeMovePath", () => {
  it.each([
    "/data/archive",
    "  /data/{{ .Category }}  ",
    "\\\\nas\\media\\archive",
    "D:\\Archive",
    "d:/archive",
    "{{ .Category }}/done",
    "",
    "   ",
  ])("accepts %j", (path) => {
    expect(isVisiblyRelativeMovePath(path)).toBe(false)
  })

  it.each([
    "archive",
    "rel/{{ .Category }}",
    "./archive",
    "../archive",
    "C:archive",
    "archive\\sub",
  ])("flags %j", (path) => {
    expect(isVisiblyRelativeMovePath(path)).toBe(true)
  })
})

describe("isMovePathServerError", () => {
  it("matches the server's move path errors", () => {
    expect(isMovePathServerError("Move path must be absolute, for example /data/archive or D:\\Archive. For a sample torrent it renders \"sample/done\"")).toBe(true)
    expect(isMovePathServerError("Invalid move path template: template: movePath:1:9: executing")).toBe(true)
  })

  it("ignores other errors", () => {
    expect(isMovePathServerError("Name is required")).toBe(false)
  })
})
