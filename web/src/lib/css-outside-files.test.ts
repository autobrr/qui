/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { findOutsideFile, type OutsideFile } from "@/lib/css-outside-files"

// The server check in internal/themes reads the same cases.
import cases from "../../../internal/themes/testdata/outside_files.json"

describe("findOutsideFile", () => {
  it.each(cases.refused)("refuses $name", ({ css, line, kind }) => {
    expect(findOutsideFile(css)).toEqual({ line, kind: kind as OutsideFile["kind"] })
  })

  it.each(cases.allowed)("allows $name", ({ css }) => {
    expect(findOutsideFile(css)).toBeNull()
  })
})
