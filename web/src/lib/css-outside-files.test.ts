/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { findOutsideFile } from "@/lib/css-outside-files"

describe("findOutsideFile", () => {
  it.each([
    ["import", "@import 'https://example.invalid/a.css';", 1, "outside-file"],
    ["import in uppercase", "\n@IMPORT url(x.css);", 2, "outside-file"],
    ["image-set", ":root{}\n.dark{}\nbody { background: image-set('a.png' 1x); }", 3, "outside-file"],
    ["http url", "body {\n  background: url(https://example.invalid/a.png);\n}", 2, "outside-file"],
    ["quoted http url", "body { background: url( \"https://example.invalid/a.png\" ); }", 1, "outside-file"],
    ["protocol-relative url", "body { background: url(//example.invalid/a.png); }", 1, "outside-file"],
    ["url in uppercase", "\n\n\nbody { background: URL(https://example.invalid/a.png); }", 4, "outside-file"],
    ["relative url", "body { background: url(a.png); }", 1, "outside-file"],
    ["escaped url", "body { background: \\75rl(https://example.invalid/a.png); }", 1, "outside-file"],
    ["escaped import", "@\\69mport 'x.css';", 1, "outside-file"],
    ["line break in a string", "body::after { content: \"a\n@import 'x.css'; b\"; }", 1, "unreadable"],
    ["url without an end", "body { background: url(data:x", 1, "unreadable"],
    ["url after a comment", "/* one\n two */ body { background: url(http://example.invalid); }", 2, "outside-file"],
    ["url after a non-ASCII letter", "/* İstanbul */ body { background: url(http://example.invalid); }", 1, "outside-file"],
  ])("refuses %s", (_name, css, line, kind) => {
    expect(findOutsideFile(css)).toEqual({ line, kind })
  })

  it.each([
    ["url in a comment", "/* background: url(https://example.invalid/a.png); @import 'x'; \\75rl() */ :root { --a: b; }"],
    ["data url", "body { background: url(data:image/png;base64,iVBORw0KGgo=); }"],
    ["quoted data url", "body { background: url( 'DATA:image/svg+xml,<svg></svg>' ); }"],
    ["fragment url", "body { filter: url(#blur); }"],
    ["url text in a string", "body::after { content: \"url(https://example.invalid) @import \\\" \"; }"],
    ["backslash in a string", "body::after { content: \"\\2014\"; }"],
    ["font from data", "@font-face { font-family: X; src: url(\"data:font/woff2;base64,AAAA\") format(\"woff2\"); }"],
  ])("allows %s", (_name, css) => {
    expect(findOutsideFile(css)).toBeNull()
  })
})
