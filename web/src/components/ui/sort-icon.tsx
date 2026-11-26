/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-react"

interface SortIconProps {
  sorted: false | "asc" | "desc"
}

export function SortIcon({ sorted }: SortIconProps) {
  if (sorted === "asc") return <ArrowUp className="h-3 w-3" />
  if (sorted === "desc") return <ArrowDown className="h-3 w-3" />
  return <ArrowUpDown className="h-3 w-3 opacity-50" />
}
