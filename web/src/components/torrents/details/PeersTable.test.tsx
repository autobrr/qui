/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import type { SortedPeer } from "@/types"
import "@/i18n"

// Keep the real react-i18next module and swap only useTranslation for a passthrough translator.
vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))

import { PeersTable } from "./PeersTable"

const peer = (key: string, client: string, peerIdClient: string) =>
  ({ key, ip: key, port: 1, client, peer_id_client: peerIdClient, progress: 0 }) as unknown as SortedPeer

describe("PeersTable", () => {
  it("sorts the Client column by the name that it shows", () => {
    render(
      <PeersTable
        peers={[
          peer("203.0.113.1", "", "-qB5120-"),
          peer("203.0.113.2", "qBittorrent/5.1.2", "-qB5120-"),
          peer("203.0.113.3", "Deluge/2.1.1 libtorrent/2.0.10", "-DE211s-"),
          peer("203.0.113.4", "Transmission 4.0.6", "-TR4060-"),
        ]}
        loading={false}
        speedUnit="bytes"
        showFlags={false}
        incognitoMode={false}
      />
    )

    fireEvent.click(screen.getByText("peersTable.client"))

    const names = [...document.querySelectorAll("tbody span.font-medium")].map((el) => el.textContent)
    expect(names).toEqual(["Deluge", "qBittorrent", "qBittorrent", "Transmission"])
  })
})
