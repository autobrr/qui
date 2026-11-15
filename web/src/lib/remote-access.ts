/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Instance } from "@/types"

// What a remote instance cannot do, from its stored probe. The keys name
// instances.json entries under access.limits.
export type RemoteAccessLimit = "noIdentity" | "noReflinks" | "noHardlinkExtension"

export function remoteAccessLimits(instance: Pick<Instance, "filesystemMode" | "sshTier" | "sshHardlink">): RemoteAccessLimit[] {
  if (instance.filesystemMode !== "remote") {
    return []
  }
  const limits: RemoteAccessLimit[] = []
  if (instance.sshTier !== "exec") {
    limits.push("noIdentity")
  }
  limits.push("noReflinks")
  // An unknown flag (no probe yet) claims nothing.
  if (instance.sshHardlink === false) {
    limits.push("noHardlinkExtension")
  }
  return limits
}
