// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import "errors"

// Sentinel errors returned by Backend implementations.
var (
	// ErrNoFilesystemAccess is returned by the NoopBackend for instances that
	// have no filesystem access configured (neither local nor remote).
	ErrNoFilesystemAccess = errors.New("filesystem access is not configured for this instance")

	// ErrUnsupported reports a per-host fact: this server lacks the extension
	// the operation needs, or this transport has no such operation at all.
	// Distinct from ErrNoFilesystemAccess, which means nothing was configured.
	ErrUnsupported = errors.New("operation is not supported by this filesystem backend")
)
