/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { Drawer, DrawerContent, DrawerTitle } from "@/components/ui/drawer"
import { stubPhone } from "@/test/stubPhone"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("DrawerContent", () => {
  it("moves focus into the drawer when it opens", async () => {
    stubPhone("upright")
    render(
      <>
        <button type="button">behind</button>
        <Drawer open>
          <DrawerContent aria-describedby={undefined}>
            <DrawerTitle>Pick</DrawerTitle>
            <input aria-label="search" />
          </DrawerContent>
        </Drawer>
      </>
    )

    const drawer = await screen.findByRole("dialog", { name: "Pick" })
    await waitFor(() => expect(document.activeElement).toBe(drawer))
  })
})
