// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package remote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/sshpool"
	"github.com/autobrr/qui/pkg/hardlink"
)

const (
	// findRecordLimit caps one find record: the fields and one path. Linux
	// paths stop at 4096 bytes, so a longer record is not a path.
	findRecordLimit = 64 << 10
	// findEntryLimit and findByteLimit bound the walk's sort: a tree that
	// prints more entries or bytes than this is handed to the sftp walk,
	// which streams, rather than grow the sort past it. A hostile remote can
	// print records of findRecordLimit each, so the entry cap alone is not a
	// bound on memory.
	findEntryLimit = 500_000
	findByteLimit  = 64 << 20
)

// findFormat prints, for each entry below the root, its type, device, inode,
// link count, size, mtime, permission bits and path relative to the root.
// Only the path can hold a space, so it goes last, and NUL ends the record
// because NUL is the one byte a path cannot hold.
const findFormat = `%y %D %i %n %s %T@ %m %P\0`

// batchFormat is findFormat with the path as given, for find on a list of
// paths at depth 0.
const batchFormat = `%y %D %i %n %s %T@ %m %p\0`

const (
	// batchPaths and batchBytes size one find of a batch stat. The command
	// goes to the shell as one argument, and Linux caps one argument at
	// 128 KiB.
	batchPaths = 1000
	batchBytes = 64 << 10
	// batchTimeout bounds one find of a batch stat.
	batchTimeout = 30 * time.Second
	// batchWidth is how many single stats of a batch are in flight at once.
	// They share the one sftp client, so they overlap their round trips.
	//
	// ponytail: a fixed width, so n single stats cost about n/32 round
	// trips: a torrent of 1,000 moved files is about 32. A per-host setting
	// is the upgrade if a host with a long delay needs a wider pipeline.
	batchWidth = 32
)

var errFindLimit = errors.New("find prints more entries than its limit")

// errRelativePath answers a batch path without a leading "/".
var errRelativePath = errors.New("path is not absolute")

// execTier reports whether the stored tier says this instance runs commands.
// The live connection decides again: an exec on a connection whose probe
// found less answers fsops.ErrUnsupported.
func (b *Backend) execTier() bool { return b.inst.SSHTier == models.SSHTierExec }

// exec returns the connection for remote commands. It is client for exec,
// and answers fsops.ErrUnsupported when the live connection has no exec tier.
func (b *Backend) exec(ctx context.Context) (*sshpool.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := b.pool.Exec(ctx, b.inst)
	switch {
	case err == nil:
		return conn, nil
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case errors.Is(err, sshpool.ErrNoExec):
		return nil, fmt.Errorf("%w: %w", fsops.ErrUnsupported, err)
	}
	return nil, lost(err)
}

// output runs cmd and maps a dropped connection to ErrConnectionLost. A
// command that exits non-zero keeps its own error.
func output(ctx context.Context, conn *sshpool.Conn, cmd sshpool.Command) ([]byte, error) {
	out, err := conn.Output(ctx, cmd)
	if err != nil && ctx.Err() == nil && errors.Is(err, sshpool.ErrConnect) {
		err = lost(err)
	}
	return out, err
}

