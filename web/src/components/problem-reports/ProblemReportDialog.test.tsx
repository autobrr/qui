/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest"

import { closeProblemReport } from "@/components/problem-reports/open"
import { ProblemReportButton, ProblemReportDialogRoot, ProblemReportMenuItem } from "@/components/problem-reports/ProblemReportDialog"
import { SENT_REPORTS_KEY } from "@/components/problem-reports/sent-reports"
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { api } from "@/lib/api"
import { _resetClientSettingsForTests, parseJsonNumber, readClientSetting, writeClientSetting } from "@/lib/client-settings"
import { server } from "@/test/msw/server"
import type { ApplicationInfo, ListedProblemReport, ProblemReportFacts, ProblemReportPayload } from "@/types"

const { toastSuccess, toastError } = vi.hoisted(() => ({ toastSuccess: vi.fn(), toastError: vi.fn() }))
vi.mock("sonner", () => ({ toast: { success: toastSuccess, error: toastError } }))

const baseFacts: ProblemReportFacts = {
  qui: {
    version: "1.2.3",
    startedAt: "2026-11-19T08:00:00Z",
    os: "linux",
    arch: "amd64",
    appContainer: false,
    database: "sqlite",
    logLevel: "DEBUG",
    experimentalFeatures: ["problemReports"],
  },
  instances: [{ qbittorrentVersion: "v5.1.0", state: "connected", torrentCount: 12 }],
  notices: [],
  logs: { warnings: ["WRN disk almost full"], all: ["DBG sync done", "INF started", "WRN disk almost full"] },
}

function serve(facts: ProblemReportFacts = baseFacts, info: Partial<ApplicationInfo> = { experimentalFeatures: ["problemReports"], problemReports: true }) {
  server.use(
    http.get("*/api/application/info", () => HttpResponse.json(info)),
    http.get("*/api/problem-reports/facts", () => HttpResponse.json(facts)),
    http.get("*/api/problem-reports", () => HttpResponse.json([]))
  )
}

/** Answers the list route with reports, and counts its calls. */
function serveSentReports(reports: ListedProblemReport[]) {
  const calls = { count: 0 }
  server.use(http.get("*/api/problem-reports", () => {
    calls.count++
    return HttpResponse.json(reports)
  }))
  return calls
}

function withQui(qui: Partial<ProblemReportFacts["qui"]>): ProblemReportFacts {
  return { ...baseFacts, qui: { ...baseFacts.qui, ...qui } }
}

/** Records each report that reaches the send route, and answers with the next answer, or 201. */
function serveSend(answers: Response[] = []) {
  const reports: ProblemReportPayload[] = []
  server.use(http.post("*/api/problem-reports", async ({ request }) => {
    const report = JSON.parse((await request.formData()).get("report") as string) as ProblemReportPayload
    reports.push(report)
    return answers[reports.length - 1] ?? HttpResponse.json(
      { id: "r1", submissionId: report.submissionId, title: "t", sentAt: "2026-11-19T09:00:00Z", status: "received", text: report.text },
      { status: 201 }
    )
  }))
  return reports
}

function mount() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <ProblemReportButton />
        <DropdownMenu open modal={false}>
          <DropdownMenuTrigger>menu</DropdownMenuTrigger>
          <DropdownMenuContent>
            <ProblemReportMenuItem />
          </DropdownMenuContent>
        </DropdownMenu>
        <ProblemReportDialogRoot />
      </TooltipProvider>
    </QueryClientProvider>
  )
  return queryClient
}

async function openDialog() {
  fireEvent.click(await screen.findByRole("button", { name: "Report a problem" }))
  await screen.findByRole("button", { name: "Details" })
  return {
    textField: screen.getByRole("textbox", { name: "Your report" }) as HTMLTextAreaElement,
    sendButton: () => screen.getByRole("button", { name: /^Send/ }) as HTMLButtonElement,
  }
}

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
}

function pickLogScope(name: string) {
  fireEvent.keyDown(screen.getByRole("combobox", { name: "Log lines to include" }), { key: "Enter" })
  fireEvent.keyDown(screen.getByRole("option", { name }), { key: "Enter" })
}

