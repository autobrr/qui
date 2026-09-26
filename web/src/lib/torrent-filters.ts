/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/**
 * Joins the column-filter expression with the sidebar's filter expression so a
 * request targets the visible set (#1925). Either side alone passes through;
 * both present yields `(column) && (expr)`.
 */
export function combineFilterExpr(columnExpr: string | null, expr: string | undefined): string | undefined {
  if (columnExpr && expr) {
    return `(${columnExpr}) && (${expr})`
  }
  return columnExpr || expr || undefined
}

/**
 * A cross-seed filter is an OR of hash equalities produced by useCrossSeedFilter.
 * In that mode the list keeps column filters client-side and every instance is
 * in scope.
 */
export function isCrossSeedExpr(expr: string | undefined): boolean {
  return Boolean(expr?.includes("Hash ==") && expr.includes("||"))
}
