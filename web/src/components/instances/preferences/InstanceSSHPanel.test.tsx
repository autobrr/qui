/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { Instance, SSHTestResponse } from "@/types"

const mocks = vi.hoisted(() => ({
  api: {
    testSSH: vi.fn(),
    setSSHCredentials: vi.fn(),
    clearSSHCredentials: vi.fn(),
    confirmSSHHostKey: vi.fn(),
    replaceSSHHostKey: vi.fn(),
  },
  invalidateQueries: vi.fn().mockResolvedValue(undefined),
  translation: { t: (key: string) => key },
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.translation,
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
vi.mock("@/components/ui/field-help", () => ({
  FieldHelp: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}))
vi.mock("@/components/ui/copy-button", () => ({ CopyButton: () => null }))
vi.mock("@/lib/incognito", () => ({ useIncognitoMode: () => [false, vi.fn()] }))
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({ invalidateQueries: mocks.invalidateQueries }) }))
vi.mock("@/lib/api", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/lib/api")>(),
  api: mocks.api,
}))

import { APIError } from "@/lib/api"
import { InstanceSSHPanel } from "./InstanceSSHPanel"

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

function makeInstance(overrides: Partial<Instance> = {}): Instance {
  return {
    id: 7,
    name: "Seedbox",
    host: "http://seedbox.example:8080",
    username: "",
    tlsSkipVerify: false,
    hasLocalFilesystemAccess: false,
    capabilities: { read: false, identity: false, trustedIdentity: false, write: false, content: false },
    filesystemMode: "none",
    useHardlinks: false,
    hardlinkBaseDir: "",
    hardlinkDirPreset: "flat",
    useReflinks: false,
    fallbackToRegularMode: false,
    sortOrder: 0,
    isActive: true,
    reannounceSettings: {
      enabled: false, initialWaitSeconds: 0, reannounceIntervalSeconds: 0, maxAgeSeconds: 0, maxRetries: 0, aggressive: false, monitorAll: false,
    } as Instance["reannounceSettings"],
    ...overrides,
  }
}

const storedInstance = makeInstance({ sshHost: "box.example", sshPort: 22, sshUsername: "qui", sshHostKeyPinned: false, sshTier: "sftp" })

function submitForm() {
  const form = document.querySelector("form")
  if (!form) throw new Error("no form")
  fireEvent.submit(form)
}

function setValue(id: string, value: string) {
  fireEvent.change(document.getElementById(id) as HTMLElement, { target: { value } })
}

async function runTest(response: SSHTestResponse) {
  mocks.api.testSSH.mockResolvedValue(response)
  fireEvent.click(screen.getByText("preferences.ssh.buttons.test"))
  await waitFor(() => expect(document.querySelector("[data-ssh-status]")).not.toBeNull())
}

