/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TokenType, tokenize, type CSSToken } from "@csstools/css-tokenizer"

/**
 * Where css can make the browser load a file from another host, or where the
 * check cannot decide. The same rule as CheckNoOutsideFiles in
 * internal/themes/outside_files.go, which refuses the save; the editor uses it
 * so that a preview never loads such a file.
 */
export interface OutsideFile {
  line: number
  kind: "outside-file" | "unreadable"
}

const CSS_SPACE = /^[ \t\n\r\f]+|[ \t\n\r\f]+$/g

function isLocalTarget(target: string): boolean {
  const t = target.replace(CSS_SPACE, "").toLowerCase()
  return t.startsWith("data:") || t.startsWith("#")
}

// Escapes stay unresolved, as on the server, so an escape in the data: prefix is refused.
function rawStringValue(token: CSSToken): string {
  const raw = token[1]
  return raw.length > 1 && raw.endsWith(raw[0]) ? raw.slice(1, -1) : raw.slice(1)
}

export function findOutsideFile(css: string): OutsideFile | null {
  const tokens = tokenize({ css })
  let line = 1
  for (let i = 0; i < tokens.length; i++) {
    const token = tokens[i]
    const [type, raw] = token
    const at = (kind: OutsideFile["kind"]): OutsideFile => ({ line, kind })

    switch (token[0]) {
      case TokenType.Comment:
      case TokenType.String:
      case TokenType.EOF:
        break
      case TokenType.BadString:
      case TokenType.BadURL:
        return at("unreadable")
      case TokenType.URL:
        if (raw.includes("\\") || !isLocalTarget(token[4].value)) return at("outside-file")
        break
      case TokenType.AtKeyword:
        if (raw.includes("\\") || token[4].value.toLowerCase() === "import") return at("outside-file")
        break
      case TokenType.Function: {
        if (raw.includes("\\")) return at("outside-file")
        const name = token[4].value.toLowerCase()
        if (name.endsWith("image-set")) return at("outside-file")
        if (name !== "url") break
        let next = i + 1
        while (tokens[next]?.[0] === TokenType.Whitespace || tokens[next]?.[0] === TokenType.Comment) next++
        const target = tokens[next]
        if (target?.[0] !== TokenType.String || !isLocalTarget(rawStringValue(target))) return at("outside-file")
        break
      }
      default:
        if (type !== TokenType.Whitespace && raw.includes("\\")) return at("outside-file")
    }
    for (const c of raw) if (c === "\n") line++
  }
  return null
}
