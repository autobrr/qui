// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by Backend implementations.
var (
	// ErrNoFilesystemAccess is returned by the NoopBackend for instances that
	// have no filesystem access configured (neither local nor remote).
	ErrNoFilesystemAccess = errors.New("filesystem access is not configured for this instance")

	// ErrRemoteBackendNotWired is returned by a Pool built without a remote
	// factory for an instance in remote mode: a wiring mistake must fail
	// loudly rather than read as "not configured".
	ErrRemoteBackendNotWired = errors.New("remote filesystem backend is not wired into this pool")

	// ErrConnectionLost marks a WalkDir Err entry that ends the walk because
	// the backend could no longer be reached (the transport dropped, or the
	// pool refused the redial), not because a directory was unreadable. A
	// consumer that skips per-directory errors must not read a walk cut short
	// as complete; this is the sentinel it checks.
	ErrConnectionLost = errors.New("connection to the filesystem backend was lost")

	// ErrUnsupported reports a per-host fact: this server lacks the extension
	// the operation needs, or this transport has no such operation at all.
	// Distinct from ErrNoFilesystemAccess, which means nothing was configured.
	ErrUnsupported = fmt.Errorf("operation is not supported by this filesystem backend: %w", errors.ErrUnsupported)
)
