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
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"slices"
	"strings"
	"sync"

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
	fi, err := await(ctx, func() (os.FileInfo, error) { return client.Stat(p) })
	if err != nil {
		return nil, readError("stat", p, err)
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
		return nil, readError("lstat", p, err)
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
	fi, err := await(ctx, func() (os.FileInfo, error) { return client.Lstat(root) })
	if err != nil {
		return nil, readError("lstat", root, err)
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
		b.walk(ctx, ch, root, opts)
	}()
	return ch, nil
}

// walkWorkers is how many directories one walk lists at a time over its sftp
// session, which pipelines the requests.
//
// ponytail: a package constant; a WalkOptions field is the upgrade if a host
// ever needs tuning.
const walkWorkers = 8

type walkJob struct{ dir, rel string }

// walk lists the tree under root with walkWorkers workers draining one
// directory queue: a worker lists a directory, emits its entries in lexical
// order, enqueues its subdirectories and takes the next. Each directory costs
// opendir, readdir, the readdir that answers EOF and close, a round trip
// apiece, so wall clock falls by the worker count. The order that survives is
// the one fsops.Backend promises: lexical within a directory, and a
// directory's own entry before anything beneath it.
//
// EmitStatErrors needs no handling here: readdir carries the attrs, so there
// is no per-entry stat left to fail.
func (b *Backend) walk(ctx context.Context, ch chan<- fsops.WalkEntry, root string, opts fsops.WalkOptions) {
	w := &walker{b: b, ch: ch, opts: opts, queue: make(chan walkJob, walkWorkers)}
	w.ctx, w.cancel = context.WithCancel(ctx)
	defer w.cancel()

	w.enqueue(walkJob{dir: root, rel: "."})
	go func() {
		w.pending.Wait()
		close(w.queue)
	}()
	var workers sync.WaitGroup
	for range walkWorkers {
		workers.Go(w.work)
	}
	workers.Wait()

	// The connection failed, not a directory: one Err entry carrying
	// ErrConnectionLost ends the walk rather than repeating the error for
	// every directory left, and tells the consumer the tree is cut short. It
	// is sent after every worker has stopped so nothing follows it.
	if w.cut != nil {
		send(ctx, ch, *w.cut)
	}
}

type walker struct {
	b      *Backend
	ch     chan<- fsops.WalkEntry
	opts   fsops.WalkOptions
	ctx    context.Context
	cancel context.CancelFunc

	queue   chan walkJob
	pending sync.WaitGroup // directories queued or being listed

	once sync.Once
	cut  *fsops.WalkEntry
}

// enqueue never blocks the caller: a worker that waited on a full queue while
// every other worker did the same would deadlock the walk.
func (w *walker) enqueue(job walkJob) {
	w.pending.Add(1)
	go func() {
		select {
		case w.queue <- job:
		case <-w.ctx.Done():
			w.pending.Done()
		}
	}()
}

func (w *walker) work() {
	for {
		select {
		case job, ok := <-w.queue:
			if !ok {
				return
			}
			w.list(job)
			w.pending.Done()
		case <-w.ctx.Done():
			return
		}
	}
}

func (w *walker) list(job walkJob) {
	ctx, opts := w.ctx, w.opts
	// The client is taken from the pool per directory, not once per walk: each
	// take marks the connection used, so a walk longer than the idle limit is
	// not closed underneath itself, and a reconnect mid-walk is picked up.
	client, err := w.b.client(ctx)
	var entries []os.FileInfo
	if err == nil {
		entries, err = readDir(ctx, client, job.dir)
		if err != nil {
			err = readDirError(job.dir, err)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		entry := fsops.WalkEntry{Path: job.dir, IsDir: true, RelPath: job.rel, Err: err}
		if errors.Is(err, fsops.ErrConnectionLost) {
			w.once.Do(func() {
				w.cut = &entry
				w.cancel()
			})
			return
		}
		// An unreadable directory is one entry with Err and the walk goes on,
		// as it does locally: a scan must not die on one denied subtree.
		send(ctx, w.ch, entry)
		return
	}

	// Local walks are lexical; sftp hands back whatever order the server used.
	slices.SortFunc(entries, func(a, b os.FileInfo) int { return strings.Compare(a.Name(), b.Name()) })

	for _, fi := range entries {
		name := fi.Name()
		childPath := path.Join(job.dir, name)
		if (opts.SkipHidden && strings.HasPrefix(name, ".")) ||
			(fi.IsDir() && ignoredDirName(name, opts)) ||
			slices.Contains(opts.IgnorePaths, childPath) {
			continue
		}
		childRel := path.Join(job.rel, name)
		if !send(ctx, w.ch, walkEntry(fi, childPath, childRel, opts.WantFileID)) {
			return
		}
		// readdir attrs are lstat-style, so a symlinked directory reports
		// IsDir false and is never descended.
		if fi.IsDir() {
			w.enqueue(walkJob{dir: childPath, rel: childRel})
		}
	}
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

// readError is pathError for a failed sftp request: a server answer keeps
// pkg/sftp's sentinel, a dropped transport becomes ErrConnectionLost.
func readError(op, p string, err error) error {
	if lostConnection(err) {
		err = lost(err)
	}
	return pathError(op, p, err)
}

// readDirError is readError for a directory listing. pkg/sftp takes the
// server's SSH_FX_EOF as the end of the listing, so an io.EOF that still
// escapes it comes from a request sent on a channel that had closed, or from
// a server that answered the opendir itself with SSH_FX_EOF.
func readDirError(p string, err error) error {
	if errors.Is(err, io.EOF) {
		return pathError("readdir", p, lost(err))
	}
	return readError("readdir", p, err)
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
		return nil, readError(op, p, err)
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
