/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import type { FilesystemMode, SSHTier } from "@/types"
import { remoteAccessLimits, type RemoteAccessLimit } from "@/lib/remote-access"

describe("remoteAccessLimits", () => {
  it.each<[string, FilesystemMode, SSHTier | undefined, boolean | undefined, RemoteAccessLimit[]]>([
    ["not remote: no limits", "local", undefined, undefined, []],
    ["sftp tier before any probe: no identity, no reflinks, nothing about hardlinks", "remote", "sftp", undefined, ["noIdentity", "noReflinks"]],
    ["sftp tier without the extension", "remote", "sftp", false, ["noIdentity", "noReflinks", "noHardlinkExtension"]],
    ["exec tier with the extension: reflinks only", "remote", "exec", true, ["noReflinks"]],
    ["exec tier without the extension", "remote", "exec", false, ["noReflinks", "noHardlinkExtension"]],
  ])("%s", (_name, filesystemMode, sshTier, sshHardlink, want) => {
    expect(remoteAccessLimits({ filesystemMode, sshTier, sshHardlink })).toEqual(want)
  })
})
