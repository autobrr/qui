// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import "context"

// Root writes new files below one folder and never outside it. Names are
// slash-separated and relative to that folder. New folders get mode 775 and
// new files 664 where the filesystem allows it, so that the group of the
// user can delete them.
type Root interface {
	Lstat(ctx context.Context, name string) (*LstatInfo, error)
	// Create creates a new file and its missing folders. It never opens a
	// file that exists: the error then wraps fs.ErrExist.
	Create(ctx context.Context, name string) (FileWriter, error)
	// OpenWrite opens a file that exists for writing from offset on. It
	// never creates or truncates a file: a missing file is an error that
	// wraps fs.ErrNotExist.
	OpenWrite(ctx context.Context, name string, offset int64) (FileWriter, error)
	// RenameNoReplace gives a file a new name. It never replaces a file:
	// when newName exists, the error wraps fs.ErrExist.
	RenameNoReplace(ctx context.Context, oldName, newName string) error
	// Link makes newName a hardlink to the file oldName, and the missing
	// folders of newName. It never replaces a file. The error differs between
	// filesystems, and a filesystem without hardlinks always fails.
	Link(ctx context.Context, oldName, newName string) error
	Remove(ctx context.Context, name string) error
	Close() error
}

type RootOpener interface {
	OpenRoot(ctx context.Context, dir string) (Root, error)
}
