/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TrackerIcon } from "@/components/torrents/table/TrackerIcon"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { withBasePath } from "@/lib/base-url"
import { isDemo } from "@/lib/demo"
import { matchPeerClient, peerClientTitle } from "@/lib/peer-client-icons"

export function PeerClientIcon({ client, peerIdClient }: { client?: string; peerIdClient?: string }) {
  const { icon, letter } = matchPeerClient(client, peerIdClient)
  // The demo has no server, so each peer shows its letter.
  const src = icon && !isDemo ? withBasePath(`/api/peer-client-icons/${icon}`) : null
  const title = peerClientTitle(client, peerIdClient)
  return (
    <Tooltip disabled={!title}>
      <TooltipTrigger asChild openOnTap="only">
        <div className="shrink-0">
          <TrackerIcon title={title} fallback={letter} src={src} size="sm" />
        </div>
      </TooltipTrigger>
      <TooltipContent>{title}</TooltipContent>
    </Tooltip>
  )
}
