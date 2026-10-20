// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import "golang.org/x/sys/windows"

// syscall.EADDRINUSE on Windows is a made-up errno that no bind returns.
const errAddrInUse = windows.WSAEADDRINUSE
