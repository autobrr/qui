/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { forwardRef } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { _resetClientSettingsForTests, applyServerSettings, readRaw, seedAndMarkReady, writeRaw } from "@/lib/client-settings"
import { _resetFeatureNoticesForTests } from "./due-notice"
import { FeatureNotice } from "./FeatureNotice"
import type { FeatureNoticeId } from "./notices"

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock("./notices", async () => {
  const { readRaw } = await import("@/lib/client-settings")
  // Fake keys, built so that the i18n checker does not look them up.
  const testNotice = (id: string) => ({
    id,
    titleKey: `${id}.title`,
    descriptionKey: `${id}.description`,
    actionLabelKey: `${id}.action`,
    isInUse: () => readRaw(`qui-test-${id}-on`) === "true",
  })
  return { FEATURE_NOTICES: [testNotice("first"), testNotice("second")] }
})

const TestButton = forwardRef<HTMLButtonElement, { label?: string }>(({ label = "control", ...props }, ref) => (
  <button ref={ref} type="button" {...props}>{label}</button>
))

const FIRST_SEEN = "qui-feature-notice-seen:first"
const SECOND_SEEN = "qui-feature-notice-seen:second"

function Absent() {
  return null
}

// The mocked list has its own ids.
function noticeTree(id: string, onAction: () => void) {
  return (
    <FeatureNotice id={id as FeatureNoticeId} onAction={onAction}>
      <TestButton label={`${id}-control`} />
    </FeatureNotice>
  )
}

function renderNotice(id: string, onAction = vi.fn()) {
  render(noticeTree(id, onAction))
  return onAction
}

// The same two calls that useClientSettingsSync makes for the first server snapshot.
function loadServerSnapshot(settings: Record<string, string> = {}) {
  act(() => {
    applyServerSettings(settings)
    seedAndMarkReady(settings)
  })
}

