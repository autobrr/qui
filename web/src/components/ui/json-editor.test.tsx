/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { JsonEditor, preloadJsonEditor } from "./json-editor"

afterEach(cleanup)

describe("JsonEditor", () => {
  it("renders CodeMirror on the first render once preloaded", async () => {
    await preloadJsonEditor()
    const { container } = render(<JsonEditor value="{}" onChange={() => {}} aria-label="json" />)

    expect(container.querySelector(".cm-editor")).not.toBeNull()
    expect(container.querySelector("textarea")).toBeNull()
  })
})
