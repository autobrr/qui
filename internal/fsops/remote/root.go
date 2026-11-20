// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package remote

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/pkg/sftp"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/sshpool"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

const (
	rootDirMode  fs.FileMode = 0o775
	rootFileMode fs.FileMode = 0o664
)

var _ fsops.RootOpener = (*Backend)(nil)

// OpenRoot opens dir as a Root over SFTP. SFTP has no openat, so each call
// checks its name with the path rules of the dialect, and refuses a name
// with a folder below dir that is a symlink or a file. A swap on the server
// between that check and the request stays possible.
func (b *Backend) OpenRoot(ctx context.Context, dir string) (fsops.Root, error) {
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	dir = path.Clean(dir)
	if err := mkdirs(ctx, client, dir); err != nil {
		return nil, err
	}
	return &root{b: b, dir: dir}, nil
}

func mkdirs(ctx context.Context, client *sftp.Client, dir string) error {
	made, err := mkdirAll(ctx, client, dir)
	for _, d := range made {
		_ = awaitMutation(ctx, func() error { return client.Chmod(d, rootDirMode) })
	}
	return err
}

type root struct {
	b   *Backend
	dir string
}

func (r *root) open(ctx context.Context, op, name string) (*sftp.Client, string, error) {
	if err := hardlinktree.ValidateRelativePath(fsops.SlashPaths, name); err != nil {
		return nil, "", pathError(op, name, err)
	}
	client, err := r.b.client(ctx)
	if err != nil {
		return nil, "", err
	}
	folder := r.dir
	for part := range strings.SplitSeq(path.Dir(name), "/") {
		if part == "." {
			break
		}
		folder = path.Join(folder, part)
		fi, err := lstat(ctx, client, folder)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return nil, "", err
		}
		if !fi.IsDir() {
			return nil, "", pathError(op, folder, errors.New("not a folder"))
		}
	}
	return client, path.Join(r.dir, name), nil
}

func (r *root) Lstat(ctx context.Context, name string) (*fsops.LstatInfo, error) {
	client, p, err := r.open(ctx, "lstat", name)
	if err != nil {
		return nil, err
	}
	fi, err := lstat(ctx, client, p)
	if err != nil {
		return nil, err
	}
	info := &fsops.LstatInfo{FileInfo: fileInfo(fi, p)}
	r.b.setIdentity(ctx, info, false)
	return info, nil
}

func (r *root) Create(ctx context.Context, name string) (fsops.FileWriter, error) {
	client, p, err := r.open(ctx, "create", name)
	if err != nil {
		return nil, err
	}
	if err := mkdirs(ctx, client, path.Dir(p)); err != nil {
		return nil, err
	}
	f, err := client.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		if _, statErr := lstat(ctx, client, p); statErr == nil {
			return nil, pathError("create", p, fs.ErrExist)
		}
		return nil, requestError("create", p, err)
	}
	_ = f.Chmod(rootFileMode)
	return &file{f: f, client: client, path: p}, nil
}

func (r *root) OpenWrite(ctx context.Context, name string, offset int64) (fsops.FileWriter, error) {
	client, p, err := r.open(ctx, "open", name)
	if err != nil {
		return nil, err
	}
	fi, err := lstat(ctx, client, p)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, pathError("open", p, errors.New("not a regular file"))
	}
	return r.b.open(ctx, p, os.O_WRONLY, offset)
}

// RenameNoReplace never trusts the plain SFTP rename to refuse a name that
// exists: SFTPGo 2.7.6 replaces the file. With hardlink@openssh.com it links
// and removes the old name. Without it, or when the link fails, it renames
// only when an lstat finds newName free.
func (r *root) RenameNoReplace(ctx context.Context, oldName, newName string) error {
	client, oldPath, err := r.open(ctx, "rename", oldName)
	if err != nil {
		return err
	}
	_, newPath, err := r.open(ctx, "rename", newName)
	if err != nil {
		return err
	}
	if _, ok := client.HasExtension(sshpool.HardlinkExtension); ok {
		if err := awaitMutation(ctx, func() error { return client.Link(oldPath, newPath) }); err == nil {
			return unlink(ctx, client, oldPath, false)
		}
	}
	if _, err := lstat(ctx, client, newPath); err == nil {
		return pathError("rename", newPath, fs.ErrExist)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := awaitMutation(ctx, func() error { return client.Rename(oldPath, newPath) }); err != nil {
		if _, statErr := lstat(ctx, client, newPath); statErr == nil {
			return pathError("rename", newPath, fs.ErrExist)
		}
		return requestError("rename", newPath, err)
	}
	return nil
}

func (r *root) Link(ctx context.Context, oldName, newName string) error {
	client, oldPath, err := r.open(ctx, "link", oldName)
	if err != nil {
		return err
	}
	_, newPath, err := r.open(ctx, "link", newName)
	if err != nil {
		return err
	}
	// Client.Link sends the extended packet unconditionally.
	if _, ok := client.HasExtension(sshpool.HardlinkExtension); !ok {
		return pathError("link", newPath, fmt.Errorf("%w: server does not advertise %s", fsops.ErrUnsupported, sshpool.HardlinkExtension))
	}
	if err := mkdirs(ctx, client, path.Dir(newPath)); err != nil {
		return err
	}
	if err := awaitMutation(ctx, func() error { return client.Link(oldPath, newPath) }); err != nil {
		if _, statErr := lstat(ctx, client, newPath); statErr == nil {
			return pathError("link", newPath, fs.ErrExist)
		}
		return requestError("link", newPath, err)
	}
	return nil
}

func (r *root) Remove(ctx context.Context, name string) error {
	client, p, err := r.open(ctx, "remove", name)
	if err != nil {
		return err
	}
	fi, err := lstat(ctx, client, p)
	if err != nil {
		return err
	}
	return unlink(ctx, client, p, fi.IsDir())
}

// Close holds nothing: the pool owns the connection.
func (r *root) Close() error { return nil }
