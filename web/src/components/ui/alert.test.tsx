/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Alert } from "@/components/ui/alert"
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

describe("Alert status variants", () => {
  it.each(["success", "warning", "info"] as const)("draws the border, tint, icon, and text of %s in its status color", (status) => {
    render(<Alert variant={status}>Status</Alert>)

    const classes = [...screen.getByRole("alert").classList]
    expect(classes).toEqual(expect.arrayContaining([`border-${status}/50`, `bg-${status}/10`, `text-${status}`, `[&>svg]:text-${status}`]))
    expect(classes.filter((name) => /(yellow|green|blue|foreground)/.test(name))).toEqual([])
  })
})