beforeAll(() => {
  // jsdom has no ResizeObserver, and the Radix popper of the tooltip measures its anchor with one.
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
  // The Radix select scrolls its picked option into view and captures the pointer, which jsdom lacks.
  Element.prototype.scrollIntoView = () => {}
  Element.prototype.hasPointerCapture = () => false
  Element.prototype.releasePointerCapture = () => {}
})

afterEach(() => {
  closeProblemReport()
  cleanup()
  toastSuccess.mockReset()
  toastError.mockReset()
  localStorage.clear()
  _resetClientSettingsForTests()
})

describe("Problem report entry point", () => {
  it("does not render while the Experimental feature is off", async () => {
    serve(baseFacts, { experimentalFeatures: [], problemReports: true })

    const queryClient = mount()

    await waitFor(() => expect(queryClient.getQueryState(["application-info"])?.status).toBe("success"))
    expect(screen.queryByRole("button", { name: "Report a problem" })).toBeNull()
    expect(screen.queryByRole("menuitem", { name: "Report a problem" })).toBeNull()
  })

  it("does not render while the qui config turns Problem reports off", async () => {
    serve(baseFacts, { experimentalFeatures: ["problemReports"], problemReports: false })

    const queryClient = mount()

    await waitFor(() => expect(queryClient.getQueryState(["application-info"])?.status).toBe("success"))
    expect(screen.queryByRole("button", { name: "Report a problem" })).toBeNull()
    expect(screen.queryByRole("menuitem", { name: "Report a problem" })).toBeNull()
  })

  it("renders when the Experimental feature and the qui config are both on", async () => {
    serve()

    mount()

    expect(await screen.findByRole("button", { name: "Report a problem" })).not.toBeNull()
    expect(screen.getByRole("menuitem", { name: "Report a problem" })).not.toBeNull()
  })
})

