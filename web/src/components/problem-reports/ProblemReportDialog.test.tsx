/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { HttpResponse, delay, http } from "msw"
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest"

import { _resetProblemReportDraftForTests } from "@/components/problem-reports/draft"
import { closeProblemReport } from "@/components/problem-reports/open"
import { ProblemReportButton, ProblemReportDialogRoot, ProblemReportMenuItem } from "@/components/problem-reports/ProblemReportDialog"
import { SENDER_KEY } from "@/components/problem-reports/sender"
import { SENT_REPORTS_KEY } from "@/components/problem-reports/sent-reports"
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { api } from "@/lib/api"
import { _resetClientSettingsForTests, parseJsonNumber, readClientSetting, writeClientSetting } from "@/lib/client-settings"
import { server } from "@/test/msw/server"
import type {
  ApplicationInfo,
  ListedProblemReport,
  ProblemReportFacts,
  ProblemReportPayload,
  ProblemReportSender,
  ProblemReportSignInState
} from "@/types"

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
    http.get("*/api/problem-reports/sender", () => HttpResponse.json({ anonymous: true })),
    http.get("*/api/problem-reports", () => HttpResponse.json([]))
  )
}

const octocat: ProblemReportSender = { anonymous: false, provider: "github", name: "octocat" }
const signInURL = "https://bin.getqui.com/report-sign-in/p1"

function serveSender(...senders: ProblemReportSender[]) {
  let calls = 0
  server.use(http.get("*/api/problem-reports/sender", () => HttpResponse.json(senders[Math.min(calls++, senders.length - 1)])))
}

function serveSignIn(states: ProblemReportSignInState[], start: () => Response = () => HttpResponse.json({ url: signInURL })) {
  const calls = { started: [] as string[], reads: 0, signOuts: 0 }
  server.use(
    http.post("*/api/problem-reports/sign-in", async ({ request }) => {
      calls.started.push(((await request.json()) as { provider: string }).provider)
      return start()
    }),
    http.get("*/api/problem-reports/sign-in", () => HttpResponse.json(states[Math.min(calls.reads++, states.length - 1)])),
    http.delete("*/api/problem-reports/sign-in", () => {
      calls.signOuts++
      return new HttpResponse(null, { status: 204 })
    })
  )
  return calls
}

function fakePopup() {
  return { opener: window as Window | null, location: { href: "" }, close: vi.fn() }
}

function stubPopup() {
  const popup = fakePopup()
  const open = vi.spyOn(window, "open").mockReturnValue(popup as unknown as Window)
  return { popup, open }
}

function holdSignInStart(urls: string[]) {
  let releaseFirst = () => {}
  const heldFirst = new Promise<void>((resolve) => { releaseFirst = resolve })
  let starts = 0
  server.use(http.post("*/api/problem-reports/sign-in", async () => {
    const start = starts++
    if (start === 0) await heldFirst
    return HttpResponse.json({ url: urls[start] })
  }))
  return { releaseFirst, starts: () => starts }
}

function openSenderMenu(sender: string) {
  fireEvent.keyDown(screen.getByRole("button", { name: sender }), { key: "Enter" })
}

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

function serveSend(answers: Response[] = []) {
  const reports: ProblemReportPayload[] = []
  const senders: ProblemReportSender[] = []
  server.use(http.post("*/api/problem-reports", async ({ request }) => {
    const form = await request.formData()
    const report = JSON.parse(form.get("report") as string) as ProblemReportPayload
    reports.push(report)
    senders.push(JSON.parse(form.get("sender") as string) as ProblemReportSender)
    return answers[reports.length - 1] ?? HttpResponse.json(
      { id: "r1", submissionId: report.submissionId, title: "t", sentAt: "2026-11-19T09:00:00Z", status: "received", text: report.text },
      { status: 201 }
    )
  }))
  return { reports, senders }
}

function mount() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } } })
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
  Element.prototype.scrollIntoView = () => {}
  Element.prototype.hasPointerCapture = () => false
  Element.prototype.releasePointerCapture = () => {}
})

