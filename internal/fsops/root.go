// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import "context"

type Root interface {
	Lstat(ctx context.Context, name string) (*LstatInfo, error)
	Create(ctx context.Context, name string) (FileWriter, error)
	OpenWrite(ctx context.Context, name string, offset int64) (FileWriter, error)
	RenameNoReplace(ctx context.Context, oldName, newName string) error
	Link(ctx context.Context, oldName, newName string) error
	Remove(ctx context.Context, name string) error
	Close() error
}

type RootOpener interface {
	OpenRoot(ctx context.Context, dir string) (Root, error)
}
