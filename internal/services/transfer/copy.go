// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha1" //nolint:gosec // BitTorrent v1 pieces are SHA-1
	"errors"
	"io"
	"io/fs"
	"math/bits"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/autobrr/qui/internal/fsops"
)

const incompleteExt = ".!qB"

const (
	maxConnections = 4
	minRange       = 1 << 20
	maxFiles       = 128
)

type open struct {
	writer fsops.FileWriter
	next   int64
}

type reading struct {
	name   string
	reader io.ReadCloser
	pos    int64
}

type copied struct {
	part  *part
	piece int
}

type task struct {
	p        *part
	from, to int
}

type copier struct {
	j              *job
	source, target string
	files          chan struct{}
	window         int64

	mu     sync.Mutex
	checks sync.Mutex
}

type stream struct {
	c         *copier
	src       fsops.Backend
	buf       []byte
	reads     map[string]*reading
	open      map[*groupFile]*open
	dirty     map[*groupFile]struct{}
	pending   []copied
	written   int64
	confirmed bool
}

type targetError struct{ error }

func (e targetError) Unwrap() error { return e.error }

func (j *job) copyAll(ctx context.Context) error {
	cfg := j.svc.deps.Config
	for drops := 0; ; {
		if err := j.checkPartials(ctx); err != nil {
			return err
		}
		before := j.flushedPieces
		err := j.copy(ctx)
		if err == nil || !errors.Is(err, fsops.ErrConnectionLost) {
			return err
		}
		if j.flushedPieces > before {
			drops = 0
		}
		if drops++; drops > cfg.Reconnects {
			return err
		}
		side := j.source.Name
		if _, ok := errors.AsType[targetError](err); ok {
			side = j.target.Name
		}
		j.logf("The connection to %s dropped. qui goes on with the pieces that are not proven yet.", side)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.ReconnectWait):
		}
	}
}

func provenCount(proven []byte) int {
	n := 0
	for _, b := range proven {
		n += bits.OnesCount8(b)
	}
	return n
}

func (j *job) copy(ctx context.Context) error {
	c := &copier{j: j, source: j.source.Name, target: j.target.Name, files: make(chan struct{}, maxFiles)}
	if j.made == nil {
		j.made = map[*groupFile]int64{}
	}
	if err := j.linkCopies(ctx); err != nil {
		return err
	}
	runs, total, all := j.runs()
	j.meter.set(all-total, all)
	conns, err := c.connections(ctx, total)
	if err != nil {
		return err
	}
	tasks := split(runs, total, len(conns))
	if err := c.create(ctx, tasks); err != nil {
		return err
	}
	c.window = j.svc.deps.Config.ReadBytes / int64(len(conns))
	queue := make(chan task, len(tasks))
	for _, t := range tasks {
		queue <- t
	}
	close(queue)
	g, streamCtx := errgroup.WithContext(ctx)
	for _, src := range conns {
		s := &stream{c: c, src: src, reads: map[string]*reading{}, open: map[*groupFile]*open{}, dirty: map[*groupFile]struct{}{}, confirmed: true}
		g.Go(func() error { return s.run(streamCtx, queue) })
	}
	if err := g.Wait(); err != nil {
		return err
	}
	if err := c.check(ctx); err != nil {
		return err
	}
	return j.checkSourceSizes(ctx)
}

func (j *job) runs() (runs []task, total, all int64) {
	for _, p := range j.parts {
		info := p.src.info
		p.proven = make([]byte, (info.NumPieces()+7)/8)
		copy(p.proven, p.ProvenPieces)
		for piece := range info.NumPieces() {
			start := int64(piece) * info.PieceLength
			length := min(info.PieceLength, info.TotalLength()-start)
			if !slices.ContainsFunc(p.segments(start, length), func(s segment) bool { return j.writes(p, s) }) {
				continue
			}
			all += length
			if p.isProven(piece) || p.isChecked(piece) {
				continue
			}
			total += length
			if last := len(runs) - 1; last >= 0 && runs[last].p == p && runs[last].to == piece {
				runs[last].to++
			} else {
				runs = append(runs, task{p: p, from: piece, to: piece + 1})
			}
		}
	}
	return runs, total, all
}

