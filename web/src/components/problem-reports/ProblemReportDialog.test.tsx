/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { useState } from "react"
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest"

import { ProblemReportButton, ProblemReportDialog } from "@/components/problem-reports/ProblemReportDialog"
import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { server } from "@/test/msw/server"
import type { ApplicationInfo, ProblemReportFacts, ProblemReportPayload } from "@/types"

const toastSuccess = vi.hoisted(() => vi.fn())
vi.mock("sonner", () => ({ toast: { success: toastSuccess } }))

const facts: ProblemReportFacts = {
  qui: {
    version: "1.2.3",
    startedAt: "2026-11-19T08:00:00Z",
    os: "linux",
    arch: "amd64",
    appContainer: false,
    database: "sqlite",
    logLevel: "INFO",
    experimentalFeatures: ["problemReports"],
  },
  instances: [{ qbittorrentVersion: "v5.1.0", state: "connected", torrentCount: 12 }],
  notices: [],
  logs: { warnings: ["WRN disk almost full"], all: ["INF started", "WRN disk almost full"] },
}

function serveInfo(info: Partial<ApplicationInfo>) {
  server.use(
    http.get("*/api/application/info", () => HttpResponse.json(info)),
    http.get("*/api/problem-reports/facts", () => HttpResponse.json(facts))
  )
}

function Harness() {
  const [open, setOpen] = useState(false)
  return (
    <>
      <ProblemReportButton onOpen={() => setOpen(true)} />
      <ProblemReportDialog open={open} onOpenChange={setOpen} />
    </>
  )
}

function mount() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <Harness />
      </TooltipProvider>
    </QueryClientProvider>
  )
  return queryClient
}

async function openDialog() {
  fireEvent.click(await screen.findByRole("button", { name: "Report a problem" }))
  await screen.findByText(/"WRN disk almost full"/)
  return {
    textField: screen.getByRole("textbox", { name: "What went wrong?" }) as HTMLTextAreaElement,
    sendButton: screen.getByRole("button", { name: "Send" }) as HTMLButtonElement,
  }
}

beforeAll(() => {
  // jsdom has no ResizeObserver, and the Radix popper of the tooltip measures its anchor with one.
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(() => {
  cleanup()
  toastSuccess.mockReset()
})

describe("Problem report entry point", () => {
  it("does not render while the Experimental feature is off", async () => {
    serveInfo({ experimentalFeatures: [], problemReports: true })

    const queryClient = mount()

    await waitFor(() => expect(queryClient.getQueryState(["application-info"])?.status).toBe("success"))
    expect(screen.queryByRole("button", { name: "Report a problem" })).toBeNull()
  })

  it("does not render while the qui config turns Problem reports off", async () => {
    serveInfo({ experimentalFeatures: ["problemReports"], problemReports: false })

    const queryClient = mount()

    await waitFor(() => expect(queryClient.getQueryState(["application-info"])?.status).toBe("success"))
    expect(screen.queryByRole("button", { name: "Report a problem" })).toBeNull()
  })

  it("renders when the Experimental feature and the qui config are both on", async () => {
    serveInfo({ experimentalFeatures: ["problemReports"], problemReports: true })

    mount()

    expect(await screen.findByRole("button", { name: "Report a problem" })).not.toBeNull()
  })
})

describe("ProblemReportDialog", () => {
  it("keeps Send off until the text holds a character that is not a space", async () => {
    serveInfo({ experimentalFeatures: ["problemReports"], problemReports: true })
    mount()

    const { textField, sendButton } = await openDialog()
    expect(sendButton.disabled).toBe(true)

    fireEvent.change(textField, { target: { value: "  \n\t " } })
    expect(sendButton.disabled).toBe(true)

    fireEvent.change(textField, { target: { value: "  x " } })
    expect(sendButton.disabled).toBe(false)
  })

  it("posts the report as multipart and closes on success", async () => {
    serveInfo({ experimentalFeatures: ["problemReports"], problemReports: true })
    const sent: { contentType: string | null; report: ProblemReportPayload }[] = []
    server.use(http.post("*/api/problem-reports", async ({ request }) => {
      const contentType = request.headers.get("content-type")
      const report = JSON.parse((await request.formData()).get("report") as string) as ProblemReportPayload
      sent.push({ contentType, report })
      return HttpResponse.json(
        { id: "r1", submissionId: report.submissionId, title: "Torrents vanish", sentAt: "2026-11-19T09:00:00Z", status: "received", text: report.text },
        { status: 201 }
      )
    }))
    mount()

    const { textField, sendButton } = await openDialog()
    expect(screen.getByText(/"userAgent"/).textContent).toContain(navigator.userAgent)
    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(sendButton)

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    expect(toastSuccess).toHaveBeenCalledWith("Report sent")
    expect(sent).toHaveLength(1)
    expect(sent[0].contentType).toMatch(/^multipart\/form-data/)
    const { report } = sent[0]
    expect(report.text).toBe("Torrents vanish")
    expect(report.startedFrom).toBe("menu")
    expect(report.submissionId).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    expect(report.logs).toEqual({ scope: "warnings", lines: ["WRN disk almost full"] })
    expect(report.qui).toEqual(facts.qui)
    expect(report.instances).toEqual(facts.instances)
    expect(report.failedRequests).toEqual([])
    expect(report.browser.userAgent).toBe(navigator.userAgent)
  })

  it("keeps a newer report open when an earlier Send succeeds after its dialog closed", async () => {
    serveInfo({ experimentalFeatures: ["problemReports"], problemReports: true })
    let answer: (response: Response) => void = () => {}
    server.use(http.post("*/api/problem-reports", () => new Promise<Response>((resolve) => { answer = resolve })))
    mount()

    const first = await openDialog()
    fireEvent.change(first.textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(first.sendButton)
    await waitFor(() => expect(first.sendButton.disabled).toBe(true))
    expect(first.textField.disabled).toBe(true)
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

  it("shows the message for a refusal, keeps the text, and retries with the same submission id", async () => {
    serveInfo({ experimentalFeatures: ["problemReports"], problemReports: true })
    const submissionIds: string[] = []
    const answers = [
      HttpResponse.json({ error: "anonymous_full" }, { status: 429 }),
      HttpResponse.json({ error: "Problem reports are off in the qui config." }, { status: 403 }),
    ]
    server.use(http.post("*/api/problem-reports", async ({ request }) => {
      const report = JSON.parse((await request.formData()).get("report") as string) as ProblemReportPayload
      submissionIds.push(report.submissionId)
      return answers[submissionIds.length - 1]
    }))
    mount()

    const { textField, sendButton } = await openDialog()
    fireEvent.change(textField, { target: { value: "Torrents vanish" } })
    fireEvent.click(sendButton)

    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toBe("Anonymous reports are full for today. Sign in to send now, or try again tomorrow.")
    expect(textField.value).toBe("Torrents vanish")
    expect(screen.queryByRole("dialog")).not.toBeNull()

    fireEvent.click(sendButton)

    await waitFor(() => expect(screen.getByRole("alert").textContent).toBe("Could not send the report: Problem reports are off in the qui config."))
    expect(submissionIds).toHaveLength(2)
    expect(submissionIds[1]).toBe(submissionIds[0])
    expect(toastSuccess).not.toHaveBeenCalled()
  })
})
