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
	findRecordLimit = 64 << 10
	findEntryLimit  = 500_000
	findByteLimit   = 64 << 20
)

const findFormat = `%y %D %i %n %s %T@ %m %P\0`

const batchFormat = `%y %D %i %n %s %T@ %m %p\0`

const (
	batchPaths   = 1000
	batchBytes   = 64 << 10
	batchTimeout = 30 * time.Second
	batchWidth   = 32
)

var errFindLimit = errors.New("find prints more entries than its limit")

var errRelativePath = errors.New("path is not absolute")

func (b *Backend) execTier() bool { return b.inst.SSHTier == models.SSHTierExec }

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

func output(ctx context.Context, conn *sshpool.Conn, cmd sshpool.Command) ([]byte, error) {
	out, err := conn.Output(ctx, cmd)
	if err != nil && ctx.Err() == nil && errors.Is(err, sshpool.ErrConnect) {
		err = lost(err)
	}
	return out, err
}

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

func remoteFileID(dev, ino uint64) hardlink.FileID {
	var raw [16]byte
	binary.BigEndian.PutUint64(raw[0:8], dev)
	binary.BigEndian.PutUint64(raw[8:16], ino)
	return hardlink.RemoteFileID(raw[:])
}

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

func (b *Backend) StatBatch(ctx context.Context, paths []string) ([]*fsops.LstatInfo, []error) {
	return b.statBatch(ctx, paths, true)
}

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
		if e.mode.IsRegular() || e.mode.IsDir() {
			info.FileID, info.Nlinks = e.id, e.nlink
		}
		infos[i] = info
	}

	var g errgroup.Group
	g.SetLimit(batchWidth)
	for _, i := range single {
		g.Go(func() error {
			infos[i], errs[i] = statOne(ctx, paths[i])
			return nil
		})
	}
	_ = g.Wait()
	return infos, errs
}

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
		if err != nil && (ctx.Err() != nil || errors.Is(err, sshpool.ErrConnect)) {
			log.Debug().Int("instanceID", b.inst.ID).Err(err).Msg("remote: batch stat stopped")
			return
		}
		paths = paths[n:]
	}
}

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

func (b *Backend) walkExec(ctx context.Context, ch chan<- fsops.WalkEntry, root string, opts fsops.WalkOptions) bool {
	if !path.IsAbs(root) {
		return false
	}
	conn, err := b.exec(ctx)
	if err != nil {
		log.Debug().Int("instanceID", b.inst.ID).Err(err).Msg("remote: walking over sftp")
		return false
	}
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
		log.Debug().Int("instanceID", b.inst.ID).Str("root", root).Err(err).Msg("remote: find output over its limit, walking over sftp")
		return false
	case errors.Is(err, sshpool.ErrConnect):
		send(ctx, ch, fsops.WalkEntry{Path: root, IsDir: true, RelPath: ".", Err: pathError("find", root, lost(err))})
		return true
	case err != nil:
		log.Debug().Int("instanceID", b.inst.ID).Str("root", root).Err(err).Msg("remote: find failed, walking over sftp")
		return false
	}

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

func find(ctx context.Context, conn *sshpool.Conn, cmd sshpool.Command, maxEntries, maxBytes int) ([]findEntry, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := conn.Stream(ctx, cmd, pw)
		_ = pw.Close()
		done <- err
	}()
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
			if streamErr := <-done; streamErr != nil {
				return entries, streamErr
			}
			return nil, fmt.Errorf("find output ends inside a record: %q", record)
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