describe("InstanceSSHPanel", () => {
  it.each<[SSHTestResponse, string, string[]]>([
    [{ status: "unpinned", hostKey: "AAAA", fingerprint: "SHA256:abc", keyType: "ssh-ed25519", capabilities: { sftp: true, statvfs: true, hardlink: true, limits: false, exec: true, gnuUserland: true } }, "unpinned", ["preferences.ssh.buttons.confirm", "SHA256:abc", "preferences.ssh.probes.hardlink"]],
    [{ status: "pinned", fingerprint: "SHA256:abc", capabilities: { sftp: true, statvfs: false, hardlink: false, limits: false, exec: false, gnuUserland: false } }, "pinned", ["preferences.ssh.status.pinned.title", "preferences.ssh.probes.tier"]],
    [{ status: "mismatch", hostKey: "BBBB", fingerprint: "SHA256:new", pinnedFingerprint: "SHA256:old" }, "mismatch", ["preferences.ssh.buttons.replace", "SHA256:old", "SHA256:new"]],
    [{ status: "pin_unreadable", hostKey: "BBBB", fingerprint: "SHA256:new" }, "pin_unreadable", ["preferences.ssh.buttons.replace", "SHA256:new"]],
    [{ status: "pin_unreadable" }, "pin_unreadable", ["preferences.ssh.status.pinUnreadable.noKey"]],
    [{ status: "error", error: "dial tcp: connection refused" }, "error", ["preferences.ssh.status.error.title", "dial tcp: connection refused"]],
  ])("renders the %o panel", async (response, status, texts) => {
    render(<InstanceSSHPanel instance={storedInstance} />)
    await runTest(response)

    expect(document.querySelector("[data-ssh-status]")?.getAttribute("data-ssh-status")).toBe(status)
    for (const text of texts) {
      expect(screen.getAllByText(text, { exact: false }).length).toBeGreaterThan(0)
    }
  })

  it("has no Test or Clear without stored credentials", () => {
    render(<InstanceSSHPanel instance={makeInstance()} />)
    expect(screen.queryByText("preferences.ssh.buttons.test")).toBeNull()
    expect(screen.queryByText("preferences.ssh.buttons.clear")).toBeNull()
  })

  it("a save with nothing changed and no key sends nothing", async () => {
    render(<InstanceSSHPanel instance={storedInstance} />)
    submitForm()
    await act(async () => {})
    expect(mocks.api.setSSHCredentials).not.toHaveBeenCalled()
  })

  it("a host change needs the key", async () => {
    render(<InstanceSSHPanel instance={storedInstance} />)
    setValue("ssh-host", "other.example")
    submitForm()
    await waitFor(() => expect(screen.getByText("preferences.ssh.validation.keyRequired")).not.toBeNull())
    expect(mocks.api.setSSHCredentials).not.toHaveBeenCalled()
  })

  it("a pasted key saves all four fields", async () => {
    mocks.api.setSSHCredentials.mockResolvedValue(undefined)
    render(<InstanceSSHPanel instance={storedInstance} />)
    setValue("ssh-private-key", "KEY")
    submitForm()
    await waitFor(() => expect(mocks.api.setSSHCredentials).toHaveBeenCalledWith(7, { host: "box.example", port: 22, username: "qui", privateKey: "KEY" }))
    expect(mocks.invalidateQueries).toHaveBeenCalled()
    await waitFor(() => expect((document.getElementById("ssh-private-key") as HTMLTextAreaElement).value).toBe(""))
    expect(screen.queryByText("preferences.ssh.validation.keyRequired")).toBeNull()
  })

  // regression: #3732
  it("a saved host change leaves no stale key error behind", async () => {
    mocks.api.setSSHCredentials.mockResolvedValue(undefined)
    render(<InstanceSSHPanel instance={storedInstance} />)
    setValue("ssh-host", "other.example")
    setValue("ssh-private-key", "KEY")
    submitForm()
    await waitFor(() => expect(mocks.api.setSSHCredentials).toHaveBeenCalled())
    await waitFor(() => expect((document.getElementById("ssh-private-key") as HTMLTextAreaElement).value).toBe(""))
    expect(screen.queryByText("preferences.ssh.validation.keyRequired")).toBeNull()
    expect((screen.getByText("preferences.ssh.buttons.save") as HTMLButtonElement).disabled).toBe(false)
  })

  it("Confirm posts the host key the test returned", async () => {
    mocks.api.confirmSSHHostKey.mockResolvedValue(undefined)
    render(<InstanceSSHPanel instance={storedInstance} />)
    await runTest({ status: "unpinned", hostKey: "AAAA", fingerprint: "SHA256:abc" })

    mocks.api.testSSH.mockResolvedValue({ status: "pinned", fingerprint: "SHA256:abc", capabilities: { sftp: true, statvfs: true, hardlink: true, limits: true, exec: true, gnuUserland: true } })
    fireEvent.click(screen.getByText("preferences.ssh.buttons.confirm"))
    await waitFor(() => expect(mocks.api.confirmSSHHostKey).toHaveBeenCalledWith(7, "AAAA"))
    await waitFor(() => expect(document.querySelector("[data-ssh-status]")?.getAttribute("data-ssh-status")).toBe("pinned"))
    expect((screen.getByText("preferences.ssh.buttons.test") as HTMLButtonElement).disabled).toBe(false)
  })

  it("a pinned remote instance lists its limits before any test", () => {
    render(<InstanceSSHPanel instance={{ ...storedInstance, filesystemMode: "remote", sshHostKeyPinned: true, sshTier: "sftp", sshHardlink: true }} />)
    expect(screen.getByText("preferences.ssh.test.pinnedAtRest")).not.toBeNull()
    expect(screen.getByText("access.limits.noIdentity")).not.toBeNull()
    expect(screen.getByText("access.limits.noReflinks")).not.toBeNull()
    expect(document.querySelector("[data-ssh-status]")).toBeNull()
  })

  it("the limits under a pinned result follow the test, not the stored flag", async () => {
    render(<InstanceSSHPanel instance={{ ...storedInstance, filesystemMode: "remote", sshHostKeyPinned: true, sshTier: "exec", sshHardlink: true }} />)
    await runTest({ status: "pinned", fingerprint: "SHA256:abc", capabilities: { sftp: true, statvfs: true, hardlink: false, limits: true, exec: true, gnuUserland: true } })
    expect(screen.getByText("access.limits.noHardlinkExtension")).not.toBeNull()
  })

  it("unsaved edits hide the test result and disable Test", async () => {
    render(<InstanceSSHPanel instance={storedInstance} />)
    await runTest({ status: "unpinned", hostKey: "AAAA", fingerprint: "SHA256:abc" })
    setValue("ssh-host", "other.example")
    await waitFor(() => expect(document.querySelector("[data-ssh-status]")).toBeNull())
    expect(screen.getByText("preferences.ssh.test.saveFirst")).not.toBeNull()
    expect((screen.getByText("preferences.ssh.buttons.test") as HTMLButtonElement).disabled).toBe(true)
    // typed back to the stored value: nothing unsaved, Test is back
    setValue("ssh-host", "box.example")
    await waitFor(() => expect((screen.getByText("preferences.ssh.buttons.test") as HTMLButtonElement).disabled).toBe(false))
    expect(screen.queryByText("preferences.ssh.test.saveFirst")).toBeNull()
  })

  // regression: #3732
  it("the fields are off while a save runs", async () => {
    let finish: () => void = () => {}
    mocks.api.setSSHCredentials.mockReturnValue(new Promise<void>((resolve) => { finish = resolve }))
    render(<InstanceSSHPanel instance={storedInstance} />)
    setValue("ssh-private-key", "KEY")
    submitForm()
    await waitFor(() => expect((document.querySelector("fieldset") as HTMLFieldSetElement).disabled).toBe(true))
    finish()
    await waitFor(() => expect((document.querySelector("fieldset") as HTMLFieldSetElement).disabled).toBe(false))
  })

  // regression: #3732
  it("a host the server normalized is shown as stored, with no key error", async () => {
    mocks.api.setSSHCredentials.mockResolvedValue(undefined)
    const { rerender } = render(<InstanceSSHPanel instance={storedInstance} />)
    setValue("ssh-host", "Other.Example")
    setValue("ssh-private-key", "KEY")
    submitForm()
    await waitFor(() => expect(mocks.api.setSSHCredentials).toHaveBeenCalledWith(7, { host: "Other.Example", port: 22, username: "qui", privateKey: "KEY" }))
    // the refetched row carries the lowercased host
    rerender(<InstanceSSHPanel instance={{ ...storedInstance, sshHost: "other.example" }} />)
    await waitFor(() => expect((document.getElementById("ssh-host") as HTMLInputElement).value).toBe("other.example"))
    expect((document.getElementById("ssh-private-key") as HTMLTextAreaElement).value).toBe("")
    expect(screen.queryByText("preferences.ssh.validation.keyRequired")).toBeNull()
    expect((screen.getByText("preferences.ssh.buttons.save") as HTMLButtonElement).disabled).toBe(false)
  })

  it("Replace asks first, then posts the presented key", async () => {
    mocks.api.replaceSSHHostKey.mockResolvedValue(undefined)
    render(<InstanceSSHPanel instance={storedInstance} />)
    await runTest({ status: "mismatch", hostKey: "BBBB", fingerprint: "SHA256:new", pinnedFingerprint: "SHA256:old" })

    fireEvent.click(screen.getByText("preferences.ssh.buttons.replace"))
    expect(mocks.api.replaceSSHHostKey).not.toHaveBeenCalled()
    fireEvent.click(await screen.findByText("preferences.ssh.replaceDialog.confirm"))
    await waitFor(() => expect(mocks.api.replaceSSHHostKey).toHaveBeenCalledWith(7, "BBBB"))
  })

  it("Replace from an unreadable pin asks first, then posts the presented key", async () => {
    mocks.api.replaceSSHHostKey.mockResolvedValue(undefined)
    render(<InstanceSSHPanel instance={storedInstance} />)
    await runTest({ status: "pin_unreadable", hostKey: "BBBB", fingerprint: "SHA256:new" })

    fireEvent.click(screen.getByText("preferences.ssh.buttons.replace"))
    expect(mocks.api.replaceSSHHostKey).not.toHaveBeenCalled()
    fireEvent.click(await screen.findByText("preferences.ssh.replaceDialog.confirm"))
    await waitFor(() => expect(mocks.api.replaceSSHHostKey).toHaveBeenCalledWith(7, "BBBB"))
  })

  it("a refused save shows the server text inline", async () => {
    mocks.api.setSSHCredentials.mockRejectedValue(new APIError("ssh host \"x\" must be a bare hostname or IP, without scheme, port or credentials", 400))
    render(<InstanceSSHPanel instance={storedInstance} />)
    setValue("ssh-private-key", "KEY")
    submitForm()
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("must be a bare hostname or IP"))
    expect(mocks.invalidateQueries).not.toHaveBeenCalled()
  })

  it.each([
    [true, "preferences.ssh.test.pinnedAtRest"],
    [false, "preferences.ssh.test.unpinnedAtRest"],
  ])("says whether a host key is pinned (%s) before any test", (pinned, text) => {
    render(<InstanceSSHPanel instance={{ ...storedInstance, sshHostKeyPinned: pinned }} />)
    expect(screen.getByText(text)).not.toBeNull()
    expect(document.querySelector("[data-ssh-status]")).toBeNull()
  })

  // regression: #3732
  it("a failed test shows the error with no result panel", async () => {
    render(<InstanceSSHPanel instance={storedInstance} />)
    await runTest({ status: "pinned", fingerprint: "SHA256:abc" })

    mocks.api.testSSH.mockRejectedValue(new APIError("Could not reach the instance", 502))
    fireEvent.click(screen.getByText("preferences.ssh.buttons.test"))
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("Could not reach the instance"))
    expect(document.querySelector("[data-ssh-status]")).toBeNull()
  })

  // regression: #3732
  it("an endpoint that changed under the panel drops the old endpoint's result", async () => {
    const { rerender } = render(<InstanceSSHPanel instance={{ ...storedInstance, sshHostKeyPinned: true }} />)
    await runTest({ status: "pinned", fingerprint: "SHA256:abc" })

    rerender(<InstanceSSHPanel instance={{ ...storedInstance, sshHost: "other.example" }} />)
    await waitFor(() => expect((document.getElementById("ssh-host") as HTMLInputElement).value).toBe("other.example"))
    expect(document.querySelector("[data-ssh-status]")).toBeNull()
    expect(screen.getByText("preferences.ssh.test.unpinnedAtRest")).not.toBeNull()
  })

  it("a refused clear shows the server text inline", async () => {
    mocks.api.clearSSHCredentials.mockRejectedValue(new APIError("Disable hardlink or reflink mode on this instance before removing its SSH credentials", 400))
    render(<InstanceSSHPanel instance={storedInstance} />)

    fireEvent.click(screen.getByText("preferences.ssh.buttons.clear"))
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("Disable hardlink or reflink mode"))
    expect(mocks.invalidateQueries).not.toHaveBeenCalled()
  })

  it("says local access wins when the switch is on", () => {
    render(<InstanceSSHPanel instance={makeInstance({ ...storedInstance, hasLocalFilesystemAccess: true })} />)
    expect(screen.getByText("preferences.ssh.localWins")).not.toBeNull()
  })
})