// identity reads the device, inode and link count of p with stat. Without
// follow it is lstat, so a symlink planted in a tree reports itself and never
// the identity of its target.
func (b *Backend) identity(ctx context.Context, p string, follow bool) (hardlink.FileID, uint64, error) {
	conn, err := b.exec(ctx)
	if err != nil {
		return hardlink.FileID{}, 0, err
	}
	options := []string{"--printf", `%d %i %h`}
	if follow {
		options = append([]string{"-L"}, options...)
	}
	out, err := output(ctx, conn, sshpool.Command{Name: "stat", Options: options, Paths: []string{p}})
	if err != nil {
		return hardlink.FileID{}, 0, pathError("stat", p, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 3 {
		return hardlink.FileID{}, 0, pathError("stat", p, fmt.Errorf("unexpected stat output %q", out))
	}
	return parseIdentity(fields[0], fields[1], fields[2])
}

func parseIdentity(dev, ino, nlink string) (hardlink.FileID, uint64, error) {
	var numbers [3]uint64
	for i, field := range []string{dev, ino, nlink} {
		n, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return hardlink.FileID{}, 0, fmt.Errorf("parse identity field %q: %w", field, err)
		}
		numbers[i] = n
	}
	return remoteFileID(numbers[0], numbers[1]), numbers[2], nil
}

// remoteFileID is the remote form of a device and inode. The tag keeps it
// apart from a kernel-attested identity on the qui host.
func remoteFileID(dev, ino uint64) hardlink.FileID {
	var raw [16]byte
	binary.BigEndian.PutUint64(raw[0:8], dev)
	binary.BigEndian.PutUint64(raw[8:16], ino)
	return hardlink.RemoteFileID(raw[:])
}

// setIdentity fills the identity of info the way the local backend does: for
// regular files and directories, with a failure in FileIDErr.
func (b *Backend) setIdentity(ctx context.Context, info *fsops.LstatInfo, follow bool) {
	if !info.Mode.IsRegular() && !info.IsDir {
		return
	}
	if !b.execTier() {
		info.FileIDErr = errNoIdentity
		return
	}
	info.FileID, info.Nlinks, info.FileIDErr = b.identity(ctx, info.Path, follow)
}

// StatBatch answers fsops.BatchStater. It is LstatBatch, but it follows
// symlinks, as Stat does.
func (b *Backend) StatBatch(ctx context.Context, paths []string) ([]*fsops.LstatInfo, []error) {
	return b.statBatch(ctx, paths, true)
}

// LstatBatch answers fsops.BatchStater. On the exec tier, one find lstats a
// batch of paths, with identity, in two round trips. Lstat answers each path
// that find did not print, such as a missing one, so the error is the one
// Lstat gives. On the sftp tier, Lstat answers every path. The single Lstat
// calls run batchWidth at a time.
//
// The paths must be absolute: find reads an argument that starts with "-",
// "!" or "(" as an expression.
func (b *Backend) LstatBatch(ctx context.Context, paths []string) ([]*fsops.LstatInfo, []error) {
	return b.statBatch(ctx, paths, false)
}

func (b *Backend) statBatch(ctx context.Context, paths []string, follow bool) ([]*fsops.LstatInfo, []error) {
	op, statOne := "lstat", b.Lstat
	if follow {
		op, statOne = "stat", b.Stat
	}
	infos := make([]*fsops.LstatInfo, len(paths))
	errs := make([]error, len(paths))

	// find reads a leading "-", "!" or "(" as the start of its expression,
	// whatever comes before it, so a path that is not absolute is answered
	// here and never sent.
	absolute := make([]string, 0, len(paths))
	for i, p := range paths {
		if path.IsAbs(p) {
			absolute = append(absolute, p)
		} else {
			errs[i] = &fs.PathError{Op: op, Path: p, Err: errRelativePath}
		}
	}
	found := make(map[string]findEntry, len(absolute))
	if b.execTier() {
		b.statFind(ctx, absolute, follow, found)
	}

	var single []int
	for i, p := range paths {
		if errs[i] != nil {
			continue
		}
		e, ok := found[p]
		// find -L prints a link it cannot follow as the link itself, but
		// Stat of a broken link is an error.
		if !ok || follow && e.mode&fs.ModeSymlink != 0 {
			single = append(single, i)
			continue
		}
		info := &fsops.LstatInfo{
			Path:      p,
			Size:      e.size,
			ModTime:   e.modTime,
			IsDir:     e.mode.IsDir(),
			IsSymlink: e.mode&fs.ModeSymlink != 0,
			Mode:      e.mode,
		}
		// Lstat gives identity to regular files and directories only.
		if e.mode.IsRegular() || e.mode.IsDir() {
			info.FileID, info.Nlinks = e.id, e.nlink
		}
		infos[i] = info
	}

	// After a lost connection, at most one call dials the host again. If
	// the host is back, each path gets a real answer. If not, the pool
	// remembers the failed dial, and each path gets its own error at once.
	var g errgroup.Group
	g.SetLimit(batchWidth)
	for _, i := range single {
		g.Go(func() error {
			infos[i], errs[i] = statOne(ctx, paths[i])
			return nil
		})
	}
	_ = g.Wait() // each error stays with its path
	return infos, errs
}

// statFind puts the find record of each path it can read into found, by
// path, one batch at a time. With follow, find reads the target of a link.
//
// ponytail: the batches run one after the other. Running them on the exec
// sessions of the connection at once is the upgrade if a torrent with many
// thousand files is ever slow.
func (b *Backend) statFind(ctx context.Context, paths []string, follow bool, found map[string]findEntry) {
	conn, err := b.exec(ctx)
	if err != nil {
		return
	}
	option := "-P"
	if follow {
		option = "-L"
	}
	for len(paths) > 0 {
		n, size := 0, 0
		for n < len(paths) && n < batchPaths && (n == 0 || size+len(paths[n]) <= batchBytes) {
			size += len(paths[n])
			n++
		}
		batchCtx, cancel := context.WithTimeout(ctx, batchTimeout)
		// find prints at most one record for each path at depth 0, so a
		// host that prints more is not find.
		entries, err := find(batchCtx, conn, sshpool.Command{
			Name:       "find",
			Options:    []string{option},
			Paths:      paths[:n],
			Expression: []string{"-maxdepth", "0", "-printf", batchFormat},
		}, n, findByteLimit)
		cancel()
		for _, e := range entries {
			found[e.rel] = e
		}
		// find exits non-zero for a missing path and still prints the rest.
		// A lost connection or an end of ctx stops the batches, and the
		// single calls then answer the paths that are left.
		if err != nil && (ctx.Err() != nil || errors.Is(err, sshpool.ErrConnect)) {
			log.Debug().Int("instanceID", b.inst.ID).Err(err).Msg("remote: batch stat stopped")
			return
		}
		paths = paths[n:]
	}
}

// sameFilesystemExec compares the device numbers of the two paths. It follows
// links, as the local backend does.
func (b *Backend) sameFilesystemExec(ctx context.Context, p1, p2 string) (bool, error) {
	conn, err := b.exec(ctx)
	if err != nil {
		return false, err
	}
	out, err := output(ctx, conn, sshpool.Command{Name: "stat", Options: []string{"-L", "--printf", `%d\0`}, Paths: []string{p1, p2}})
	if err != nil {
		return false, fmt.Errorf("samefilesystem %s %s: %w", p1, p2, err)
	}
	devices := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(devices) != 2 || devices[0] == "" {
		return false, fmt.Errorf("samefilesystem %s %s: unexpected stat output %q", p1, p2, out)
	}
	return devices[0] == devices[1], nil
}

// walkExec walks root with one find and reports whether it answered the
// walk. False means find could not, and the sftp walk takes over: find exits
// non-zero for an unreadable directory, and only the sftp walk can name
// which one; and a tree past the find limits is one the sftp walk streams.
//
// ponytail: find prints in disk order, so the whole tree is read and sorted
// in memory before the first entry goes out, up to findEntryLimit entries.
// find does not prune the directories that opts skips, so their entries count
// toward that limit too.
// Sorting per directory as the output streams is the upgrade if that
// ceiling is ever hit.
func (b *Backend) walkExec(ctx context.Context, ch chan<- fsops.WalkEntry, root string, opts fsops.WalkOptions) bool {
	// find reads a root that starts with "-", "!" or "(" as an expression, so
	// only an absolute root goes to it; the sftp walk takes any other.
	if !path.IsAbs(root) {
		return false
	}
	conn, err := b.exec(ctx)
	if err != nil {
		log.Debug().Int("instanceID", b.inst.ID).Err(err).Msg("remote: walking over sftp")
		return false
	}
	// -H follows only the root.
	option := "-P"
	if opts.FollowRoot {
		option = "-H"
	}
	entries, err := find(ctx, conn, sshpool.Command{
		Name:       "find",
		Options:    []string{option},
		Paths:      []string{root},
		Expression: []string{"-mindepth", "1", "-printf", findFormat},
	}, findEntryLimit, findByteLimit)
	switch {
	case ctx.Err() != nil:
		return true
	case errors.Is(err, errFindLimit):
		// Nothing has been sent yet, so the sftp walk takes the whole tree.
		log.Debug().Int("instanceID", b.inst.ID).Str("root", root).Err(err).Msg("remote: find output over its limit, walking over sftp")
		return false
	case err != nil:
		log.Debug().Int("instanceID", b.inst.ID).Str("root", root).Err(err).Msg("remote: find failed, walking over sftp")
		return false
	}

	// fsops.Backend promises lexical order within a directory and a directory
	// before its contents. Comparing path components gives both.
	slices.SortFunc(entries, func(a, b findEntry) int { return comparePaths(a.rel, b.rel) })

	skipped := ""
	for _, e := range entries {
		if skipped != "" && strings.HasPrefix(e.rel, skipped+"/") {
			continue
		}
		name := path.Base(e.rel)
		fullPath := path.Join(root, e.rel)
		if opts.Skip(name, fullPath, e.mode.IsDir(), false) {
			if e.mode.IsDir() {
				skipped = e.rel
			}
			continue
		}
		entry := fsops.WalkEntry{
			Path:      fullPath,
			Size:      e.size,
			ModTime:   e.modTime,
			IsDir:     e.mode.IsDir(),
			IsSymlink: e.mode&fs.ModeSymlink != 0,
			Mode:      e.mode,
			RelPath:   e.rel,
		}
		if opts.WantFileID && e.mode.IsRegular() {
			entry.FileID, entry.Nlinks = e.id, e.nlink
		}
		if !send(ctx, ch, entry) {
			return true
		}
	}
	return true
}

type findEntry struct {
	rel     string
	mode    fs.FileMode
	size    int64
	modTime time.Time
	id      hardlink.FileID
	nlink   uint64
}

// find runs cmd, streams its output and parses it record by record, at most
// limit records. When the command exits non-zero after clean output, the
// entries it printed come back with the error.
func find(ctx context.Context, conn *sshpool.Conn, cmd sshpool.Command, maxEntries, maxBytes int) ([]findEntry, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := conn.Stream(ctx, cmd, pw)
		// The reader sees a clean end and takes the exit status from done,
		// so a non-zero exit keeps the records before it.
		_ = pw.Close()
		done <- err
	}()
	// stop ends the command early: the closed pipe unblocks a write in flight.
	stop := func(err error) ([]findEntry, error) {
		cancel()
		_ = pr.CloseWithError(err)
		<-done
		return nil, err
	}

	reader := bufio.NewReaderSize(pr, findRecordLimit)
	var entries []findEntry
	read := 0
	for {
		record, err := reader.ReadSlice(0)
		switch {
		case errors.Is(err, io.EOF) && len(record) == 0:
			return entries, <-done
		case errors.Is(err, io.EOF):
			return stop(fmt.Errorf("find output ends inside a record: %q", record))
		case errors.Is(err, bufio.ErrBufferFull):
			return stop(fmt.Errorf("find record exceeds %d bytes", findRecordLimit))
		case err != nil:
			return stop(err)
		}
		if len(entries) == maxEntries {
			return stop(fmt.Errorf("%w of %d entries", errFindLimit, maxEntries))
		}
		if read += len(record); read > maxBytes {
			return stop(fmt.Errorf("%w of %d bytes", errFindLimit, maxBytes))
		}
		entry, err := parseFindRecord(record[:len(record)-1])
		if err != nil {
			return stop(err)
		}
		entries = append(entries, entry)
	}
}

