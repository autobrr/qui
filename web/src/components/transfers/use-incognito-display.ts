/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { getLinuxIsoName, getLinuxSavePath, getLinuxTracker, useIncognitoMode } from "@/lib/incognito"
import { INCOGNITO_PEER_ADDRESS } from "@/lib/torrent-peer-address"
import { useMemo } from "react"
import { useTranslation } from "react-i18next"

export function useIncognitoDisplay() {
  const { t } = useTranslation("transfers")
  const [incognito] = useIncognitoMode()
  return useMemo(() => ({
    incognito,
    name: (torrent: { hash: string; name: string }) => incognito ? getLinuxIsoName(torrent.hash) : torrent.name,
    savePath: (hash: string, path: string) => incognito ? getLinuxSavePath(hash) : path,
    tracker: (url: string) => incognito ? getLinuxTracker(url) : url,
    peerAddress: (address: string) => incognito ? INCOGNITO_PEER_ADDRESS : address,
    text: (text: string) => incognito ? t("incognitoHidden") : text,
  }), [incognito, t])
}
