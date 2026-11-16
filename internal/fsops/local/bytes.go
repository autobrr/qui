// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package local

import (
	"context"
	"io"
	"os"

	"github.com/autobrr/qui/internal/fsops"
)

func (b *Backend) OpenRead(ctx context.Context, path string, offset int64) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return openAt(path, os.O_RDONLY, offset)
}

func (b *Backend) OpenWrite(ctx context.Context, path string, offset int64) (fsops.FileWriter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := openAt(path, os.O_WRONLY|os.O_CREATE, offset)
	if err != nil {
		return nil, err
	}
	return writer{f}, nil
}

func openAt(path string, flag int, offset int64) (*os.File, error) {
	f, err := os.OpenFile(path, flag, 0o666)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

type writer struct{ *os.File }

func (w writer) Flush() (bool, error) {
	if err := w.Sync(); err != nil {
		return false, err
	}
	return true, nil
}
