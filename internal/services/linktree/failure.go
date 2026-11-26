// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package linktree

import (
	"errors"
	"syscall"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

type FailureKind int

const (
	NoBaseDir FailureKind = iota + 1
	InvalidPlan
	NotSupported
	TargetExists
	CrossDevice
	ConnectionLost
	CreateFailed
)

type Error struct {
	Kind    FailureKind
	Partial bool
	Dir     string
	Err     error

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
