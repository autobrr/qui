/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export function combineFilterExpr(columnExpr: string | null, expr: string | undefined): string | undefined {
  if (columnExpr && expr) {
    return `(${columnExpr}) && (${expr})`
  }
  return columnExpr || expr || undefined
}

export function isCrossSeedExpr(expr: string | undefined): boolean {
  return Boolean(expr?.includes("Hash ==") && expr.includes("||"))
}
