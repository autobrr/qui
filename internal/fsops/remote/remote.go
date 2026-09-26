// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package remote implements the read half of fsops.Backend over SFTP, on the
// pooled SSH connection the instance's stored credentials open. Write and
// tree operations refuse with fsops.ErrUnsupported in this release. Remote
// paths are slash-delimited, so this package uses path and never
// path/filepath: the separator belongs to the remote host, not to ours.
package remote

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/pkg/sftp"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/sshpool"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

// Backend implements fsops.Backend for one instance over SFTP. It holds no
// connection of its own: the pool owns those, so a backend is cheap to build
// per resolution and safe to discard.
type Backend struct {
	pool *sshpool.Pool
	inst *models.Instance
}

func New(pool *sshpool.Pool, inst *models.Instance) *Backend {
	return &Backend{pool: pool, inst: inst}
}

var _ fsops.Backend = (*Backend)(nil)

// errNoIdentity is what sftp cannot answer: its attrs carry no device or inode
// number, so hardlink identity degrades to zero FileID plus this error, which
// every consumer already reads as "no identity".
var errNoIdentity = errors.New("file identity is not available over sftp")

const statvfsExtension = "statvfs@openssh.com"

// client is the context check plus the pooled connection every method opens
// with. A pool error (unpinned host, dial failure, mismatch) is returned as-is
// so callers can tell a broken connection from a broken path.
func (b *Backend) client(ctx context.Context) (*sftp.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return b.pool.SFTP(ctx, b.inst)
}

func (b *Backend) Stat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	fi, err := await(ctx, func() (os.FileInfo, error) { return client.Stat(p) })
	if err != nil {
		return nil, pathError("stat", p, err)
	}
	return lstatInfo(fi, p), nil
}

func (b *Backend) Lstat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	fi, err := await(ctx, func() (os.FileInfo, error) { return client.Lstat(p) })
	if err != nil {
		return nil, pathError("lstat", p, err)
	}
	return lstatInfo(fi, p), nil
}

func (b *Backend) ReadDir(ctx context.Context, p string) ([]fsops.DirEntry, error) {
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := client.ReadDirContext(ctx, p)
	if err != nil {
		return nil, pathError("readdir", p, err)
	}

	result := make([]fsops.DirEntry, 0, len(entries))
	for _, fi := range entries {
		result = append(result, fsops.DirEntry{
			Name:      fi.Name(),
			IsDir:     fi.IsDir(),
			IsSymlink: fi.Mode()&fs.ModeSymlink != 0,
		})
	}
	return result, nil
}

func (b *Backend) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	// A missing root errors before the goroutine, so the caller sees
	// fs.ErrNotExist from the call rather than as a lone channel entry.
	fi, err := await(ctx, func() (os.FileInfo, error) { return client.Lstat(root) })
	if err != nil {
		return nil, pathError("lstat", root, err)
	}

	ch := make(chan fsops.WalkEntry, 64)
	go func() {
		defer close(ch)
		// The root is subject to IgnorePaths like every other entry, as it is
		// locally: an ignored root yields an empty walk, not a lone root entry.
		if slices.Contains(opts.IgnorePaths, root) {
			return
		}
		if !send(ctx, ch, walkEntry(fi, root, ".", opts.WantFileID)) || !fi.IsDir() {
			return
		}
		b.walk(ctx, ch, root, ".", opts)
	}()
	return ch, nil
}