func split(runs []task, total int64, n int) []task {
	size := (total + int64(n) - 1) / int64(n)
	var out []task
	for _, run := range runs {
		length := run.p.src.info.PieceLength
		pieces := max(1, int((size+length-1)/length))
		for from := run.from; from < run.to; from += pieces {
			out = append(out, task{p: run.p, from: from, to: min(from+pieces, run.to)})
		}
	}
	return out
}

func (c *copier) connections(ctx context.Context, total int64) ([]fsops.Backend, error) {
	want := int(min(max((total+minRange-1)/minRange, 1), maxConnections))
	connector, ok := c.j.srcFiles.(fsops.Connector)
	if !ok || want == 1 {
		return []fsops.Backend{c.j.srcFiles}, nil
	}
	conns, err := connector.Connections(ctx, want)
	if err != nil {
		return nil, temporary("qui cannot read the files on %s: %w", c.source, err)
	}
	return conns, nil
}

func (c *copier) create(ctx context.Context, tasks []task) error {
	written := map[*groupFile]bool{}
	for _, t := range tasks {
		info := t.p.src.info
		start := int64(t.from) * info.PieceLength
		for _, s := range t.p.segments(start, min(int64(t.to)*info.PieceLength, info.TotalLength())-start) {
			if c.j.writes(t.p, s) {
				written[c.j.byRel[t.p.rel(t.p.plan[s.file])]] = true
			}
		}
	}
	for _, f := range c.j.files {
		if _, made := c.j.made[f]; made || f.onTarget || f.size > 0 && !written[f] {
			continue
		}
		if err := c.j.createTemp(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

func (j *job) createTemp(ctx context.Context, f *groupFile) error {
	name := j.tempName(f.rel)
	if err := j.retake(ctx, name); err != nil {
		return err
	}
	w, err := j.root.Create(ctx, name)
	if errors.Is(err, fsops.ErrConnectionLost) {
		j.unanswered[name] = true
		return temporary("qui cannot create %s on %s: %w", name, j.target.Name, targetError{err})
	}
	if err != nil {
		return permanent("qui cannot create %s on %s: %v", name, j.target.Name, err)
	}
	j.made[f] = 0
	if f.size == 0 {
		confirmed, err := w.Flush()
		if err != nil {
			_ = w.Close()
			return temporary("qui cannot flush %s on %s: %w", f.rel, j.target.Name, targetError{err})
		}
		if err := j.recordFlush(ctx, confirmed); err != nil {
			_ = w.Close()
			return err
		}
	}
	if err := w.Close(); err != nil {
		return temporary("qui cannot close %s on %s: %w", f.rel, j.target.Name, targetError{err})
	}
	return nil
}

func (j *job) retake(ctx context.Context, name string) error {
	if !j.unanswered[name] {
		return nil
	}
	if err := j.root.Remove(ctx, name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return temporary("qui cannot remove %s on %s: %w", name, j.target.Name, targetError{err})
	}
	delete(j.unanswered, name)
	return nil
}

func (c *copier) prove(ctx context.Context, pending []copied, confirmed bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(pending) == 0 {
		return nil
	}
	if err := c.j.recordFlush(ctx, confirmed); err != nil {
		return err
	}
	c.j.flushedPieces += len(pending)
	if c.j.rec.FlushState == FlushUnconfirmed {
		return nil
	}
	var parts []*part
	for _, done := range pending {
		done.part.proven[done.piece/8] |= 0x80 >> (done.piece % 8)
		if !slices.Contains(parts, done.part) {
			parts = append(parts, done.part)
		}
	}
	for _, p := range parts {
		p.ProvenPieces = p.proven
		if err := c.j.svc.deps.Store.SaveProven(ctx, c.j.rec.ID, p.position, p.proven); err != nil {
			return err
		}
	}
	return nil
}

func (c *copier) check(ctx context.Context) error {
	c.checks.Lock()
	defer c.checks.Unlock()
	if err := c.j.checkInstances(ctx); err != nil {
		return err
	}
	return c.j.checkSource(ctx)
}

func (s *stream) run(ctx context.Context, tasks <-chan task) error {
	for t := range tasks {
		err := s.task(ctx, t)
		s.close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *stream) task(ctx context.Context, t task) error {
	info := t.p.src.info
	window := max(1, int(s.c.window/info.PieceLength))
	for from := t.from; from < t.to; from += window {
		if err := ctx.Err(); err != nil {
			return err
		}
		to := min(from+window, t.to)
		start := int64(from) * info.PieceLength
		end := min(int64(to)*info.PieceLength, info.TotalLength())
		if int64(cap(s.buf)) < end-start {
			s.buf = make([]byte, end-start)
		}
		data := s.buf[:end-start]
		if err := s.read(ctx, t.p, start, data); err != nil {
			return err
		}
		for piece := from; piece < to; piece++ {
			at := int64(piece)*info.PieceLength - start
			if err := s.piece(ctx, t.p, piece, data[at:min(at+info.PieceLength, end-start)]); err != nil {
				return err
			}
			if s.written >= s.c.j.svc.deps.Config.FlushBytes || piece == t.to-1 {
				if err := s.checkpoint(ctx); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *stream) read(ctx context.Context, p *part, start int64, data []byte) error {
	segments := p.segments(start, int64(len(data)))
	paths := s.src.Paths()
	reads := make([]*reading, len(segments))
	errs := make([]error, len(segments))
	var wg sync.WaitGroup
	for i, seg := range segments {
		f := p.plan[seg.file]
		into := data[seg.at : seg.at+seg.n]
		if f.pad {
			clear(into)
			continue
		}
		name := paths.Join(p.src.torrent.SavePath, paths.FromSlash(f.name))
		r := s.reads[name]
		delete(s.reads, name)
		wg.Go(func() { reads[i], errs[i] = s.readFile(ctx, f, name, r, seg.offset, into) })
	}
	wg.Wait()
	for _, r := range reads {
		if r != nil {
			s.reads[r.name] = r
		}
	}
	return cmp.Or(errs...)
}

func (s *stream) readFile(ctx context.Context, f file, name string, r *reading, offset int64, into []byte) (*reading, error) {
	if r != nil && r.pos != offset {
		_ = r.reader.Close()
		r = nil
	}
	select {
	case s.c.files <- struct{}{}:
	case <-ctx.Done():
		if r != nil {
			_ = r.reader.Close()
		}
		return nil, ctx.Err()
	}
	defer func() { <-s.c.files }()
	if r == nil {
		reader, err := s.src.OpenRead(ctx, name, offset)
		if err != nil {
			return nil, temporary("qui cannot read %s on %s: %w", f.name, s.c.source, err)
		}
		r = &reading{name: name, reader: reader, pos: offset}
	}
	n, err := io.ReadFull(r.reader, into)
	r.pos += int64(n)
	if err != nil {
		_ = r.reader.Close()
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return nil, sourceChanged("the file " + f.name + " got shorter")
		}
		return nil, temporary("qui cannot read %s on %s: %w", f.name, s.c.source, err)
	}
	if r.pos == f.size {
		_ = r.reader.Close()
		return nil, nil
	}
	return r, nil
}

func (s *stream) piece(ctx context.Context, p *part, piece int, data []byte) error {
	info := p.src.info
	sum := sha1.Sum(data) //nolint:gosec // BitTorrent v1 pieces are SHA-1
	if !bytes.Equal(sum[:], info.Pieces[piece*sha1.Size:(piece+1)*sha1.Size]) {
		return permanent("Piece %d on %s does not match the .torrent. qBittorrent can find the damage with a recheck.", piece, s.c.source)
	}
	for _, seg := range p.segments(int64(piece)*info.PieceLength, int64(len(data))) {
		if s.c.j.writes(p, seg) {
			if err := s.write(ctx, p, seg, data[seg.at:seg.at+seg.n]); err != nil {
				return err
			}
		}
	}
	s.pending = append(s.pending, copied{part: p, piece: piece})
	s.written += int64(len(data))
	s.c.j.meter.add(int64(len(data)))
	return nil
}

type segment struct {
	file          int
	offset, at, n int64
}

func (p *part) segments(start, length int64) []segment {
	var out []segment
	end := start + length
	for i, f := range p.plan {
		from, to := max(start, f.offset), min(end, f.offset+f.size)
		if from < to {
			out = append(out, segment{file: i, offset: from - f.offset, at: from - start, n: to - from})
		}
	}
	return out
}

func (j *job) writes(p *part, s segment) bool {
	f := p.plan[s.file]
	if f.pad || f.onTarget {
		return false
	}
	g := j.byRel[p.rel(f)]
	return g.owner == p && !g.linked && !g.linksCopy
}

func (s *stream) write(ctx context.Context, p *part, seg segment, data []byte) error {
	f := s.c.j.byRel[p.rel(p.plan[seg.file])]
	o, err := s.writer(ctx, f, seg.offset)
	if err != nil {
		return err
	}
	if _, err := o.writer.Write(data); err != nil {
		return temporary("qui cannot write on %s: %w", s.c.target, targetError{err})
	}
	o.next += seg.n
	s.dirty[f] = struct{}{}
	if o.next == f.size {
		return s.finish(f)
	}
	return nil
}

func (s *stream) writer(ctx context.Context, f *groupFile, offset int64) (*open, error) {
	o := s.open[f]
	if o != nil && o.next != offset {
		if err := s.finish(f); err != nil {
			return nil, err
		}
		o = nil
	}
	if o == nil {
		name := s.c.j.tempName(f.rel)
		w, err := s.c.j.root.OpenWrite(ctx, name, offset)
		if errors.Is(err, fs.ErrNotExist) {
			w, err = s.c.j.root.OpenWrite(ctx, name+incompleteExt, offset)
		}
		if err != nil {
			return nil, temporary("qui cannot open %s on %s: %w", name, s.c.target, targetError{err})
		}
		o = &open{writer: w, next: offset}
		s.open[f] = o
	}
	return o, nil
}

func (s *stream) finish(f *groupFile) error {
	o := s.open[f]
	delete(s.open, f)
	delete(s.dirty, f)
	confirmed, err := o.writer.Flush()
	if err != nil {
		_ = o.writer.Close()
		return temporary("qui cannot flush %s on %s: %w", f.rel, s.c.target, targetError{err})
	}
	s.confirmed = s.confirmed && confirmed
	if err := o.writer.Close(); err != nil {
		return temporary("qui cannot close %s on %s: %w", f.rel, s.c.target, targetError{err})
	}
	return nil
}

func (s *stream) checkpoint(ctx context.Context) error {
	for f := range s.dirty {
		confirmed, err := s.open[f].writer.Flush()
		if err != nil {
			return temporary("qui cannot flush %s on %s: %w", f.rel, s.c.target, targetError{err})
		}
		s.confirmed = s.confirmed && confirmed
	}
	clear(s.dirty)
	pending, confirmed := s.pending, s.confirmed
	s.pending, s.written, s.confirmed = nil, 0, true
	if err := s.c.prove(ctx, pending, confirmed); err != nil {
		return err
	}
	return s.c.check(ctx)
}

func (s *stream) close() {
	for _, r := range s.reads {
		_ = r.reader.Close()
	}
	for _, o := range s.open {
		_ = o.writer.Close()
	}
	clear(s.reads)
	clear(s.open)
	clear(s.dirty)
}

func (j *job) checkSourceSizes(ctx context.Context) error {
	paths := j.srcFiles.Paths()
	for _, f := range j.files {
		info, err := j.srcFiles.Lstat(ctx, paths.Join(f.owner.src.torrent.SavePath, paths.FromSlash(f.name)))
		if err != nil {
			return temporary("qui cannot read %s on %s: %w", f.name, j.source.Name, err)
		}
		if info.Size != f.size {
			return sourceChanged("the file " + f.name + " changed its size")
		}
	}
	return nil
}

func (p *part) allProven() bool {
	for piece := range p.src.info.NumPieces() {
		if !p.isProven(piece) {
			return false
		}
	}
	return true
}

func (p *part) isProven(piece int) bool {
	return p.proven[piece/8]&(0x80>>(piece%8)) != 0
}
