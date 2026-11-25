/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { AlertDialog, AlertDialogAction } from "@/components/ui/alert-dialog"
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

describe("AlertDialogAction variant", () => {
  it("takes the destructive colors in place of the primary ones", () => {
    render(<AlertDialog open><AlertDialogAction variant="destructive">Delete</AlertDialogAction></AlertDialog>)

    const classes = [...screen.getByText("Delete").classList]
    expect(classes).toEqual(expect.arrayContaining(["bg-destructive", "text-destructive-foreground", "hover:bg-destructive/90"]))
    expect(classes).not.toContain("bg-primary")
    expect(classes).not.toContain("hover:bg-primary/90")
  })
})
