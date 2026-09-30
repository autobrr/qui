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

	// ErrNotLocal is returned by Pool.LocalBackend for an instance that no
	// longer has local filesystem access.
	ErrNotLocal = errors.New("instance does not have local filesystem access")

	// ErrRemoteBackendNotWired is returned by a Pool built without a remote
	// factory for an instance in remote mode: a wiring mistake must fail
	// loudly rather than read as "not configured".
	ErrRemoteBackendNotWired = errors.New("remote filesystem backend is not wired into this pool")

	// ErrConnectionLost marks a remote read that failed because the backend
	// could not be reached (the transport dropped, or the pool would not
	// dial), not because of the path. It ends a walk as its one Err entry, and
	// a consumer that skips per-directory errors checks for it so that a walk
	// cut short is not read as complete. The cause is kept as text only, apart
	// from sshpool's own sentinels, so a lost connection never also matches
	// fs.ErrPermission or fs.ErrNotExist.
	ErrConnectionLost = errors.New("connection to the filesystem backend was lost")

	// ErrUnsupported reports a per-host fact: this server lacks the extension
	// the operation needs, or this transport has no such operation at all.
	// Distinct from ErrNoFilesystemAccess, which means nothing was configured.
	ErrUnsupported = fmt.Errorf("operation is not supported by this filesystem backend: %w", errors.ErrUnsupported)
)
