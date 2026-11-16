/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import "@/i18n"

import { ConfirmDialog } from "./ConfirmDialog"

afterEach(cleanup)

describe("ConfirmDialog", () => {
  it.each([[true, true], [false, false]])("holds the confirm button off while confirmDisabled is %s", (confirmDisabled, disabled) => {
    render(
      <ConfirmDialog open onOpenChange={() => {}} title="Delete seedbox?" description="It goes away." confirmLabel="Delete" onConfirm={() => {}} confirmDisabled={confirmDisabled} />
    )

    expect((screen.getByRole("button", { name: "Delete" }) as HTMLButtonElement).disabled).toBe(disabled)
    expect((screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement).disabled).toBe(false)
  })
})