// parseFindRecord reads one findFormat record without its NUL.
func parseFindRecord(record []byte) (findEntry, error) {
	fields := bytes.SplitN(record, []byte(" "), 8)
	if len(fields) != 8 || len(fields[0]) != 1 || len(fields[7]) == 0 {
		return findEntry{}, fmt.Errorf("unexpected find record %q", record)
	}
	id, nlink, err := parseIdentity(string(fields[1]), string(fields[2]), string(fields[3]))
	if err != nil {
		return findEntry{}, err
	}
	size, err := strconv.ParseInt(string(fields[4]), 10, 64)
	if err != nil {
		return findEntry{}, fmt.Errorf("parse find size %q: %w", fields[4], err)
	}
	modTime, err := parseFindTime(string(fields[5]))
	if err != nil {
		return findEntry{}, err
	}
	perm, err := strconv.ParseUint(string(fields[6]), 8, 32)
	if err != nil {
		return findEntry{}, fmt.Errorf("parse find mode %q: %w", fields[6], err)
	}
	return findEntry{
		rel:     string(fields[7]),
		mode:    findMode(fields[0][0], uint32(perm)),
		size:    size,
		modTime: modTime,
		id:      id,
		nlink:   nlink,
	}, nil
}

// parseFindTime reads %T@, seconds and a fraction, without the rounding of a
// float.
func parseFindTime(text string) (time.Time, error) {
	secText, fracText, _ := strings.Cut(text, ".")
	sec, err := strconv.ParseInt(secText, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse find mtime %q: %w", text, err)
	}
	fracText = (fracText + "000000000")[:9]
	nsec, err := strconv.ParseInt(fracText, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse find mtime %q: %w", text, err)
	}
	return time.Unix(sec, nsec), nil
}

