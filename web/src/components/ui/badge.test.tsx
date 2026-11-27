/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

describe("Badge size", () => {
  it("sets 10 px text and keeps the padding of a plain badge", () => {
    render(<Badge size="sm" variant="outline" className="text-muted-foreground">Beta</Badge>)

    const classes = [...screen.getByText("Beta").classList]
    expect(classes).toEqual(expect.arrayContaining(["text-10", "px-2", "py-0.5", "text-muted-foreground"]))
    expect(classes).not.toContain("text-xs")
  })
})

describe("Badge status variants", () => {
  it.each(["success", "warning", "info"] as const)("tints %s with its status color only", (status) => {
    render(<Badge variant={status}>Status</Badge>)

    const classes = [...screen.getByText("Status").classList]
    expect(classes).toEqual(expect.arrayContaining([`border-${status}/20`, `bg-${status}/10`, `text-${status}`]))
    expect(classes.filter((name) => /^(bg|text|border)-(primary|secondary|foreground)/.test(name))).toEqual([])
  })
})
