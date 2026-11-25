/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it } from "vitest"
import { cleanup, render } from "@testing-library/react"
import { SettingsSummary } from "@/components/instances/preferences/SettingsSummary"

afterEach(cleanup)

describe("SettingsSummary", () => {
  it("keeps each pair with its separator on one line", () => {
    const { container } = render(<p><SettingsSummary parts={["Wait 15s", "Retry 7s", "Max 50x"]} /></p>)

    const spans = Array.from(container.querySelectorAll("p > span"))
    const pairs = spans.map((el) => el.textContent)
    expect(pairs).toEqual(["Wait 15s\u00a0·", "Retry 7s\u00a0·", "Max 50x"])
    expect(container.querySelector("p")!.textContent).toBe(pairs.join(" "))
  })
})
