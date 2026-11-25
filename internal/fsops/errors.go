// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"errors"
	"fmt"
	"syscall"
)

var (
	ErrNoFilesystemAccess = errors.New("filesystem access is not configured for this instance")

	ErrNotCapable = errors.New("instance filesystem mode does not grant this capability")

	ErrRemoteBackendNotWired = errors.New("remote filesystem backend is not wired into this pool")

	ErrConnectionLost = errors.New("connection to the filesystem backend was lost")

	ErrUnsupported = fmt.Errorf("operation is not supported by this filesystem backend: %w", errors.ErrUnsupported)
)

func IsDirNotEmpty(err error) bool {
	return errors.Is(err, syscall.ENOTEMPTY) || isPlatformDirNotEmpty(err)
}
