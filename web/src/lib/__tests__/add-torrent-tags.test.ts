/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { parseTypedTags } from "../add-torrent-tags"

describe("parseTypedTags", () => {
  it.each([
    ["", []],
    ["fresh", ["fresh"]],
    ["radarr,", ["radarr"]],
    ["fresh, fresh ,fresh", ["fresh"]],
    ["Keep,keep", ["Keep", "keep"]],
    ["autobrr,new tag", ["autobrr", "new tag"]],
  ])("reads %j as %j", (text, expected) => {
    expect(parseTypedTags(text)).toEqual(expected)
  })
})
