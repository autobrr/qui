// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import "errors"

// Sentinel errors returned by Backend implementations.
var (
	// ErrNoFilesystemAccess is returned by the NoopBackend for instances that
	// have no filesystem access configured (neither local nor remote).
	ErrNoFilesystemAccess = errors.New("filesystem access is not configured for this instance")

	// ErrRemoteBackendNotImplemented is returned by Pool.GetBackend for an
	// instance that resolves to remote filesystem access: the SSH backend does
	// not exist yet, which is a different answer from "not configured".
	ErrRemoteBackendNotImplemented = errors.New("remote filesystem backend is not implemented")
)
