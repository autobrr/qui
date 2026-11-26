// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package remote

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/pkg/sftp"

	"github.com/autobrr/qui/internal/fsops"
)

const fsyncExtension = "fsync@openssh.com"

func (b *Backend) OpenRead(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	return b.open(ctx, p, os.O_RDONLY, offset)
}

func (b *Backend) OpenWrite(ctx context.Context, p string, offset int64) (fsops.FileWriter, error) {
	return b.open(ctx, p, os.O_WRONLY|os.O_CREATE, offset)
}

func (b *Backend) open(ctx context.Context, p string, flag int, offset int64) (*file, error) {
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	f, err := client.OpenFile(p, flag)
	if err != nil {
		return nil, requestError("open", p, err)
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, pathError("seek", p, err)
	}
	return &file{f: f, client: client, path: p}, nil
}

type file struct {
	f      *sftp.File
	client *sftp.Client
	path   string
}

func (f *file) Read(p []byte) (int, error) {
	n, err := f.f.Read(p)
	if err == nil || errors.Is(err, io.EOF) && !lostConnection(err) {
		return n, err
	}
	return n, requestError("read", f.path, err)
}

func (f *file) Write(p []byte) (int, error) {
	n, err := f.f.Write(p)
	if err != nil {
		return n, requestError("write", f.path, err)
	}
	return n, nil
}

func (f *file) Flush() (bool, error) {
	if version, ok := f.client.HasExtension(fsyncExtension); !ok || version != "1" {
		return false, nil
	}
	if err := f.f.Sync(); err != nil {
		return false, requestError("fsync", f.path, err)
	}
	return true, nil
}

func (f *file) Close() error {
	if err := f.f.Close(); err != nil {
		return requestError("close", f.path, err)
	}
	return nil
}