afterEach(() => {
  closeProblemReport()
  cleanup()
  toastSuccess.mockReset()
  toastError.mockReset()
  vi.restoreAllMocks()
  vi.useRealTimers()
  localStorage.clear()
  _resetClientSettingsForTests()
  _resetProblemReportDraftForTests()
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
    expect(report.logs).toEqual({ scope: "all", lines: ["DBG sync done", "INF started", "WRN disk almost full"] })
    expect(report.qui).toEqual(baseFacts.qui)
    expect(report.instances).toEqual(baseFacts.instances)
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
    ["Warnings and errors", { scope: "warnings", lines: baseFacts.logs.warnings }],
    ["Leave out", { scope: "none", lines: [] }],
  ])("sends the log lines of the scope %s", async (option, logs) => {
    serve()
    const { reports } = serveSend()
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
    const { reports } = serveSend()
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

  it("retries a refused report with the same submission id", async () => {
    serve()
    const { reports } = serveSend([
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

describe("Draft", () => {
  it("keeps the text and the log choice across a close and a reopen", async () => {
    serve()
    mount()

    const first = await openDialog()
    fireEvent.change(first.textField, { target: { value: "Torrents vanish" } })
    pickLogScope("Warnings and errors")
    fireEvent.click(screen.getByRole("button", { name: "Close" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())

    const second = await openDialog()

    expect(second.textField.value).toBe("Torrents vanish")
    expect(screen.getByRole("combobox", { name: "Log lines to include" }).textContent).toBe("Warnings and errors")
  })

  it("clears the draft after a successful Send", async () => {
    serve()
    serveSend()
    mount()

    const first = await openDialog()
    fireEvent.change(first.textField, { target: { value: "Torrents vanish" } })
    pickLogScope("Warnings and errors")
    fireEvent.click(first.sendButton())
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())

    const second = await openDialog()

    expect(second.textField.value).toBe("")
    expect(screen.getByRole("combobox", { name: "Log lines to include" }).textContent).toBe("Everything recent")
  })

  it("sends a second Send of the same draft with the same submission id, also after a close and a reopen", async () => {
    serve()
    const { reports } = serveSend([HttpResponse.json({ error: "unavailable" }, { status: 502 })])
    mount()

    const first = await openDialog()
    fireEvent.change(first.textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(first.sendButton())
    await screen.findByRole("alert")
    fireEvent.click(screen.getByRole("button", { name: "Close" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    const second = await openDialog()
    fireEvent.click(second.sendButton())

    await waitFor(() => expect(reports).toHaveLength(2))
    expect(reports[1].submissionId).toBe(reports[0].submissionId)
    expect(reports[1].text).toBe("Torrents vanish")
  })

  it("sends a draft from an earlier page load with its submission id", async () => {
    const submissionId = "0b6f8a52-3c1e-4d7a-9f20-5e8c1b2a4d6f"
    localStorage.setItem("qui-problem-report-draft", JSON.stringify({
      text: "Torrents vanish",
      logScope: "none",
      attempt: { submissionId, text: "Torrents vanish", logScope: "none" },
    }))
    serve()
    const { reports } = serveSend()
    mount()

    const { textField, sendButton } = await openDialog()
    expect(textField.value).toBe("Torrents vanish")
    fireEvent.click(sendButton())

    await waitFor(() => expect(reports).toHaveLength(1))
    expect(reports[0].submissionId).toBe(submissionId)
    expect(reports[0].logs).toEqual({ scope: "none", lines: [] })
  })

  it("after a 429 anonymous_full, shows the draft on top of Sent reports as Not sent, and its Delete clears the draft", async () => {
    serve()
    serveSend([HttpResponse.json({ error: "anonymous_full" }, { status: 429 })])
    serveSentReports([listed({ id: "r1", title: "Uploads stall" })])
    mount()

    const { textField, sendButton } = await openDialog()
    expect(screen.getByRole("button", { name: /^Sent reports/ })).not.toBeNull()
    fireEvent.change(textField, { target: { value: "Torrents vanish\nafter a restart" } })
    fireEvent.click(sendButton())
    await screen.findByRole("alert")
    fireEvent.click(screen.getByRole("button", { name: /^Sent reports/ }))

    const rows = Array.from((await screen.findByRole("list")).querySelectorAll("li"))
    expect(rows.map((row) => row.textContent)).toEqual([
      "Torrents vanishNot sent",
      expect.stringContaining("Uploads stallReceived"),
    ])
    fireEvent.click(rows[0].querySelector("button") as HTMLButtonElement)

    await waitFor(() => expect(screen.queryByText("Torrents vanish")).toBeNull())
    expect(screen.getByText("Uploads stall")).not.toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Back to report" }))
    expect((screen.getByRole("textbox", { name: "Your report" }) as HTMLTextAreaElement).value).toBe("")
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("makes a new submission id when the text or the log choice changes after a failed Send", async () => {
    serve()
    const { reports } = serveSend([
      HttpResponse.json({ error: "unavailable" }, { status: 502 }),
      HttpResponse.json({ error: "unavailable" }, { status: 502 }),
      HttpResponse.json({ error: "unavailable" }, { status: 502 }),
    ])
    mount()

    const { textField, sendButton } = await openDialog()
    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(sendButton())
    await waitFor(() => expect(sendButton().disabled).toBe(false))
    fireEvent.change(textField, { target: { value: "Torrents vanish after a restart" } })
    fireEvent.click(sendButton())
    await waitFor(() => expect(reports).toHaveLength(2))
    await waitFor(() => expect(sendButton().disabled).toBe(false))
    pickLogScope("Leave out")
    fireEvent.click(sendButton())

    await waitFor(() => expect(reports).toHaveLength(3))
    expect(new Set(reports.map((report) => report.submissionId)).size).toBe(3)
  })

  it("shows no Not sent draft while its Send runs", async () => {
    serve()
    let answer: (response: Response) => void = () => {}
    server.use(http.post("*/api/problem-reports", () => new Promise<Response>((resolve) => { answer = resolve })))
    mount()

    const { textField, sendButton } = await openDialog()
    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(sendButton())
    await waitFor(() => expect(sendButton().disabled).toBe(true))

    expect(screen.queryByRole("button", { name: /^Sent reports/ })).toBeNull()
    answer(HttpResponse.json({ error: "unavailable" }, { status: 502 }))
    expect(await screen.findByRole("button", { name: /^Sent reports/ })).not.toBeNull()
  })

  it("shows Sent reports with the Not sent draft while no report was sent", async () => {
    serve()
    serveSend([HttpResponse.json({ error: "anonymous_full" }, { status: 429 })])
    mount()

    const { textField, sendButton } = await openDialog()
    expect(screen.queryByRole("button", { name: /^Sent reports/ })).toBeNull()
    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    expect(screen.queryByRole("button", { name: /^Sent reports/ })).toBeNull()
    fireEvent.click(sendButton())
    await screen.findByRole("alert")
    fireEvent.click(screen.getByRole("button", { name: /^Sent reports/ }))

    const rows = Array.from((await screen.findByRole("list")).querySelectorAll("li"))
    expect(rows.map((row) => row.textContent)).toEqual(["Torrents vanishNot sent"])
  })

  it("keeps a text that the user changed while an earlier Send ran, and sends it as a new report", async () => {
    serve()
    let answer: (response: Response) => void = () => {}
    const reports: ProblemReportPayload[] = []
    server.use(http.post("*/api/problem-reports", async ({ request }) => {
      reports.push(JSON.parse((await request.formData()).get("report") as string) as ProblemReportPayload)
      if (reports.length > 1) return HttpResponse.json({ id: "r2" }, { status: 201 })
      return new Promise<Response>((resolve) => { answer = resolve })
    }))
    mount()

    const first = await openDialog()
    fireEvent.change(first.textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(first.sendButton())
    await waitFor(() => expect(reports).toHaveLength(1))
    fireEvent.click(screen.getByRole("button", { name: "Close" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    const second = await openDialog()
    fireEvent.change(second.textField, { target: { value: "Torrents vanish after a restart" } })
    answer(HttpResponse.json({ id: "r1" }, { status: 201 }))
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledTimes(1))

    expect(second.textField.value).toBe("Torrents vanish after a restart")
    fireEvent.click(second.sendButton())
    await waitFor(() => expect(reports).toHaveLength(2))
    expect(reports[1].text).toBe("Torrents vanish after a restart")
    expect(reports[1].submissionId).not.toBe(reports[0].submissionId)
  })

  it("shows the draft that another tab stored while the dialog was closed", async () => {
    serve()
    mount()

    const first = await openDialog()
    fireEvent.change(first.textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(screen.getByRole("button", { name: "Close" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    storeDraftFromAnotherTab({ text: "Uploads stall", logScope: "all" })

    const second = await openDialog()

    expect(second.textField.value).toBe("Uploads stall")
  })

  it("keeps a draft that another tab changed when a Send from a closed dialog succeeds", async () => {
    serve()
    let answer: (response: Response) => void = () => {}
    server.use(http.post("*/api/problem-reports", () => new Promise<Response>((resolve) => { answer = resolve })))
    mount()

    const { textField, sendButton } = await openDialog()
    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(sendButton())
    await waitFor(() => expect(sendButton().disabled).toBe(true))
    fireEvent.click(screen.getByRole("button", { name: "Close" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    const stored = JSON.parse(localStorage.getItem("qui-problem-report-draft") as string) as { attempt: unknown }
    storeDraftFromAnotherTab({ text: "Torrents vanish after a restart", logScope: "all", attempt: stored.attempt })
    answer(HttpResponse.json({ id: "r1" }, { status: 201 }))
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledTimes(1))

    expect(JSON.parse(localStorage.getItem("qui-problem-report-draft") as string)).toEqual({ text: "Torrents vanish after a restart", logScope: "all" })
  })
})

function storeDraftFromAnotherTab(draft: object) {
  localStorage.setItem("qui-problem-report-draft", JSON.stringify(draft))
  window.dispatchEvent(new StorageEvent("storage", { key: "qui-problem-report-draft" }))
}

describe("Sender", () => {
  it("opens the sign-in page in a popup, waits while it is pending, and then sends as the name", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    serve()
    serveSender({ anonymous: true }, { anonymous: false, provider: "discord", name: "wumpus" })
    const signIn = serveSignIn([{ state: "pending" }, { state: "done" }])
    const { senders } = serveSend()
    const { popup, open } = stubPopup()
    mount()

    const { textField, sendButton } = await openDialog()
    openSenderMenu("Anonymous")
    fireEvent.click(screen.getByRole("menuitem", { name: "Sign in with Discord" }))

    expect(open).toHaveBeenCalledTimes(1)
    await screen.findByRole("button", { name: "Waiting for Discord" })
    await waitFor(() => expect(signIn.reads).toBe(1))
    expect(signIn.started).toEqual(["discord"])
    expect(popup.location.href).toBe(signInURL)
    expect(popup.opener).toBeNull()
    expect(screen.queryByRole("alert")).toBeNull()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000)
    })

    await screen.findByRole("button", { name: "wumpus" })
    expect(screen.getByText(/^The maintainers see your Discord name \(wumpus\) and may contact you\./)).not.toBeNull()
    expect(signIn.reads).toBe(2)

    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(sendButton())
    await waitFor(() => expect(senders).toEqual([{ anonymous: false, provider: "discord", name: "wumpus" }]))
  })

  it.each<[string, ProblemReportSignInState, string]>([
    [
      "a Discord account outside the autobrr server",
      { state: "refused", reason: "discord_not_member" },
      "Your Discord account is not in the autobrr Discord server. Join the server and sign in again, sign in with GitHub, or send anonymously.",
    ],
    ["another refusal", { state: "refused", reason: "account_too_new" }, "Sign-in refused: account_too_new"],
    ["a sign-in that expired", { state: "expired" }, "Sign-in expired. Sign in again."],
  ])("for %s, says why the sign-in ended and stays Anonymous", async (_name, state, message) => {
    serve()
    serveSignIn([state])
    stubPopup()
    mount()

    const { textField } = await openDialog()
    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    openSenderMenu("Anonymous")
    fireEvent.click(screen.getByRole("menuitem", { name: "Sign in with Discord" }))

    expect((await screen.findByRole("alert")).textContent).toBe(message)
    expect(screen.getByRole("button", { name: "Anonymous" })).not.toBeNull()
    expect(textField.value).toBe("Torrents vanish")
  })

  it("closes the popup and says so when brrbin cannot start a sign-in", async () => {
    serve()
    const signIn = serveSignIn([], () => HttpResponse.json({ error: "unavailable" }, { status: 502 }))
    const { popup } = stubPopup()
    mount()

    await openDialog()
    openSenderMenu("Anonymous")
    fireEvent.click(screen.getByRole("menuitem", { name: "Sign in with GitHub" }))

    expect((await screen.findByRole("alert")).textContent).toBe("brrbin cannot sign you in right now. Try again later.")
    expect(popup.close).toHaveBeenCalledTimes(1)
    expect(popup.location.href).toBe("")
    expect(signIn.reads).toBe(0)
    expect(screen.getByRole("button", { name: "Anonymous" })).not.toBeNull()
  })

  it("starts no sign-in and says so when the browser blocks the popup", async () => {
    serve()
    const signIn = serveSignIn([])
    vi.spyOn(window, "open").mockReturnValue(null)
    mount()

    await openDialog()
    openSenderMenu("Anonymous")
    fireEvent.click(screen.getByRole("menuitem", { name: "Sign in with GitHub" }))

    expect((await screen.findByRole("alert")).textContent).toBe("Your browser blocked the sign-in window. Allow pop-ups for qui, then sign in again.")
    expect(signIn.started).toEqual([])
    expect(screen.getByRole("button", { name: "Anonymous" })).not.toBeNull()
  })

  it("keeps watching a pending sign-in when the browser blocks the popup of another one", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    serve()
    serveSender({ anonymous: true }, octocat)
    const signIn = serveSignIn([{ state: "pending" }, { state: "done" }])
    const { open } = stubPopup()
    mount()

    await openDialog()
    openSenderMenu("Anonymous")
    fireEvent.click(screen.getByRole("menuitem", { name: "Sign in with GitHub" }))
    await waitFor(() => expect(signIn.reads).toBe(1))
    open.mockReturnValue(null)
    openSenderMenu("Waiting for GitHub")
    fireEvent.click(screen.getByRole("menuitem", { name: "Sign in with Discord" }))
    await screen.findByRole("alert")

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000)
    })

    await screen.findByRole("button", { name: "octocat" })
    expect(signIn.started).toEqual(["github"])
  })

  it("reads the sender again when the user comes back to the tab, as another tab can sign in or out", async () => {
    serve()
    serveSender({ anonymous: true }, octocat)
    mount()

    await openDialog()
    await screen.findByText(/^Anonymous: no name, no account\./)
    act(() => {
      window.dispatchEvent(new Event("visibilitychange"))
    })

    await screen.findByRole("button", { name: "octocat" })
    expect(screen.getByText(/^The maintainers see your GitHub name \(octocat\) and may contact you\./)).not.toBeNull()
  })

  it("stops a pending sign-in when the user picks Anonymous", async () => {
    serve()
    const signIn = serveSignIn([{ state: "pending" }])
    stubPopup()
    mount()

    await openDialog()
    openSenderMenu("Anonymous")
    fireEvent.click(screen.getByRole("menuitem", { name: "Sign in with GitHub" }))
    await waitFor(() => expect(signIn.reads).toBe(1))
    openSenderMenu("Waiting for GitHub")
    fireEvent.click(screen.getByRole("menuitemradio", { name: "Anonymous" }))

    await screen.findByRole("button", { name: "Anonymous" })
    expect(signIn.signOuts).toBe(1)
  })

  it("closes the popup and stays Anonymous when the sign-in starts after the user picked Anonymous", async () => {
    serve()
    const signIn = serveSignIn([{ state: "pending" }])
    const start = holdSignInStart([signInURL])
    const { popup } = stubPopup()
    const queryClient = mount()

    await openDialog()
    openSenderMenu("Anonymous")
    fireEvent.click(screen.getByRole("menuitem", { name: "Sign in with GitHub" }))
    await waitFor(() => expect(start.starts()).toBe(1))
    openSenderMenu("Waiting for GitHub")
    fireEvent.click(screen.getByRole("menuitemradio", { name: "Anonymous" }))
    await screen.findByRole("button", { name: "Anonymous" })
    await waitFor(() => expect(signIn.signOuts).toBe(1))

    start.releaseFirst()
    await waitFor(() => expect(queryClient.isMutating()).toBe(0))

    expect(popup.location.href).toBe("")
    expect(popup.close).toHaveBeenCalledTimes(1)
    expect(screen.getByRole("button", { name: "Anonymous" })).not.toBeNull()
    expect(screen.queryByRole("alert")).toBeNull()
    expect(signIn.reads).toBe(0)
  })

  it("closes the popup of a sign-in that a newer sign-in replaced, and waits for the newer one", async () => {
    serve()
    serveSignIn([{ state: "pending" }])
    const discordURL = "https://bin.getqui.com/report-sign-in/p2"
    const start = holdSignInStart([signInURL, discordURL])
    const first = fakePopup()
    const second = fakePopup()
    vi.spyOn(window, "open").mockReturnValueOnce(first as unknown as Window).mockReturnValueOnce(second as unknown as Window)
    const queryClient = mount()

    await openDialog()
    openSenderMenu("Anonymous")
    fireEvent.click(screen.getByRole("menuitem", { name: "Sign in with GitHub" }))
    await waitFor(() => expect(start.starts()).toBe(1))
    openSenderMenu("Waiting for GitHub")
    fireEvent.click(screen.getByRole("menuitem", { name: "Sign in with Discord" }))
    await waitFor(() => expect(second.location.href).toBe(discordURL))

    start.releaseFirst()
    await waitFor(() => expect(queryClient.isMutating()).toBe(0))

    expect(first.location.href).toBe("")
    expect(first.close).toHaveBeenCalledTimes(1)
    expect(second.close).not.toHaveBeenCalled()
    expect(screen.getByRole("button", { name: "Waiting for Discord" })).not.toBeNull()
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("signs out and sends as Anonymous again", async () => {
    serve()
    serveSender(octocat)
    const signIn = serveSignIn([])
    const { senders } = serveSend()
    mount()

    const { textField, sendButton } = await openDialog()
    await screen.findByRole("button", { name: "octocat" })
    openSenderMenu("octocat")
    fireEvent.click(screen.getByRole("menuitem", { name: "Sign out" }))

    await screen.findByRole("button", { name: "Anonymous" })
    expect(signIn.signOuts).toBe(1)
    expect(screen.getByText(/^Anonymous: no name, no account\./)).not.toBeNull()
    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(sendButton())
    await waitFor(() => expect(senders).toEqual([{ anonymous: true }]))
  })

  it("stays Anonymous after a sign-out when a sender read from before it answers later", async () => {
    serve()
    let releaseRead = () => {}
    const heldRead = new Promise<void>((resolve) => { releaseRead = resolve })
    let reads = 0
    server.use(http.get("*/api/problem-reports/sender", async () => {
      if (++reads === 2) await heldRead
      return HttpResponse.json(octocat)
    }))
    const signIn = serveSignIn([])
    const queryClient = mount()

    await openDialog()
    await screen.findByRole("button", { name: "octocat" })
    act(() => {
      window.dispatchEvent(new Event("visibilitychange"))
    })
    await waitFor(() => expect(reads).toBe(2))
    openSenderMenu("octocat")
    fireEvent.click(screen.getByRole("menuitem", { name: "Sign out" }))
    await screen.findByRole("button", { name: "Anonymous" })
    expect(signIn.signOuts).toBe(1)

    releaseRead()
    await waitFor(() => expect(queryClient.getQueryState(SENDER_KEY)?.fetchStatus).toBe("idle"))

    expect(screen.getByRole("button", { name: "Anonymous" })).not.toBeNull()
    expect(screen.getByText(/^Anonymous: no name, no account\./)).not.toBeNull()
  })

  it("sends as Anonymous while qui has not answered which sender it holds", async () => {
    serve()
    server.use(http.get("*/api/problem-reports/sender", async () => {
      await delay("infinite")
    }))
    const { senders } = serveSend()
    mount()

    const { textField, sendButton } = await openDialog()
    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(sendButton())

    await waitFor(() => expect(senders).toEqual([{ anonymous: true }]))
  })

  it("when brrbin rejects the sign-in on Send, says that it ended, keeps the text, and keeps the user on the page", async () => {
    serve()
    serveSender(octocat, { anonymous: true })
    serveSend([HttpResponse.json({ error: "token_rejected" }, { status: 409 })])
    mount()
    const page = window.location.href

    const { textField, sendButton } = await openDialog()
    await screen.findByRole("button", { name: "octocat" })
    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(sendButton())

    expect((await screen.findByRole("alert")).textContent).toBe("Your sign-in ended. Sign in again, then send.")
    await screen.findByRole("button", { name: "Anonymous" })
    expect(textField.value).toBe("Torrents vanish")
    expect(screen.queryByRole("dialog")).not.toBeNull()
    expect(window.location.href).toBe(page)
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

  it("keeps a deleted row gone when a list that Sent reports asked for during the Delete answers later", async () => {
    serve()
    const rows = [listed({ id: "r2", title: "Uploads stall" }), listed({ id: "r1", title: "Torrents vanish" })]
    let releaseList = () => {}
    const heldList = new Promise<void>((resolve) => { releaseList = resolve })
    let lists = 0
    server.use(http.get("*/api/problem-reports", async () => {
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
