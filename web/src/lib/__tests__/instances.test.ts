/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { instanceInitials } from "../instances"

describe("instanceInitials", () => {
  it.each([
    { names: ["main", "secondary", "seedbox", "test"], want: ["MA", "SE", "SD", "TE"] },
    { names: ["qbit1", "qbit2", "qbit10", "qbit11"], want: ["Q1", "Q2", "10", "11"] },
    { names: ["Stub 1", "Stub 02", "home-nas"], want: ["S1", "S2", "HN"] },
    { names: ["qbit-us-1", "qbit-us-2", "qbit-eu-1"], want: ["Q1", "Q2", "QE"] },
    { names: ["qbit-movies", "qbit-tv", "qbit-music"], want: ["QM", "QT", "QU"] },
    { names: ["Home (NAS)", "Seedbox [EU]", "qbit/tv"], want: ["HN", "SE", "QT"] },
    { names: ["A4", "A2", "A", "a", "aa", "aa"], want: ["A4", "A2", "A", "A1", "AA", "A3"] },
    { names: ["x", "", "下载机", "🙂"], want: ["X", "", "下载", ""] },
  ])("$names", ({ names, want }) => {
    expect(instanceInitials(names)).toEqual(want)
  })
})