// walk lists dir and recurses into its subdirectories. EmitStatErrors needs no
// handling here: readdir carries the attrs, so there is no per-entry stat left
// to fail.
//
// ponytail: four round trips per directory (opendir, readdir, the readdir
// that answers EOF, close), walked serially; a bounded walk over sibling
// directories is the lever on a key that forbids exec, and the exec-tier find
// sweep is the upgrade when a deep tree makes the latency hurt.
func (b *Backend) walk(ctx context.Context, ch chan<- fsops.WalkEntry, dir, rel string, opts fsops.WalkOptions) bool {
	// The client is taken from the pool per directory, not once per walk: each
	// take marks the connection used, so a walk longer than the idle limit is
	// not closed underneath itself, and a reconnect mid-walk is picked up.
	client, err := b.client(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return false
		}
		// The connection failed, not the directory: one Err entry ends the
		// walk rather than repeating it for every directory left.
		send(ctx, ch, fsops.WalkEntry{Path: dir, IsDir: true, RelPath: rel, Err: err})
		return false
	}
	entries, err := client.ReadDirContext(ctx, dir)
	if err != nil {
		if ctx.Err() != nil {
			return false
		}
		// An unreadable directory is one entry with Err and the walk goes on,
		// as it does locally: a scan must not die on one denied subtree.
		return send(ctx, ch, fsops.WalkEntry{
			Path: dir, IsDir: true,
			RelPath: rel,
			Err:     pathError("readdir", dir, err),
		})
	}

	// Local walks are lexical; sftp hands back whatever order the server used.
	slices.SortFunc(entries, func(a, b os.FileInfo) int { return strings.Compare(a.Name(), b.Name()) })

	for _, fi := range entries {
		name := fi.Name()
		childPath := path.Join(dir, name)

		if (opts.SkipHidden && strings.HasPrefix(name, ".")) ||
			(fi.IsDir() && ignoredDirName(name, opts)) ||
			slices.Contains(opts.IgnorePaths, childPath) {
			continue
		}

		childRel := path.Join(rel, name)
		if !send(ctx, ch, walkEntry(fi, childPath, childRel, opts.WantFileID)) {
			return false
		}

		// readdir attrs are lstat-style, so a symlinked directory reports
		// IsDir false and is never descended.
		if fi.IsDir() && !b.walk(ctx, ch, childPath, childRel, opts) {
			return false
		}
	}
	return true
}

// ignoredDirName matches case-insensitively: these are OS/NAS metadata dirs
// ($RECYCLE.BIN, @eaDir) whose on-disk case varies.
func ignoredDirName(name string, opts fsops.WalkOptions) bool {
	return slices.ContainsFunc(opts.IgnoreDirNames, func(ignored string) bool {
		return strings.EqualFold(ignored, name)
	}) || slices.ContainsFunc(opts.IgnoreDirNamePrefixes, func(prefix string) bool {
		return len(name) >= len(prefix) && strings.EqualFold(name[:len(prefix)], prefix)
	})
}

func send(ctx context.Context, ch chan<- fsops.WalkEntry, entry fsops.WalkEntry) bool {
	select {
	case ch <- entry:
		return true
	case <-ctx.Done():
		return false
	}
}

func (b *Backend) Statfs(ctx context.Context, p string) (*fsops.StatfsResult, error) {
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	stat, err := statVFS(ctx, client, "statfs", p)
	if err != nil {
		return nil, err
	}
	return statfsResult(stat), nil
}

// statfsResult reports Bavail, not Bfree: the root reserve is not space this
// user can fill, and the local backend reports the same figure.
func statfsResult(stat *sftp.StatVFS) *fsops.StatfsResult {
	return &fsops.StatfsResult{
		BytesAvailable: int64(stat.Frsize * stat.Bavail),
		BytesTotal:     int64(stat.TotalSpace()),
	}
}

func (b *Backend) SameFilesystem(ctx context.Context, p1, p2 string) (bool, error) {
	client, err := b.client(ctx)
	if err != nil {
		return false, err
	}
	first, err := statVFS(ctx, client, "samefilesystem", p1)
	if err != nil {
		return false, err
	}
	second, err := statVFS(ctx, client, "samefilesystem", p2)
	if err != nil {
		return false, err
	}
	return sameFsid(first, second)
}

// sameFsid answers the hardlink question from two statvfs replies. A zero fsid
// is not an answer: callers read an error as "do not hardlink", which is the
// safe half, while a false from two zeros would be a guess.
func sameFsid(first, second *sftp.StatVFS) (bool, error) {
	if first.Fsid == 0 || second.Fsid == 0 {
		return false, fmt.Errorf("samefilesystem: %w: server reports no filesystem id", fsops.ErrUnsupported)
	}
	return first.Fsid == second.Fsid, nil
}

