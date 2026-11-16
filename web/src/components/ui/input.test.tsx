/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"

const IGNORE_ATTRIBUTES = {
  "data-1p-ignore": "true",
  "data-bwignore": "true",
  "data-lpignore": "true",
  "data-protonpass-ignore": "true",
  "data-form-type": "other",
}

function attributesOf(element: HTMLElement) {
  return Object.fromEntries(Object.keys(IGNORE_ATTRIBUTES).map((name) => [name, element.getAttribute(name)]))
}

describe("password managers", () => {
  afterEach(cleanup)

  // regression: #4237
  it("leave a plain field alone, such as the name of a Display profile", () => {
    render(<Input aria-label="Name" />)
    expect(attributesOf(screen.getByLabelText("Name"))).toEqual(IGNORE_ATTRIBUTES)
  })

  // regression: #4237
  it("leave a secret of another service alone, such as an API key", () => {
    render(<Input aria-label="API key" type="password" autoComplete="off" />)
    expect(attributesOf(screen.getByLabelText("API key"))).toEqual(IGNORE_ATTRIBUTES)
  })

  // regression: #4237
  it("leave a text area alone", () => {
    render(<Textarea aria-label="Notes" />)
    expect(attributesOf(screen.getByLabelText("Notes"))).toEqual(IGNORE_ATTRIBUTES)
  })

  // regression: #4237
  it.each(["username", "current-password", "new-password", "one-time-code", "section-login username"])(
    "fill a sign-in field with autoComplete %s",
    (autoComplete) => {
      render(<Input aria-label="Sign-in" autoComplete={autoComplete} />)
      const field = screen.getByLabelText("Sign-in")
      for (const name of Object.keys(IGNORE_ATTRIBUTES)) {
        expect(field.hasAttribute(name), name).toBe(false)
      }
    }
  )
})