// findMode builds a FileMode from the %y type letter and the %m bits.
func findMode(kind byte, perm uint32) fs.FileMode {
	mode := fs.FileMode(perm & 0o777)
	if perm&0o4000 != 0 {
		mode |= fs.ModeSetuid
	}
	if perm&0o2000 != 0 {
		mode |= fs.ModeSetgid
	}
	if perm&0o1000 != 0 {
		mode |= fs.ModeSticky
	}
	switch kind {
	case 'd':
		mode |= fs.ModeDir
	case 'l':
		mode |= fs.ModeSymlink
	case 'p':
		mode |= fs.ModeNamedPipe
	case 's':
		mode |= fs.ModeSocket
	case 'c':
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case 'b':
		mode |= fs.ModeDevice
	case 'f':
	default:
		mode |= fs.ModeIrregular
	}
	return mode
}

// comparePaths orders slash paths component by component, so a directory
// sorts before everything beneath it and siblings sort lexically.
func comparePaths(a, b string) int {
	for {
		aHead, aRest, aMore := strings.Cut(a, "/")
		bHead, bRest, bMore := strings.Cut(b, "/")
		if c := strings.Compare(aHead, bHead); c != 0 {
			return c
		}
		switch {
		case !aMore && !bMore:
			return 0
		case !aMore:
			return -1
		case !bMore:
			return 1
		}
		a, b = aRest, bRest
	}
}
