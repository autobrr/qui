// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

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
	"syscall"

	"github.com/pkg/sftp"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/sshpool"
	"github.com/autobrr/qui/pkg/hardlink"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

type Backend struct {
	pool *sshpool.Pool
	inst *models.Instance
	conn *sftp.Client
}

func New(pool *sshpool.Pool, inst *models.Instance) *Backend {
	return &Backend{pool: pool, inst: inst}
}

var (
	_ fsops.Backend     = (*Backend)(nil)
	_ fsops.BatchStater = (*Backend)(nil)
	_ fsops.Connector   = (*Backend)(nil)
)

var errNoIdentity = errors.New("file identity is not available over sftp")

const statvfsExtension = "statvfs@openssh.com"

func (b *Backend) client(ctx context.Context) (*sftp.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.conn != nil {
		return b.conn, nil
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

func (b *Backend) Connections(ctx context.Context, n int) ([]fsops.Backend, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clients, err := b.pool.SFTPConns(ctx, b.inst, n)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, lost(err)
	}
	out := make([]fsops.Backend, len(clients))
	for i, client := range clients {
		out[i] = &Backend{pool: b.pool, inst: b.inst, conn: client}
	}
	return out, nil
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
	info := &fsops.LstatInfo{FileInfo: fileInfo(fi, p)}
	b.setIdentity(ctx, info, true)
	return info, nil
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
	info := &fsops.LstatInfo{FileInfo: fileInfo(fi, p)}
	b.setIdentity(ctx, info, false)
	return info, nil
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
			Mode:      fi.Mode().Type(),
		})
	}
	return result, nil
}

func (b *Backend) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	rootStat := lstat
	if opts.FollowRoot {
		rootStat = stat
	}
	fi, err := rootStat(ctx, client, root)
	if err != nil {
		return nil, err
	}

	ch := make(chan fsops.WalkEntry, 64)
	go func() {
		defer close(ch)
		if opts.Skip(path.Base(root), root, fi.IsDir(), true) {
			return
		}
		entry := walkEntry(fi, root, ".", opts.WantFileID)
		if opts.WantFileID && fi.Mode().IsRegular() {
			b.setIdentity(ctx, &entry.LstatInfo, false)
		}
		if !send(ctx, ch, entry) || !fi.IsDir() {
			return
		}
		if b.execTier() && b.walkExec(ctx, ch, root, opts) {
			return
		}
		b.walk(ctx, ch, root, opts)
	}()
	return ch, nil
}

const walkWorkers = 8

type walkJob struct{ dir, rel string }

func (b *Backend) walk(ctx context.Context, ch chan<- fsops.WalkEntry, root string, opts fsops.WalkOptions) {
	w := &walker{b: b, ch: ch, opts: opts}
	w.ctx, w.cancel = context.WithCancel(ctx)
	defer w.cancel()
	w.cond.L = &w.mu
	stop := context.AfterFunc(w.ctx, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.cond.Broadcast()
	})
	defer stop()

	w.enqueue(walkJob{dir: root, rel: "."})
	var workers sync.WaitGroup
	for range walkWorkers {
		workers.Go(w.work)
	}
	workers.Wait()

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

	mu      sync.Mutex
	cond    sync.Cond
	jobs    []walkJob
	pending int

	once sync.Once
	cut  *fsops.WalkEntry
}

func (w *walker) enqueue(job walkJob) {
	w.mu.Lock()
	w.jobs = append(w.jobs, job)
	w.pending++
	w.mu.Unlock()
	w.cond.Signal()
}

func (w *walker) next() (walkJob, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for len(w.jobs) == 0 && w.pending > 0 && w.ctx.Err() == nil {
		w.cond.Wait()
	}
	if len(w.jobs) == 0 || w.ctx.Err() != nil {
		return walkJob{}, false
	}
	job := w.jobs[0]
	w.jobs = w.jobs[1:]
	return job, true
}

func (w *walker) work() {
	for {
		job, ok := w.next()
		if !ok {
			return
		}
		w.list(job)
		w.mu.Lock()
		w.pending--
		w.mu.Unlock()
		w.cond.Broadcast()
	}
}

func (w *walker) list(job walkJob) {
	ctx, opts := w.ctx, w.opts
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
		send(ctx, w.ch, entry)
		return
	}

	slices.SortFunc(entries, func(a, b os.FileInfo) int { return strings.Compare(a.Name(), b.Name()) })

	for _, fi := range entries {
		name := fi.Name()
		childPath := path.Join(job.dir, name)
		if opts.Skip(name, childPath, fi.IsDir(), false) {
			continue
		}
		childRel := path.Join(job.rel, name)
		if !send(ctx, w.ch, walkEntry(fi, childPath, childRel, opts.WantFileID)) {
			return
		}
		if fi.IsDir() {
			w.enqueue(walkJob{dir: childPath, rel: childRel})
		}
	}
}

