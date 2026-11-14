/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { readThemeName, themeIdFromName, writeThemeName } from "@/lib/custom-theme-editor"

describe("themeIdFromName", () => {
  it.each([
    ["Ocean", "ocean"],
    ["Deep Ocean Blue", "deep-ocean-blue"],
    ["  My   theme!! v2 ", "my-theme-v2"],
    ["Nord_Like", "nord-like"],
    ["--Retro--", "retro"],
    ["Ørken sol", "rken-sol"],
    ["!!!", ""],
    ["a".repeat(70), "a".repeat(64)],
    [`${"a".repeat(63)} b`, "a".repeat(63)],
  ])("%s -> %s", (name, id) => {
    expect(themeIdFromName(name)).toBe(id)
  })
})

describe("readThemeName", () => {
  it("reads the @name line", () => {
    expect(readThemeName("/* @name: Ocean\n * @description: Blue */\n:root{}")).toBe("Ocean")
    expect(readThemeName("/* @name: Sunset */")).toBe("Sunset")
  })

  it("returns null without a @name line", () => {
    expect(readThemeName(":root { --a: b; }")).toBeNull()
  })
})

describe("writeThemeName", () => {
  it("replaces the name of an existing @name line", () => {
    const css = "/* @name: Ocean\n * @description: Blue\n */\n:root { --a: b; }"
    expect(writeThemeName(css, "Deep Sea")).toBe("/* @name: Deep Sea\n * @description: Blue\n */\n:root { --a: b; }")
  })

  it("replaces a name on a one-line header", () => {
    expect(writeThemeName("/* @name: Ocean */\n.dark{}", "Sea")).toBe("/* @name: Sea */\n.dark{}")
  })

  it("adds a header when the css has no @name line", () => {
    expect(writeThemeName(":root { --a: b; }", "Ocean")).toBe("/* @name: Ocean */\n:root { --a: b; }")
  })

  it("keeps the name from closing the comment", () => {
    const css = writeThemeName(":root{}", "a */ body { display: none } /*")
    expect(css.indexOf("*/")).toBe(css.lastIndexOf("*/"))
    expect(readThemeName(css)).toBe("a body { display: none }")
  })

  it("reads back what it wrote", () => {
    expect(readThemeName(writeThemeName("/* @name: Old */", "New name"))).toBe("New name")
  })
})
