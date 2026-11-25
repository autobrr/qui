/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Label } from "@/components/ui/label"
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

describe("Label size", () => {
  it("sets 12 px text without the tight line height", () => {
    render(<Label size="sm" className="text-muted-foreground">Tag</Label>)

    const classes = [...screen.getByText("Tag").classList]
    expect(classes).toEqual(expect.arrayContaining(["text-xs", "font-medium", "text-muted-foreground"]))
    expect(classes).not.toContain("leading-none")
    expect(classes).not.toContain("text-sm")
  })
})
