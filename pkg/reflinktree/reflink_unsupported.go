// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !linux && !darwin && !windows

package reflinktree

func SupportsReflink(_ string) (supported bool, reason string) {
	return false, "reflink is not supported on this operating system"
}

func cloneFile(_, _ string) error {
	return ErrReflinkUnsupported
}
