/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import "@/i18n"

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { InstanceResponse } from "@/types"

import { InstanceErrorDisplay } from "./InstanceErrorDisplay"

afterEach(cleanup)

describe("InstanceErrorDisplay", () => {
  // regression: #4233
  it("names each error category with a label, and an unknown category as Unknown", () => {
    const recentErrors = [
      { id: 1, instanceId: 3, errorType: "api", errorMessage: "bad response", occurredAt: "2026-01-02T03:04:05Z" },
      { id: 2, instanceId: 3, errorType: "timeout", errorMessage: "slow", occurredAt: "2026-01-02T03:04:06Z" },
    ]
    render(<InstanceErrorDisplay instance={{ connected: false, recentErrors } as InstanceResponse} onEdit={vi.fn()} />)
    fireEvent.click(screen.getByRole("button", { name: /Recent errors/ }))

    expect(screen.getByText("API")).not.toBeNull()
    expect(screen.getByText("Unknown")).not.toBeNull()
    expect(screen.queryByText("timeout")).toBeNull()
  })
})
