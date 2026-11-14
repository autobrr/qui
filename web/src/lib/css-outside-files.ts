/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/**
 * Where css can make the browser load a file from another host, or where the
 * scan cannot decide. The same rule as CheckNoOutsideFiles in
 * internal/themes/outside_files.go, which refuses the save; the editor uses it
 * so that a preview never loads such a file.
 */
export interface OutsideFile {
  line: number
  kind: "outside-file" | "unreadable"
}

const SPACE = /[ \t\n\r\f]/

export function findOutsideFile(css: string): OutsideFile | null {
  let pos = 0
  let line = 1
  const at = (word: string): boolean => {
    for (let i = 0; i < word.length; i++) {
      let code = css.charCodeAt(pos + i)
      if (code >= 65 && code <= 90) code += 32
      if (code !== word.charCodeAt(i)) return false
    }
    return true
  }

  const advance = (): string => {
    const c = css[pos++]
    if (c === "\n") line++
    return c
  }

  const readString = (): { value: string } | OutsideFile => {
    const start = line
    const quote = advance()
    let value = ""
    while (pos < css.length) {
      const c = advance()
      if (c === quote) return { value }
      if (c === "\n" || c === "\r" || c === "\f") return { line: start, kind: "unreadable" }
      value += c
      if (c === "\\" && pos < css.length) value += advance()
    }
    return { line: start, kind: "unreadable" }
  }

  while (pos < css.length) {
    const c = css[pos]
    if (at("/*")) {
      pos += 2
      while (pos < css.length && !at("*/")) advance()
      pos = Math.min(pos + 2, css.length)
    } else if (c === "\"" || c === "'") {
      const result = readString()
      if ("kind" in result) return result
    } else if (c === "\\" || at("@import") || at("image-set(")) {
      return { line, kind: "outside-file" }
    } else if (at("url(")) {
      const urlLine = line
      pos += 4
      while (pos < css.length && SPACE.test(css[pos])) advance()
      let target: string
      if (css[pos] === "\"" || css[pos] === "'") {
        const result = readString()
        if ("kind" in result) return result
        target = result.value
      } else {
        const end = css.indexOf(")", pos)
        if (end < 0) return { line: urlLine, kind: "unreadable" }
        target = css.slice(pos, end)
        if (/["'\\(]/.test(target)) return { line: urlLine, kind: "unreadable" }
        while (pos < end) advance()
      }
      const t = target.trim().toLowerCase()
      if (!t.startsWith("data:") && !t.startsWith("#")) return { line: urlLine, kind: "outside-file" }
    } else {
      advance()
    }
  }
  return null
}
