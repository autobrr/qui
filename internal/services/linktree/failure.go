// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package linktree

import (
	"errors"
	"syscall"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

// FailureKind says why Plan or Create gave no tree.
type FailureKind int

const (
	// NoBaseDir: no Link base directory is on the filesystem of every source
	// folder, or none could be made.
	NoBaseDir FailureKind = iota + 1
	InvalidPlan
	// NotSupported: the filesystem has no reflinks, or the question failed.
	NotSupported
	TargetExists
	// CrossDevice: every Link base directory gave a cross-device error.
	CrossDevice
	ConnectionLost
	CreateFailed
)

// Error is the error of Plan and Create. Err keeps the cause for
// errors.Is and errors.As.
type Error struct {
	Kind FailureKind
	// Partial is set when a part of the tree can remain in Dir.
	Partial bool
	Dir     string
	Err     error

	// skip lets Create try the next Link base directory.
	skip bool
}

func (f *Error) Error() string {
	var msg string
	switch f.Kind {
	case NoBaseDir:
		msg = "no suitable link base directory"
	case InvalidPlan:
		msg = "invalid link tree plan"
	case NotSupported:
		msg = "reflinks are not supported"
	case CrossDevice:
		msg = "cross-device link in every link base directory"
	case ConnectionLost:
		msg = "lost the connection to the filesystem"
	default:
		msg = "create link tree"
	}
	if f.Err != nil {
		msg += ": " + f.Err.Error()
	}
	if f.Partial {
		msg += "; a partial link tree can remain in " + f.Dir
	}
	return msg
}

func (f *Error) Unwrap() error { return f.Err }

// classify gives a create error its kind.
func classify(err error) *Error {
	kind := CreateFailed
	switch {
	case errors.Is(err, fsops.ErrConnectionLost):
		kind = ConnectionLost
	case errors.Is(err, hardlinktree.ErrTargetExists):
		kind = TargetExists
	case errors.Is(err, syscall.EXDEV) || isPlatformCrossDevice(err):
		kind = CrossDevice
	}
	return &Error{Kind: kind, Err: err}
}
