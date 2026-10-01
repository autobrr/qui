// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package remote implements fsops.Backend over SFTP, on the pooled SSH
// connection the instance's stored credentials open. Reflinks and file
// identity have no sftp operation and answer fsops.ErrUnsupported; hardlinks
// need the server to advertise hardlink@openssh.com. Remote paths are
// slash-delimited, so this package uses path and never path/filepath: the
// separator belongs to the remote host, not to ours.
package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"slices"
	"strings"
	"syscall"

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
// with. A pool error (dial failure, mismatch, instance no longer remote) is
// marked ErrConnectionLost and carries no path, so callers can tell a broken
// connection from a broken path. The pool's own sentinels stay matchable.
func (b *Backend) client(ctx context.Context) (*sftp.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client, err := b.pool.SFTP(ctx, b.inst)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, lost(err)
	}
	return client, nil
}

func (b *Backend) Stat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	fi, err := stat(ctx, client, p)
	if err != nil {
		return nil, err
	}
	return lstatInfo(fi, p), nil
}

func (b *Backend) Lstat(ctx context.Context, p string) (*fsops.LstatInfo, error) {
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	fi, err := lstat(ctx, client, p)
	if err != nil {
		return nil, err
	}
	return lstatInfo(fi, p), nil
}

func (b *Backend) ReadDir(ctx context.Context, p string) ([]fsops.DirEntry, error) {
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := readDir(ctx, client, p)
	if err != nil {
		return nil, readDirError(p, err)
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
	fi, err := lstat(ctx, client, root)
	if err != nil {
		return nil, err
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
		// The connection failed, not the directory: one Err entry carrying
		// ErrConnectionLost ends the walk rather than repeating the error for
		// every directory left, and tells the consumer the tree is cut short.
		send(ctx, ch, fsops.WalkEntry{Path: dir, IsDir: true, RelPath: rel, Err: err})
		return false
	}
	entries, err := readDir(ctx, client, dir)
	if err != nil {
		if ctx.Err() != nil {
			return false
		}
		err = readDirError(dir, err)
		if errors.Is(err, fsops.ErrConnectionLost) {
			send(ctx, ch, fsops.WalkEntry{Path: dir, IsDir: true, RelPath: rel, Err: err})
			return false
		}
		// An unreadable directory is one entry with Err and the walk goes on,
		// as it does locally: a scan must not die on one denied subtree.
		return send(ctx, ch, fsops.WalkEntry{
			Path: dir, IsDir: true,
			RelPath: rel,
			Err:     err,
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

// lostConnection tells a request that failed because the transport went away
// from one the server refused: pkg/sftp answers every request in flight with
// ErrSSHFxConnectionLost when its connection closes, and a closed socket
// surfaces as net.ErrClosed. io.EOF is not here, because pkg/sftp also returns
// it for a server's SSH_FX_EOF answer. readDirError is where it counts.
func lostConnection(err error) bool {
	return errors.Is(err, sftp.ErrSSHFxConnectionLost) || errors.Is(err, net.ErrClosed)
}

// poolSentinels are the answers sshpool gives on purpose. They say why the
// pool would not serve the read, which a caller may want to tell apart.
var poolSentinels = []error{sshpool.ErrConnect, sshpool.ErrPinUnusable, sshpool.ErrPoolClosed, sshpool.ErrNotRemote}

// connectionLostError is ErrConnectionLost plus whichever pool sentinels its cause
// carries. The rest of the cause is text only, because a redial refused with
// EACCES carries an errno that matches fs.ErrPermission, and a consumer that
// steps over denied directories would read the cut as one.
type connectionLostError struct {
	msg  string
	kept []error
}

func (e *connectionLostError) Error() string   { return e.msg }
func (e *connectionLostError) Unwrap() []error { return e.kept }

func lost(err error) error {
	kept := []error{fsops.ErrConnectionLost}
	for _, sentinel := range poolSentinels {
		if errors.Is(err, sentinel) {
			kept = append(kept, sentinel)
		}
	}
	if mismatch, ok := errors.AsType[*sshpool.MismatchError](err); ok {
		kept = append(kept, mismatch)
	}
	return &connectionLostError{msg: fsops.ErrConnectionLost.Error() + ": " + err.Error(), kept: kept}
}

// requestError is pathError for a failed sftp request: a server answer keeps
// pkg/sftp's sentinel, a dropped transport becomes ErrConnectionLost.
func requestError(op, p string, err error) error {
	if lostConnection(err) {
		err = lost(err)
	}
	return pathError(op, p, err)
}

// readDirError is requestError for a directory listing. pkg/sftp takes the
// server's SSH_FX_EOF as the end of the listing, so an io.EOF that still
// escapes it comes from a request sent on a channel that had closed, or from
// a server that answered the opendir itself with SSH_FX_EOF.
func readDirError(p string, err error) error {
	if errors.Is(err, io.EOF) {
		return pathError("readdir", p, lost(err))
	}
	return requestError("readdir", p, err)
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
		return nil, requestError(op, p, err)
	}
	return stat, nil
}

// MkdirAll ignores perm: pkg/sftp sends SSH_FXP_MKDIR with empty attrs, so
// the server's umask decides, the way it does for every file sftp creates.
func (b *Backend) MkdirAll(ctx context.Context, p string, _ fs.FileMode) error {
	client, err := b.client(ctx)
	if err != nil {
		return err
	}
	_, err = mkdirAll(ctx, client, p)
	return err
}

// mkdirAll creates p and its missing ancestors and returns the directories
// this call made, shallowest first, so a tree create can roll back exactly
// those. It is hardlinktree.MkdirAllTracked over sftp, with two differences
// the local one does not need: Stat instead of Lstat, so a symlinked ancestor
// counts as a directory the way os.MkdirAll treats it, and a file in the way
// is refused up front rather than left for the server's mkdir to refuse.
// Client.MkdirAll is not used: it reports nothing about what it made.
func mkdirAll(ctx context.Context, client *sftp.Client, p string) ([]string, error) {
	fi, err := stat(ctx, client, p)
	if err == nil {
		if fi.IsDir() {
			return nil, nil
		}
		return nil, pathError("mkdir", p, syscall.ENOTDIR)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	var created []string
	if parent := path.Dir(p); parent != p {
		if created, err = mkdirAll(ctx, client, parent); err != nil {
			return created, err
		}
	}
	if err := awaitErr(ctx, func() error { return client.Mkdir(p) }); err != nil {
		// A cancel while the request is on the wire may still land it on the
		// server, so the directory is recorded: rollback removes it only when
		// empty, so recording a directory that never appeared costs nothing.
		if ctx.Err() != nil {
			return append(created, p), err
		}
		// SSH_FX_FAILURE covers "exists" too: a concurrent attempt won the
		// race, so the directory is there but not ours to record.
		if fi, statErr := lstat(ctx, client, p); statErr == nil && fi.IsDir() {
			return created, nil
		}
		return created, requestError("mkdir", p, err)
	}
	return append(created, p), nil
}

func (b *Backend) Remove(ctx context.Context, p string, opts fsops.RemoveOptions) error {
	client, err := b.client(ctx)
	if err != nil {
		return err
	}
	fi, err := lstat(ctx, client, p)
	if err != nil {
		// Recursive mirrors os.RemoveAll, which is content with a path that
		// is already gone; plain Remove mirrors os.Remove, which is not.
		if opts.Recursive && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if opts.Recursive && fi.IsDir() {
		return removeAll(ctx, client, p)
	}
	return unlink(ctx, client, p, fi.IsDir())
}

// removeAll empties dir bottom-up and removes it. Client.RemoveAll is not
// used: it Stats the root, so a symlinked root would empty the link's target.
// Here every decision comes from lstat-shaped attrs, so a symlinked directory
// is unlinked as a plain entry and never descended, on the root or below.
//
// SFTP v3 has no openat, so a directory swapped for a symlink between the
// listing and the unlink can redirect the unlink. The re-lstat in remove
// bounds that window to one round trip; closing it needs the exec tier.
//
// ponytail: serial, one extra round trip per file on top of the listing;
// nothing reachable in this release removes a large tree, and rm -rf -- over
// exec (#2726) is the upgrade when one does.
func removeAll(ctx context.Context, client *sftp.Client, dir string) error {
	entries, err := readDir(ctx, client, dir)
	if err != nil {
		return readDirError(dir, err)
	}
	for _, fi := range entries {
		child := path.Join(dir, fi.Name())
		if fi.IsDir() {
			err = removeAll(ctx, client, child)
		} else {
			err = remove(ctx, client, child)
		}
		if err != nil {
			return err
		}
	}
	return unlink(ctx, client, dir, true)
}

// remove unlinks one non-directory entry found by a listing, re-checking with
// lstat first that it still is one: a directory swapped in under the name is
// refused rather than removed through, and an entry already gone is fine.
// Directories skip the check, since rmdir refuses anything but a directory.
func remove(ctx context.Context, client *sftp.Client, p string) error {
	fi, err := lstat(ctx, client, p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return pathError("remove", p, errors.New("entry became a directory during removal"))
	}
	return unlink(ctx, client, p, false)
}

// unlink sends the remove. Client.Remove retries a failed unlink as rmdir and
// then names the error from a link-following Stat, so a symlink to a
// directory that cannot be unlinked would read as "not found" while it still
// exists; a failure is therefore answered from our own lstat. The one case
// that fallback can still remove is an empty directory swapped in under a
// file name, which loses nothing. A directory that is not empty is reported
// as ENOTEMPTY, which SSH_FX_FAILURE does not carry and callers classify on.
func unlink(ctx context.Context, client *sftp.Client, p string, isDir bool) error {
	call := client.Remove
	if isDir {
		call = client.RemoveDirectory
	}
	err := awaitErr(ctx, func() error { return call(p) })
	if err == nil || ctx.Err() != nil {
		return err
	}
	if _, statErr := lstat(ctx, client, p); errors.Is(statErr, fs.ErrNotExist) {
		return nil
	}
	switch {
	case isDir && dirNotEmpty(ctx, client, p):
		err = syscall.ENOTEMPTY
	case errors.Is(err, fs.ErrNotExist):
		err = errors.New("server refused the remove and the entry still exists")
	}
	return requestError("remove", p, err)
}

func stat(ctx context.Context, client *sftp.Client, p string) (os.FileInfo, error) {
	fi, err := await(ctx, func() (os.FileInfo, error) { return client.Stat(p) })
	if err != nil {
		return nil, requestError("stat", p, err)
	}
	return fi, nil
}

func lstat(ctx context.Context, client *sftp.Client, p string) (os.FileInfo, error) {
	fi, err := await(ctx, func() (os.FileInfo, error) { return client.Lstat(p) })
	if err != nil {
		return nil, requestError("lstat", p, err)
	}
	return fi, nil
}

const hardlinkExtension = "hardlink@openssh.com"

// HardlinkTree mirrors hardlinktree.Create with one difference: a target that
// already exists always fails. Locally a target that is already a link to the
// source is skipped; sftp attrs carry no inode, so this backend cannot tell
// that link from a stranger's file and refuses rather than guesses. A retry
// after a crash therefore needs the partial tree removed by hand.
func (b *Backend) HardlinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	// Client.Link sends the extended packet unconditionally, so the gate is
	// here, the way statVFS gates on its extension.
	if _, ok := client.HasExtension(hardlinkExtension); !ok {
		return nil, fmt.Errorf("hardlinktree: %w: server does not advertise %s", fsops.ErrUnsupported, hardlinkExtension)
	}
	if plan == nil || plan.RootDir == "" || len(plan.Files) == 0 {
		return nil, errors.New("hardlinktree: plan is empty")
	}

	created := &fsops.TreeCreateResult{}
	// Rollback runs on a context that outlives the caller's cancel: a tree
	// half made because the job was cancelled is still ours to take back.
	fail := func(err error) (*fsops.TreeCreateResult, error) {
		if rollbackErr := b.RemoveTree(context.WithoutCancel(ctx), created); rollbackErr != nil {
			return nil, fmt.Errorf("%w (rollback also failed: %w)", err, rollbackErr)
		}
		return nil, err
	}

	// Every directory this call has seen or made, so a flat tree pays one
	// stat for its root rather than one per file.
	known := map[string]struct{}{}
	for _, fp := range plan.Files {
		dir := path.Dir(fp.TargetPath)
		if _, ok := known[dir]; !ok {
			dirs, err := mkdirAll(ctx, client, dir)
			created.Dirs = append(created.Dirs, dirs...)
			if err != nil {
				return fail(fmt.Errorf("create directory %s: %w", dir, err))
			}
			for d := dir; ; d = path.Dir(d) {
				known[d] = struct{}{}
				if d == plan.RootDir || path.Dir(d) == d {
					break
				}
			}
		}
		// Link first: an existing target makes it fail, and the lstat that
		// names the cause is paid only then.
		if err := awaitErr(ctx, func() error { return client.Link(fp.SourcePath, fp.TargetPath) }); err != nil {
			if ctx.Err() != nil {
				// The request may still land on the server; record the
				// target so rollback takes it back if it did.
				created.Files = append(created.Files, fp.TargetPath)
				return fail(err)
			}
			if _, statErr := lstat(ctx, client, fp.TargetPath); statErr == nil {
				return fail(fmt.Errorf("target already exists: %s", fp.TargetPath))
			}
			return fail(fmt.Errorf("hardlink %s -> %s: %w", fp.SourcePath, fp.TargetPath, requestError("link", fp.TargetPath, err)))
		}
		created.Files = append(created.Files, fp.TargetPath)
		created.Created++
	}
	return created, nil
}

func (b *Backend) ReflinkTree(ctx context.Context, _ *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("reflinktree: %w: sftp has no reflink operation", fsops.ErrUnsupported)
}

// RemoveTree is hardlinktree.Created.Rollback over sftp: each recorded file
// is unlinked, each recorded directory removed deepest first and only when
// empty, so a directory that gained a sibling torrent's files stays. The
// algorithm is repeated rather than shared because the local one is bound to
// os.Remove and an errno; a second copy is smaller than the seam.
func (b *Backend) RemoveTree(ctx context.Context, created *fsops.TreeCreateResult) error {
	// A nil handle is nothing to remove, per the interface.
	if created == nil {
		return ctx.Err()
	}
	client, err := b.client(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, f := range created.Files {
		if err := unlink(ctx, client, f, false); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	dirs := slices.Clone(created.Dirs)
	slices.SortFunc(dirs, func(a, b string) int { return len(b) - len(a) })
	for _, d := range dirs {
		if err := unlink(ctx, client, d, true); err != nil && firstErr == nil && !errors.Is(err, syscall.ENOTEMPTY) {
			firstErr = err
		}
	}
	return firstErr
}

// dirNotEmpty tells the rmdir failure callers tolerate from the ones they
// report: SSH_FX_FAILURE carries no errno, so the listing is the answer.
func dirNotEmpty(ctx context.Context, client *sftp.Client, dir string) bool {
	entries, err := readDir(ctx, client, dir)
	return err == nil && len(entries) > 0
}

func (b *Backend) SupportsReflink(ctx context.Context, _ string) (bool, string, error) {
	if err := ctx.Err(); err != nil {
		return false, "", err
	}
	return false, "reflinks are not available over sftp", nil
}

// readDir goes through await although ReadDirContext takes ctx: pkg/sftp
// closes the handle with context.Background() on the way out, so a server
// that never answers the readdir holds the call past ctx.
func readDir(ctx context.Context, client *sftp.Client, p string) ([]os.FileInfo, error) {
	return await(ctx, func() ([]os.FileInfo, error) { return client.ReadDirContext(ctx, p) })
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

// awaitErr is await for a call that only answers with an error.
func awaitErr(ctx context.Context, call func() error) error {
	_, err := await(ctx, func() (struct{}, error) { return struct{}{}, call() })
	return err
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

func (b *Backend) Paths() fsops.PathDialect { return fsops.SlashPaths }