const bubbleShown = () => screen.queryByText("first.title") !== null

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
  _resetFeatureNoticesForTests()
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 200 }))
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe("FeatureNotice", () => {
  it("shows the bubble with its text and buttons when the feature is off", () => {
    loadServerSnapshot()
    renderNotice("first")

    expect(screen.getByText("first.title")).not.toBeNull()
    expect(screen.getByText("first.description")).not.toBeNull()
    expect(screen.getByRole("button", { name: "common:featureNotice.gotIt" })).not.toBeNull()
    expect(screen.getByRole("button", { name: "first.action" })).not.toBeNull()
  })

  it("shows nothing before the first server snapshot arrives", () => {
    renderNotice("first")
    expect(bubbleShown()).toBe(false)

    loadServerSnapshot()
    expect(bubbleShown()).toBe(true)
  })

  it("closes and stores seen after Got it", () => {
    loadServerSnapshot()
    const onAction = renderNotice("first")

    fireEvent.click(screen.getByRole("button", { name: "common:featureNotice.gotIt" }))

    expect(bubbleShown()).toBe(false)
    expect(readRaw(FIRST_SEEN)).toBe("true")
    expect(onAction).not.toHaveBeenCalled()
  })

  it("calls the feature toggle and stores seen after the primary action", () => {
    loadServerSnapshot()
    const onAction = renderNotice("first")

    fireEvent.click(screen.getByRole("button", { name: "first.action" }))

    expect(onAction).toHaveBeenCalledTimes(1)
    expect(bubbleShown()).toBe(false)
    expect(readRaw(FIRST_SEEN)).toBe("true")
  })

  it("closes and stores seen when the feature turns on from outside", () => {
    loadServerSnapshot()
    renderNotice("first")
    expect(bubbleShown()).toBe(true)

    act(() => writeRaw("qui-test-first-on", "true"))

    expect(bubbleShown()).toBe(false)
    expect(readRaw(FIRST_SEEN)).toBe("true")
  })

  it("shows nothing when the server says the notice was seen", () => {
    loadServerSnapshot({ [FIRST_SEEN]: "true" })
    renderNotice("first")
    expect(bubbleShown()).toBe(false)
  })

  it("shows nothing and stores seen when the feature is on at load", () => {
    loadServerSnapshot({ "qui-test-first-on": "true" })
    renderNotice("first")

    expect(bubbleShown()).toBe(false)
    expect(readRaw(FIRST_SEEN)).toBe("true")
  })

  it("stays open, stores nothing, and leaves a click outside and Esc to the page", async () => {
    loadServerSnapshot()
    render(<button type="button">elsewhere</button>)
    renderNotice("first")
    // An outside-click listener would attach on the next tick, as the one of a Radix popover does.
    await act(() => new Promise((resolve) => setTimeout(resolve, 0)))

    const elsewhere = screen.getByRole("button", { name: "elsewhere" })
    fireEvent.pointerDown(elsewhere)
    fireEvent.mouseDown(elsewhere)
    fireEvent.click(elsewhere)
    expect(bubbleShown()).toBe(true)

    // The torrent page clears its selection on an Esc that is not prevented while no dialog is open.
    const pageEscape = vi.fn((event: KeyboardEvent) => ({
      prevented: event.defaultPrevented,
      dialogOpen: document.querySelector("[role=\"dialog\"]") !== null,
    }))
    window.addEventListener("keydown", pageEscape)
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" })
    window.removeEventListener("keydown", pageEscape)

    expect(pageEscape.mock.results[0]?.value).toEqual({ prevented: false, dialogOpen: false })
    expect(bubbleShown()).toBe(true)
    expect(readRaw(FIRST_SEEN)).toBeNull()
  })

  it("shows only the first due notice, and the next one after the next page load", () => {
    loadServerSnapshot()
    renderNotice("first")
    const second = render(noticeTree("second", vi.fn()))

    expect(bubbleShown()).toBe(true)
    expect(screen.queryByText("second.title")).toBeNull()

    fireEvent.click(screen.getByRole("button", { name: "common:featureNotice.gotIt" }))
    second.rerender(noticeTree("second", vi.fn()))
    expect(bubbleShown()).toBe(false)
    expect(screen.queryByText("second.title")).toBeNull()
    expect(readRaw(SECOND_SEEN)).toBeNull()

    cleanup()
    _resetFeatureNoticesForTests()
    renderNotice("first")
    renderNotice("second")

    expect(bubbleShown()).toBe(false)
    expect(screen.queryByText("second.title")).not.toBeNull()
  })

  it("shows nothing and stores nothing while the control is not in the page", () => {
    loadServerSnapshot()
    render(
      <FeatureNotice id={"first" as FeatureNoticeId} onAction={vi.fn()}>
        <Absent />
      </FeatureNotice>
    )
    expect(bubbleShown()).toBe(false)

    act(() => writeRaw("qui-test-first-on", "true"))
    expect(readRaw(FIRST_SEEN)).toBeNull()
  })

  it("lets Tab leave the bubble to the next control in the page", async () => {
    loadServerSnapshot()
    render(
      <>
        {noticeTree("first", vi.fn())}
        <button type="button">next control</button>
      </>
    )

    screen.getByRole("button", { name: "first.action" }).focus()
    await userEvent.tab()

    expect(document.activeElement).toBe(screen.getByRole("button", { name: "next control" }))
  })

  // regression: #4070
  it.each([
    ["Got it", "common:featureNotice.gotIt"],
    ["the primary action", "first.action"],
  ])("returns keyboard focus to the control after %s", async (_, name) => {
    loadServerSnapshot()
    renderNotice("first")

    screen.getByRole("button", { name }).focus()
    await userEvent.keyboard("{Enter}")

    expect(bubbleShown()).toBe(false)
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "first-control" }))
  })

  it("leaves focus where it is when a pointer closes the bubble", () => {
    loadServerSnapshot()
    render(<input aria-label="search" />)
    renderNotice("first")

    screen.getByRole("textbox", { name: "search" }).focus()
    fireEvent.click(screen.getByRole("button", { name: "common:featureNotice.gotIt" }))

    expect(document.activeElement).toBe(screen.getByRole("textbox", { name: "search" }))
  })
})