describe("ProblemReportDialog", () => {
  it("keeps Send off until the text holds a character that is not a space, and while a Send runs", async () => {
    serve()
    server.use(http.post("*/api/problem-reports", () => new Promise<Response>(() => {})))
    mount()

    const { textField, sendButton } = await openDialog()
    expect(sendButton().disabled).toBe(true)

    fireEvent.change(textField, { target: { value: "  \n\t " } })
    expect(sendButton().disabled).toBe(true)

    fireEvent.change(textField, { target: { value: "  x " } })
    expect(sendButton().disabled).toBe(false)

    fireEvent.click(sendButton())
    await waitFor(() => expect(sendButton().disabled).toBe(true))
    expect(textField.disabled).toBe(true)
  })

  it("shows each open notice with its cause, as Send includes it", async () => {
    serve({
      ...baseFacts,
      notices: [{ kind: "recording_stopped", severity: "error", cause: "database is locked", openedAt: "2026-11-19T08:30:00Z" }],
    })
    mount()

    await openDialog()
    fireEvent.click(screen.getByRole("button", { name: "Details" }))

    expect(screen.getByText(/Recording stopped · .+ · database is locked/)).not.toBeNull()
  })

  it("posts the report as multipart, with the facts, the browser facts and the failed requests, and closes on success", async () => {
    serve()
    server.use(http.get("*/api/instances/9/torrents", () => HttpResponse.json({ error: "instance offline" }, { status: 502 })))
    await expect(api.request("/instances/9/torrents?filter=x")).rejects.toThrow()
    let contentType: string | null = null
    const reports: ProblemReportPayload[] = []
    server.use(http.post("*/api/problem-reports", async ({ request }) => {
      contentType = request.headers.get("content-type")
      const report = JSON.parse((await request.formData()).get("report") as string) as ProblemReportPayload
      reports.push(report)
      return HttpResponse.json({ id: "r1", submissionId: report.submissionId, title: "t", sentAt: "2026-11-19T09:00:00Z", status: "received", text: report.text }, { status: 201 })
    }))
    mount()

    const { textField, sendButton } = await openDialog()
    fireEvent.click(screen.getByRole("button", { name: "Details" }))
    // regression: #4288
    expect(screen.getByText(new RegExp(`Browser: ${escapeRegExp(navigator.userAgent)}`))).not.toBeNull()
    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(sendButton())

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    expect(toastSuccess).toHaveBeenCalledWith("Report sent")
    expect(reports).toHaveLength(1)
    expect(contentType).toMatch(/^multipart\/form-data/)
    const [report] = reports
    expect(report.text).toBe("Torrents vanish")
    expect(report.startedFrom).toBe("menu")
    expect(report.submissionId).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    expect(report.logs).toEqual({ scope: "warnings", lines: ["WRN disk almost full"] })
    expect(report.qui).toEqual(baseFacts.qui)
    expect(report.instances).toEqual(baseFacts.instances)
    // regression: #4288
    expect(report.browser.userAgent).toBe(navigator.userAgent)
    expect(report.browser).toEqual({
      route: window.location.pathname,
      userAgent: navigator.userAgent,
      viewportWidth: window.innerWidth,
      viewportHeight: window.innerHeight,
      devicePixelRatio: window.devicePixelRatio,
    })
    expect(report.failedRequests).toContainEqual(expect.objectContaining({ method: "GET", path: "/api/instances/9/torrents", status: 502, error: "instance offline" }))
  })

  it.each([
    ["Everything recent", { scope: "all", lines: baseFacts.logs.all }],
    ["Leave out", { scope: "none", lines: [] }],
  ])("sends the log lines of the scope %s", async (option, logs) => {
    serve()
    const reports = serveSend()
    mount()

    const { textField, sendButton } = await openDialog()
    pickLogScope(option)
    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(sendButton())

    await waitFor(() => expect(reports).toHaveLength(1))
    expect(reports[0].logs).toEqual(logs)
  })

  it.each([
    ["INFO", true],
    ["DEBUG", false],
    ["trace", false],
  ])("with the Log level %s, says that debug lines are missing: %s", async (logLevel, missing) => {
    serve(withQui({ logLevel }))
    mount()

    await openDialog()

    expect(screen.queryByText(/does not record debug lines/) !== null).toBe(missing)
  })

  it("names a newer release above Send and lets the user send anyway", async () => {
    serve(withQui({ version: "1.30.0", newerRelease: { version: "v1.36.0", url: "https://github.com/autobrr/qui/releases/tag/v1.36.0" } }))
    const reports = serveSend()
    mount()

    const { textField, sendButton } = await openDialog()

    const link = screen.getByRole("link", { name: "v1.36.0" })
    expect(link.getAttribute("href")).toBe("https://github.com/autobrr/qui/releases/tag/v1.36.0")
    expect(link.parentElement?.textContent).toBe("You run v1.30.0. v1.36.0 is out.")
    expect(sendButton().textContent).toBe("Send anyway")

    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(sendButton())
    await waitFor(() => expect(reports).toHaveLength(1))
  })

  it("from 1024 px, shows what gets sent in a second column with no click", async () => {
    const matchMedia = window.matchMedia
    window.matchMedia = ((query: string) => ({
      matches: query === "(min-width: 1024px)",
      addEventListener: () => {},
      removeEventListener: () => {},
    })) as unknown as typeof window.matchMedia
    try {
      serve()
      mount()

      fireEvent.click(await screen.findByRole("button", { name: "Report a problem" }))

      const column = await screen.findByRole("complementary", { name: "What gets sent" })
      await waitFor(() => expect(column.textContent).toContain("WRN disk almost full"))
      expect(column.textContent).toContain("v5.1.0")
      expect(screen.queryByRole("button", { name: "Details" })).toBeNull()
    } finally {
      window.matchMedia = matchMedia
    }
  })

  it("shows no release line and reads Send while no newer release exists", async () => {
    serve()
    mount()

    const { sendButton } = await openDialog()

    expect(screen.queryByText(/ is out\./)).toBeNull()
    expect(sendButton().textContent).toBe("Send")
  })

  it.each([
    [429, "anonymous_full", "Anonymous reports are full for todaySign in to send now, or try again tomorrow. Your text stays here as a draft."],
    [403, "sign_in_required", "Sign in to send this report. Your text stays here as a draft."],
    [429, "account_limit", "You sent many reports in the last hour. Try again later."],
    [409, "reporter_full", "Your reports use all the room brrbin gives one person. Delete an old report in Sent reports, then send again."],
    [413, "too_large", "The report is too large. Pick fewer log lines or a smaller image."],
    [502, "unavailable", "brrbin cannot take reports right now. Try again later."],
  ])("for a %i %s refusal, shows its message and keeps the text", async (status, reason, message) => {
    serve()
    serveSend([HttpResponse.json({ error: reason }, { status })])
    mount()

    const { textField, sendButton } = await openDialog()
    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(sendButton())

    expect((await screen.findByRole("alert")).textContent).toBe(message)
    expect(textField.value).toBe("Torrents vanish")
    expect(screen.queryByRole("dialog")).not.toBeNull()
    expect(toastSuccess).not.toHaveBeenCalled()
  })

  // regression: #4288
  it("retries a refused report with the same submission id", async () => {
    serve()
    const reports = serveSend([
      HttpResponse.json({ error: "anonymous_full" }, { status: 429 }),
      HttpResponse.json({ error: "Problem reports are off in the qui config." }, { status: 403 }),
    ])
    mount()

    const { textField, sendButton } = await openDialog()
    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(sendButton())
    await screen.findByRole("alert")
    fireEvent.click(sendButton())

    await waitFor(() => expect(screen.getByRole("alert").textContent).toBe("Could not send the report: Problem reports are off in the qui config."))
    expect(reports).toHaveLength(2)
    expect(reports[1].submissionId).toBe(reports[0].submissionId)
  })

  // regression: #4288
  it("keeps a newer report open when an earlier Send succeeds after its dialog closed", async () => {
    serve()
    let answer: (response: Response) => void = () => {}
    server.use(http.post("*/api/problem-reports", () => new Promise<Response>((resolve) => { answer = resolve })))
    mount()

    const first = await openDialog()
    fireEvent.change(first.textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(first.sendButton())
    await waitFor(() => expect(first.sendButton().disabled).toBe(true))
    fireEvent.click(screen.getByRole("button", { name: "Close" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())

    const second = await openDialog()
    fireEvent.change(second.textField, { target: { value: "Another problem" } })
    answer(HttpResponse.json(
      { id: "r1", submissionId: "s1", title: "Torrents vanish", sentAt: "2026-11-19T09:00:00Z", status: "received", text: "Torrents vanish" },
      { status: 201 }
    ))

    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("Report sent"))
    expect(screen.queryByRole("dialog")).not.toBeNull()
    expect(second.textField.value).toBe("Another problem")
  })
})

const sentAt = "2026-11-10T09:00:00Z"

function listed(report: Partial<ListedProblemReport> & Pick<ListedProblemReport, "id" | "title">): ListedProblemReport {
  return { sentAt, status: "received", statusChangedAt: sentAt, ...report }
}

const issueURL = "https://github.com/autobrr/qui/issues/4321"
const SEEN_AT_KEY = "qui-problem-reports-seen-at"

describe("Sent reports", () => {
  it("asks qui once when the dialog opens, and shows no entry while no report exists", async () => {
    serve()
    const calls = serveSentReports([])
    mount()

    await openDialog()

    await waitFor(() => expect(calls.count).toBe(1))
    expect(screen.queryByRole("button", { name: /^Sent reports/ })).toBeNull()
  })

  it("shows each Report status, and links an issue to its URL", async () => {
    serve()
    const calls = serveSentReports([
      listed({ id: "r4", title: "Uploads stall" }),
      listed({ id: "r3", title: "Search finds nothing", status: "issue", issue: { number: 4321, url: issueURL, closed: false } }),
      listed({ id: "r2", title: "Torrents vanish", status: "issue", issue: { number: 4300, url: "https://github.com/autobrr/qui/discussions/4300", closed: true } }),
      listed({ id: "r1", title: "Pin an instance", status: "closed", reason: "Already in v1.36.0: Settings, Instances." }),
    ])
    mount()

    await openDialog()
    fireEvent.click(await screen.findByRole("button", { name: /^Sent reports/ }))

    const list = await screen.findByRole("list")
    expect(screen.getByRole("heading", { name: "Sent reports" })).not.toBeNull()
    expect(Array.from(list.querySelectorAll("li"), (row) => row.textContent?.includes("Received") ?? false)).toEqual([true, false, false, false])
    const issue = screen.getByRole("link", { name: "Became issue #4321" })
    expect(issue.getAttribute("href")).toBe(issueURL)
    const closedIssue = screen.getByRole("link", { name: "Issue #4300 closed" })
    expect(closedIssue.getAttribute("href")).toBe("https://github.com/autobrr/qui/discussions/4300")
    expect(screen.getByText("Closed: Already in v1.36.0: Settings, Instances.")).not.toBeNull()
    expect(calls.count).toBe(2)
  })

  it("counts each status that changed since Sent reports last opened, and opening it clears the number", async () => {
    serve()
    const changedAt = "2026-11-19T10:00:00Z"
    serveSentReports([
      listed({ id: "r3", title: "New and unchanged" }),
      listed({ id: "r2", title: "Became an issue", status: "issue", issue: { number: 4321, url: issueURL, closed: false }, statusChangedAt: changedAt }),
      listed({ id: "r1", title: "Closed", status: "closed", reason: "Fixed for the next qui release.", statusChangedAt: changedAt }),
    ])
    mount()

    await openDialog()

    const entry = await screen.findByRole("button", { name: "Sent reports 2 statuses changed" })
    fireEvent.click(entry)
    await screen.findByRole("list")
    fireEvent.click(screen.getByRole("button", { name: "Back to report" }))

    await waitFor(() => expect(screen.getByRole("button", { name: /^Sent reports/ }).textContent).toBe("Sent reports"))
  })

  it("counts a status that changed after Sent reports last opened, also when the clock of the browser runs ahead", async () => {
    serve()
    const reports = [listed({ id: "r1", title: "Uploads stall", status: "issue", issue: { number: 4321, url: issueURL, closed: false }, statusChangedAt: "2026-11-19T10:00:00Z" })]
    serveSentReports(reports)
    const now = vi.spyOn(Date, "now").mockReturnValue(Date.parse("2027-11-19T10:00:00Z"))
    try {
      mount()
      await openDialog()
      fireEvent.click(await screen.findByRole("button", { name: /^Sent reports/ }))
      await screen.findByRole("list")
      act(() => closeProblemReport())
      reports[0] = listed({ id: "r1", title: "Uploads stall", status: "closed", reason: "Fixed for the next qui release.", statusChangedAt: "2026-11-20T10:00:00Z" })

      await openDialog()

      expect(await screen.findByRole("button", { name: "Sent reports 1 status changed" })).not.toBeNull()
    } finally {
      now.mockRestore()
    }
  })

  // regression: #4302
  it("keeps a newer seen time that another browser stored when Sent reports shows older statuses", async () => {
    serve()
    serveSentReports([listed({ id: "r1", title: "Uploads stall", status: "issue", issue: { number: 4321, url: issueURL, closed: false }, statusChangedAt: "2026-11-19T10:00:00Z" })])
    const seenElsewhere = Date.parse("2026-11-20T10:00:00Z")
    writeClientSetting(SEEN_AT_KEY, seenElsewhere)
    mount()

    await openDialog()
    fireEvent.click(await screen.findByRole("button", { name: /^Sent reports/ }))
    await screen.findByRole("list")

    expect(readClientSetting(SEEN_AT_KEY, { defaultValue: null, parse: parseJsonNumber })).toBe(seenElsewhere)
  })

  it("asks qui once for each open, also when the layout changes", async () => {
    const matchMedia = window.matchMedia
    let wide = false
    const listeners = new Set<() => void>()
    window.matchMedia = ((query: string) => ({
      matches: wide && query === "(min-width: 1024px)",
      addEventListener: (_: string, listener: () => void) => listeners.add(listener),
      removeEventListener: (_: string, listener: () => void) => listeners.delete(listener),
    })) as unknown as typeof window.matchMedia
    const list = vi.spyOn(api, "getSentProblemReports")
    try {
      serve()
      serveSentReports([listed({ id: "r1", title: "Uploads stall" })])
      mount()
      await openDialog()
      await screen.findByRole("button", { name: /^Sent reports/ })

      wide = true
      act(() => listeners.forEach((listener) => listener()))

      await screen.findByRole("complementary", { name: "What gets sent" })
      expect(screen.getByRole("button", { name: /^Sent reports/ })).not.toBeNull()
      expect(list).toHaveBeenCalledTimes(1)
    } finally {
      window.matchMedia = matchMedia
      list.mockRestore()
    }
  })

  it("keeps the text of the report across Sent reports", async () => {
    serve()
    serveSentReports([listed({ id: "r1", title: "Uploads stall" })])
    mount()

    const { textField } = await openDialog()
    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(await screen.findByRole("button", { name: /^Sent reports/ }))
    await screen.findByRole("list")
    fireEvent.click(screen.getByRole("button", { name: "Back to report" }))

    expect((screen.getByRole("textbox", { name: "Your report" }) as HTMLTextAreaElement).value).toBe("Torrents vanish")
  })

  it("deletes a report and removes its row, and keeps a row that brrbin could not delete", async () => {
    serve()
    serveSentReports([listed({ id: "r2", title: "Uploads stall" }), listed({ id: "r1", title: "Torrents vanish" })])
    const deleted: string[] = []
    server.use(http.delete("*/api/problem-reports/:id", ({ params }) => {
      deleted.push(params.id as string)
      return params.id === "r2" ? new HttpResponse(null, { status: 204 }) : HttpResponse.json({ error: "unavailable" }, { status: 502 })
    }))
    mount()

    await openDialog()
    fireEvent.click(await screen.findByRole("button", { name: /^Sent reports/ }))
    const list = await screen.findByRole("list")
    const [first, second] = Array.from(list.querySelectorAll("li"))
    fireEvent.click(first.querySelector("button") as HTMLButtonElement)

    await waitFor(() => expect(screen.queryByText("Uploads stall")).toBeNull())
    expect(deleted).toEqual(["r2"])

    fireEvent.click(second.querySelector("button") as HTMLButtonElement)
    await waitFor(() => expect(toastError).toHaveBeenCalledWith("brrbin cannot delete reports right now. Try again later."))
    expect(screen.getByText("Torrents vanish")).not.toBeNull()
  })

  // regression: #4302
  it("keeps a deleted row gone when a list that Sent reports asked for during the Delete answers later", async () => {
    serve()
    const rows = [listed({ id: "r2", title: "Uploads stall" }), listed({ id: "r1", title: "Torrents vanish" })]
    let releaseList = () => {}
    const heldList = new Promise<void>((resolve) => { releaseList = resolve })
    let lists = 0
    server.use(http.get("*/api/problem-reports", async () => {
      // The third list, from the second open of Sent reports, read its rows before the Delete ended.
      if (++lists === 3) await heldList
      return HttpResponse.json(rows)
    }))
    let releaseDelete = () => {}
    const heldDelete = new Promise<void>((resolve) => { releaseDelete = resolve })
    server.use(http.delete("*/api/problem-reports/:id", async () => {
      await heldDelete
      return new HttpResponse(null, { status: 204 })
    }))
    const queryClient = mount()

    await openDialog()
    fireEvent.click(await screen.findByRole("button", { name: /^Sent reports/ }))
    fireEvent.click((await screen.findByRole("list")).querySelector("button") as HTMLButtonElement)
    fireEvent.click(screen.getByRole("button", { name: "Back to report" }))
    fireEvent.click(screen.getByRole("button", { name: /^Sent reports/ }))
    await screen.findByRole("list")
    await waitFor(() => expect(lists).toBe(3))

    releaseDelete()
    await waitFor(() => expect(screen.queryByText("Uploads stall")).toBeNull())
    releaseList()
    await waitFor(() => expect(queryClient.getQueryState(SENT_REPORTS_KEY)?.fetchStatus).toBe("idle"))

    expect(screen.queryByText("Uploads stall")).toBeNull()
    expect(screen.getByText("Torrents vanish")).not.toBeNull()
  })

  it("keeps the Delete of a row busy while the Delete of another row runs", async () => {
    serve()
    serveSentReports([listed({ id: "r2", title: "Uploads stall" }), listed({ id: "r1", title: "Torrents vanish" })])
    let release = () => {}
    const held = new Promise<void>((resolve) => { release = resolve })
    server.use(http.delete("*/api/problem-reports/:id", async () => {
      await held
      return new HttpResponse(null, { status: 204 })
    }))
    mount()

    await openDialog()
    fireEvent.click(await screen.findByRole("button", { name: /^Sent reports/ }))
    const [first, second] = Array.from((await screen.findByRole("list")).querySelectorAll("button"))
    fireEvent.click(first)
    fireEvent.click(second)

    await waitFor(() => expect(second.disabled).toBe(true))
    expect(first.disabled).toBe(true)
    release()
    await waitFor(() => expect(screen.queryByText("Uploads stall") ?? screen.queryByText("Torrents vanish")).toBeNull())
  })
})