func lostConnection(err error) bool {
	if errors.Is(err, sftp.ErrSSHFxConnectionLost) || errors.Is(err, net.ErrClosed) {
		return true
	}
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return true
	}
	return errors.Is(err, io.EOF) && errors.Unwrap(err) != nil
}

var poolSentinels = []error{sshpool.ErrConnect, sshpool.ErrPinUnusable, sshpool.ErrPoolClosed, sshpool.ErrNotRemote}

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

func requestError(op, p string, err error) error {
	if lostConnection(err) {
		err = lost(err)
	}
	return pathError(op, p, err)
}

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

func statfsResult(stat *sftp.StatVFS) *fsops.StatfsResult {
	return &fsops.StatfsResult{
		BytesAvailable: int64(stat.Frsize * stat.Bavail),
		BytesTotal:     int64(stat.TotalSpace()),
	}
}

func (b *Backend) SameFilesystem(ctx context.Context, p1, p2 string) (bool, error) {
	if b.execTier() {
		return b.sameFilesystemExec(ctx, p1, p2)
	}
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

func sameFsid(first, second *sftp.StatVFS) (bool, error) {
	if first.Fsid == 0 || second.Fsid == 0 {
		return false, fmt.Errorf("samefilesystem: %w: server reports no filesystem id", fsops.ErrUnsupported)
	}
	return first.Fsid == second.Fsid, nil
}

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

func (b *Backend) MkdirAll(ctx context.Context, p string, _ fs.FileMode) error {
	client, err := b.client(ctx)
	if err != nil {
		return err
	}
	_, err = mkdirAll(ctx, client, p)
	return err
}

func mkdirAll(ctx context.Context, client *sftp.Client, p string) ([]string, error) {
	fi, err := stat(ctx, client, p)
	if err == nil {
		if fi.IsDir() {
			return nil, nil
		}
		return nil, pathError("mkdir", p, syscall.ENOTDIR)
	}
	created := make([]string, 0, 1)
	if parent := path.Dir(p); parent != p {
		if created, err = mkdirAll(ctx, client, parent); err != nil {
			return created, err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err := awaitMutation(ctx, func() error { return client.Mkdir(p) }); err != nil {
		if fi, statErr := lstat(ctx, client, p); statErr == nil && fi.IsDir() {
			return created, nil
		}
		return created, requestError("mkdir", p, err)
	}
	return append(created, p), ctx.Err()
}

func (b *Backend) Mkdir(ctx context.Context, p string, _ fs.FileMode) error {
	client, err := b.client(ctx)
	if err != nil {
		return err
	}
	if err := awaitMutation(ctx, func() error { return client.Mkdir(p) }); err != nil {
		if _, statErr := lstat(ctx, client, p); statErr == nil {
			return pathError("mkdir", p, fs.ErrExist)
		}
		return requestError("mkdir", p, err)
	}
	return nil
}

func (b *Backend) Remove(ctx context.Context, p string, opts fsops.RemoveOptions) error {
	client, err := b.client(ctx)
	if err != nil {
		return err
	}
	fi, err := lstat(ctx, client, p)
	if err != nil {
		if opts.Recursive && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if opts.Recursive && fi.IsDir() {
		return removeDir(ctx, client, p)
	}
	return unlink(ctx, client, p, fi.IsDir())
}

func removeDir(ctx context.Context, client *sftp.Client, dir string) error {
	entries, err := readDir(ctx, client, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return readDirError(dir, err)
	}
	for _, fi := range entries {
		if err := removeListed(ctx, client, path.Join(dir, fi.Name()), fi.IsDir()); err != nil {
			return err
		}
	}
	return unlink(ctx, client, dir, true)
}

func removeListed(ctx context.Context, client *sftp.Client, child string, wasDir bool) error {
	fi, err := lstat(ctx, client, child)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	switch {
	case fi.IsDir() && wasDir:
		return removeDir(ctx, client, child)
	case fi.IsDir():
		return pathError("remove", child, errors.New("entry became a directory during removal"))
	}
	return unlink(ctx, client, child, false)
}

func unlink(ctx context.Context, client *sftp.Client, p string, isDir bool) error {
	call := client.Remove
	if isDir {
		call = client.RemoveDirectory
	}
	err := awaitMutation(ctx, func() error { return call(p) })
	if err == nil {
		return ctx.Err()
	}
	if ctx.Err() != nil {
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

func (b *Backend) HardlinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	client, err := b.client(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := client.HasExtension(sshpool.HardlinkExtension); !ok {
		return nil, fmt.Errorf("hardlinktree: %w: server does not advertise %s", fsops.ErrUnsupported, sshpool.HardlinkExtension)
	}
	if plan == nil || plan.RootDir == "" || len(plan.Files) == 0 {
		return nil, errors.New("hardlinktree: plan is empty")
	}

	created := &fsops.TreeCreateResult{}
	fail := func(err error) (*fsops.TreeCreateResult, error) {
		if rollbackErr := b.RemoveTree(context.WithoutCancel(ctx), created); rollbackErr != nil {
			return nil, fmt.Errorf("%w (%w: %w)", err, hardlinktree.ErrRollbackFailed, rollbackErr)
		}
		return nil, err
	}

	known := map[string]struct{}{}
	for _, fp := range plan.Files {
		dir := path.Dir(fp.TargetPath)
		if _, ok := known[dir]; !ok {
			dirs, err := mkdirAll(ctx, client, dir)
			created.Dirs = append(created.Dirs, dirs...)
			if err != nil {
				return fail(fmt.Errorf("create directory %s: %w", dir, err))
			}
			known[dir] = struct{}{}
		}
		if err := awaitMutation(ctx, func() error { return client.Link(fp.SourcePath, fp.TargetPath) }); err != nil {
			if _, statErr := lstat(ctx, client, fp.TargetPath); statErr != nil {
				if !errors.Is(statErr, fs.ErrNotExist) {
					return fail(statErr)
				}
				return fail(fmt.Errorf("hardlink %s -> %s: %w", fp.SourcePath, fp.TargetPath, requestError("link", fp.TargetPath, err)))
			}
			same, err := b.targetIsSource(ctx, fp)
			if err != nil {
				return fail(err)
			}
			if !same {
				return fail(fmt.Errorf("%w: %s", hardlinktree.ErrTargetExists, fp.TargetPath))
			}
			created.SkippedExists++
		} else {
			created.Files = append(created.Files, fp.TargetPath)
			created.Created++
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
	}
	return created, nil
}

func (b *Backend) targetIsSource(ctx context.Context, fp hardlinktree.FilePlan) (bool, error) {
	conn, err := b.exec(ctx)
	if err != nil {
		return false, transient(ctx, err)
	}
	ids, types, err := identities(ctx, conn, fp.TargetPath, false, fp.TargetPath, fp.SourcePath)
	if err != nil {
		return false, transient(ctx, err)
	}
	if fsops.SameFile(ids[0], b.inst, ids[1], b.inst) {
		return true, nil
	}
	if types[1] != "symbolic link" {
		return false, nil
	}
	followed, _, err := identities(ctx, conn, fp.TargetPath, true, fp.SourcePath)
	if err != nil {
		return false, transient(ctx, err)
	}
	return fsops.SameFile(ids[0], b.inst, followed[0], b.inst), nil
}

func transient(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, fsops.ErrConnectionLost) {
		return err
	}
	return nil
}

func identities(ctx context.Context, conn *sshpool.Conn, errPath string, follow bool, paths ...string) ([]hardlink.FileID, []string, error) {
	options := []string{"--printf", `%d %i %h %F\0`}
	if follow {
		options = append([]string{"-L"}, options...)
	}
	out, err := output(ctx, conn, sshpool.Command{Name: "stat", Options: options, Paths: paths})
	if err != nil {
		return nil, nil, pathError("stat", errPath, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(lines) != len(paths) {
		return nil, nil, pathError("stat", errPath, fmt.Errorf("unexpected stat output %q", out))
	}
	ids := make([]hardlink.FileID, len(lines))
	types := make([]string, len(lines))
	for i, line := range lines {
		fields := strings.SplitN(line, " ", 4)
		if len(fields) != 4 {
			return nil, nil, pathError("stat", errPath, fmt.Errorf("unexpected stat output %q", out))
		}
		if ids[i], _, err = parseIdentity(fields[0], fields[1], fields[2]); err != nil {
			return nil, nil, pathError("stat", errPath, err)
		}
		types[i] = fields[3]
	}
	return ids, types, nil
}

func (b *Backend) ReflinkTree(ctx context.Context, _ *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("reflinktree: %w: sftp has no reflink operation", fsops.ErrUnsupported)
}

func (b *Backend) RemoveTree(ctx context.Context, created *fsops.TreeCreateResult) error {
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

func readDir(ctx context.Context, client *sftp.Client, p string) ([]os.FileInfo, error) {
	return await(ctx, func() ([]os.FileInfo, error) { return client.ReadDirContext(ctx, p) })
}

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

func awaitMutation(_ context.Context, call func() error) error {
	return call()
}

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