// statVFS gates the call on the extension the server advertises, so an
// unsupported host is named as such instead of failing as a protocol error.
func statVFS(ctx context.Context, client *sftp.Client, op, p string) (*sftp.StatVFS, error) {
	if _, ok := client.HasExtension(statvfsExtension); !ok {
		return nil, fmt.Errorf("%s %s: %w: server does not advertise %s", op, p, fsops.ErrUnsupported, statvfsExtension)
	}
	stat, err := await(ctx, func() (*sftp.StatVFS, error) { return client.StatVFS(p) })
	if err != nil {
		return nil, pathError(op, p, err)
	}
	return stat, nil
}

func (b *Backend) MkdirAll(ctx context.Context, _ string, _ fs.FileMode) error {
	return readOnly(ctx, "mkdirall")
}

func (b *Backend) Remove(ctx context.Context, _ string, _ fsops.RemoveOptions) error {
	return readOnly(ctx, "remove")
}

func (b *Backend) HardlinkTree(ctx context.Context, _ *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	return nil, readOnly(ctx, "hardlinktree")
}

func (b *Backend) ReflinkTree(ctx context.Context, _ *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	return nil, readOnly(ctx, "reflinktree")
}

func (b *Backend) RemoveTree(ctx context.Context, created *fsops.TreeCreateResult) error {
	// A nil handle is nothing to remove, per the interface.
	if created == nil {
		return ctx.Err()
	}
	return readOnly(ctx, "removetree")
}

func (b *Backend) SupportsReflink(ctx context.Context, _ string) (bool, string, error) {
	if err := ctx.Err(); err != nil {
		return false, "", err
	}
	return false, "reflinks are not available over sftp", nil
}

// readOnly is every mutating method's answer while this release ships reads
// only; the op is named because the message reaches the user through a failed
// job.
func readOnly(ctx context.Context, op string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%s: %w: sftp backend is read-only in this release", op, fsops.ErrUnsupported)
}

// await runs an sftp call that takes no context and returns as soon as ctx is
// done. The abandoned call finishes on its own: the connection is shared with
// every other caller of this instance, so one caller giving up must not close
// it the way the one-shot probe does.
func await[T any](ctx context.Context, call func() (T, error)) (T, error) {
	type outcome struct {
		value T
		err   error
	}

	done := make(chan outcome, 1)
	go func() {
		value, err := call()
		done <- outcome{value: value, err: err}
	}()

	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case result := <-done:
		return result.value, result.err
	}
}

// pathError re-attaches the path pkg/sftp drops: it normalises status errors to
// bare os.ErrNotExist / os.ErrPermission, and the Backend contract promises
// both the sentinel and the path the local backend's *fs.PathError carries.
func pathError(op, p string, err error) error {
	return &fs.PathError{Op: op, Path: p, Err: err}
}

func fileInfo(fi os.FileInfo, p string) fsops.FileInfo {
	return fsops.FileInfo{
		Path:      p,
		Size:      fi.Size(),
		ModTime:   fi.ModTime(),
		IsDir:     fi.IsDir(),
		IsSymlink: fi.Mode()&fs.ModeSymlink != 0,
		Mode:      fi.Mode(),
	}
}

func lstatInfo(fi os.FileInfo, p string) *fsops.LstatInfo {
	info := &fsops.LstatInfo{FileInfo: fileInfo(fi, p)}
	if fi.Mode().IsRegular() || fi.IsDir() {
		info.FileIDErr = errNoIdentity
	}
	return info
}

func walkEntry(fi os.FileInfo, p, rel string, wantFileID bool) fsops.WalkEntry {
	entry := fsops.WalkEntry{
		FileInfo: fileInfo(fi, p),
		RelPath:  rel,
	}
	if wantFileID && fi.Mode().IsRegular() {
		entry.FileIDErr = errNoIdentity
	}
	return entry
}
