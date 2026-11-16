// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // BitTorrent v1 pieces are SHA-1
	"errors"
	"io"
	"math/bits"
	"slices"
	"time"

	"github.com/autobrr/qui/internal/fsops"
)

// incompleteExt is the suffix that qBittorrent's setting "Append .!qB
// extension to incomplete files" gives each incomplete file that it checks.
const incompleteExt = ".!qB"

// open is a file of the group that qui writes on the target.
type open struct {
	writer fsops.FileWriter
	// next is the offset in the file of the next byte of writer.
	next int64
	// done is set once every byte of the file is written.
	done bool
}

type reading struct {
	reader io.ReadCloser
	pos    int64
}

type copied struct {
	part  *part
	piece int
}

// copier copies the pieces of each member in order and checks each one.
type copier struct {
	j       *job
	open    map[*groupFile]*open
	reads   map[string]*reading
	dirty   map[*groupFile]struct{}
	pending []copied
	written int64
	// confirmed is false once a flush was not confirmed.
	confirmed bool
}

// copyAll copies the pieces that are not Proven pieces. After a dropped
// connection it goes on with the pieces that are still missing, until
// Config.Reconnects drops in a row proved no piece.
func (j *job) copyAll(ctx context.Context) error {
	cfg := j.svc.deps.Config
	for drops := 0; ; {
		before := j.provenCount()
		err := j.copy(ctx)
		if err == nil || !errors.Is(err, fsops.ErrConnectionLost) {
			return err
		}
		if j.provenCount() > before {
			drops = 0
		}
		if drops++; drops > cfg.Reconnects {
			return err
		}
		j.logf("The connection to %s dropped. qui goes on with the pieces that are not proven yet.", j.source.Name)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.ReconnectWait):
		}
	}
}

func (j *job) provenCount() int {
	n := 0
	for _, p := range j.parts {
		n += provenCount(p.ProvenPieces)
	}
	return n
}

func provenCount(proven []byte) int {
	n := 0
	for _, b := range proven {
		n += bits.OnesCount8(b)
	}
	return n
}

// copy copies each piece of the group that is not a Proven piece, from the
// source to temporary files on the target. qui checks each piece against the
// .torrent before it writes it, and records it as a Proven piece once the
// target confirmed the flush of its bytes. A piece that holds no file that
// the member writes stays unproven: the check on the target proves it.
func (j *job) copy(ctx context.Context) error {
	c := &copier{j: j, open: map[*groupFile]*open{}, reads: map[string]*reading{}, dirty: map[*groupFile]struct{}{}, confirmed: true}
	defer c.close()
	if j.made == nil {
		j.made = map[*groupFile]int64{}
	}
	for _, f := range j.files {
		if _, made := j.made[f]; f.size == 0 && !f.onTarget && !made {
			if _, err := c.writer(ctx, f, 0); err != nil {
				return err
			}
			c.open[f].done = true
		}
	}
	for _, p := range j.parts {
		if err := c.part(ctx, p); err != nil {
			return err
		}
	}
	if err := c.flush(ctx); err != nil {
		return err
	}
	j.rec.FlushState = FlushConfirmed
	if !c.confirmed {
		j.rec.FlushState = FlushUnconfirmed
	}
	return j.checkSourceSizes(ctx)
}

func (c *copier) part(ctx context.Context, p *part) error {
	info := p.src.info
	p.proven = make([]byte, (info.NumPieces()+7)/8)
	copy(p.proven, p.ProvenPieces)
	buf := make([]byte, info.PieceLength)
	for piece := range info.NumPieces() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !p.isProven(piece) {
			if err := c.piece(ctx, p, piece, buf); err != nil {
				return err
			}
		}
		if c.written >= c.j.svc.deps.Config.FlushBytes || piece == info.NumPieces()-1 {
			if err := c.flush(ctx); err != nil {
				return err
			}
			if err := c.j.checkInstances(ctx); err != nil {
				return err
			}
			if err := c.j.checkSource(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

// piece reads one piece from the source, checks it against the .torrent,
// and writes the files of the member to the temporary files.
func (c *copier) piece(ctx context.Context, p *part, piece int, buf []byte) error {
	info := p.src.info
	start := int64(piece) * info.PieceLength
	data := buf[:min(start+info.PieceLength, info.TotalLength())-start]
	segments := p.segments(start, int64(len(data)))
	if !slices.ContainsFunc(segments, func(s segment) bool { return c.j.writes(p, s) }) {
		return nil
	}
	for _, s := range segments {
		if err := c.read(ctx, p, s, data[s.at:s.at+s.n]); err != nil {
			return err
		}
	}
	sum := sha1.Sum(data) //nolint:gosec // BitTorrent v1 pieces are SHA-1
	if !bytes.Equal(sum[:], info.Pieces[piece*sha1.Size:(piece+1)*sha1.Size]) {
		return permanent("Piece %d on %s does not match the .torrent. qBittorrent can find the damage with a recheck.", piece, c.j.source.Name)
	}
	for _, s := range segments {
		if c.j.writes(p, s) {
			if err := c.write(ctx, p, s, data[s.at:s.at+s.n]); err != nil {
				return err
			}
		}
	}
	c.pending = append(c.pending, copied{part: p, piece: piece})
	c.written += int64(len(data))
	return nil
}

// segment is the part of one file in a piece.
type segment struct {
	file int
	// offset is where the part starts in the file, at where it starts in
	// the piece, and n is its length.
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

// writes reports whether the member writes the file of the segment: the
// target does not have the file, qui did not link it, and the member is the
// first one that has it.
func (j *job) writes(p *part, s segment) bool {
	f := p.plan[s.file]
	if f.pad || f.onTarget {
		return false
	}
	g := j.byRel[p.rel(f)]
	return g.owner == p && !g.linked
}

func (c *copier) read(ctx context.Context, p *part, s segment, into []byte) error {
	f := p.plan[s.file]
	if f.pad {
		clear(into)
		return nil
	}
	paths := c.j.srcFiles.Paths()
	name := paths.Join(p.src.torrent.SavePath, paths.FromSlash(f.name))
	r := c.reads[name]
	if r != nil && r.pos != s.offset {
		_ = r.reader.Close()
		r = nil
	}
	if r == nil {
		reader, err := c.j.srcFiles.OpenRead(ctx, name, s.offset)
		if err != nil {
			return temporary("qui cannot read %s on %s: %w", f.name, c.j.source.Name, err)
		}
		r = &reading{reader: reader, pos: s.offset}
		c.reads[name] = r
	}
	n, err := io.ReadFull(r.reader, into)
	r.pos += int64(n)
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return sourceChanged("the file " + f.name + " got shorter")
		}
		return temporary("qui cannot read %s on %s: %w", f.name, c.j.source.Name, err)
	}
	if r.pos == f.size {
		_ = r.reader.Close()
		delete(c.reads, name)
	}
	return nil
}

func (c *copier) write(ctx context.Context, p *part, s segment, data []byte) error {
	f := c.j.byRel[p.rel(p.plan[s.file])]
	w, err := c.writer(ctx, f, s.offset)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return temporary("qui cannot write on %s: %v", c.j.target.Name, err)
	}
	c.open[f].next += s.n
	c.dirty[f] = struct{}{}
	if s.offset+s.n == f.size {
		c.open[f].done = true
	}
	return nil
}

// writer returns the writer of the temporary file of a file at offset. It
// creates the temporary file at its first byte, and writes into the one that
// qui made before. Proven pieces lie between the last write and offset when
// they differ: the writer starts again there after a flush.
func (c *copier) writer(ctx context.Context, f *groupFile, offset int64) (fsops.FileWriter, error) {
	o, ok := c.open[f]
	if !ok {
		o = &open{}
		c.open[f] = o
	}
	if o.writer != nil && o.next != offset {
		if err := c.flush(ctx); err != nil {
			return nil, err
		}
		if o.writer != nil {
			if err := o.writer.Close(); err != nil {
				return nil, temporary("qui cannot close %s on %s: %v", f.rel, c.j.target.Name, err)
			}
			o.writer = nil
		}
	}
	if o.writer == nil {
		name := c.j.tempName(f.rel)
		var (
			w   fsops.FileWriter
			err error
		)
		if _, made := c.j.made[f]; made {
			if w, err = c.j.root.OpenWrite(ctx, name, offset); err != nil {
				return nil, temporary("qui cannot open %s on %s: %v", name, c.j.target.Name, err)
			}
		} else {
			if w, err = c.j.root.Create(ctx, name); err != nil {
				return nil, permanent("qui cannot create %s on %s: %v", name, c.j.target.Name, err)
			}
			c.j.made[f] = 0
		}
		o.writer, o.next = w, offset
		c.dirty[f] = struct{}{}
	}
	return o.writer, nil
}

// flush flushes each file written since the last flush and closes the files
// that are complete. When the target confirms every flush, the pieces
// written since the last flush become Proven pieces.
func (c *copier) flush(ctx context.Context) error {
	confirmed := true
	for f := range c.dirty {
		o := c.open[f]
		ok, err := o.writer.Flush()
		if err != nil {
			return temporary("qui cannot flush %s on %s: %v", f.rel, c.j.target.Name, err)
		}
		confirmed = confirmed && ok
		if o.done {
			if err := o.writer.Close(); err != nil {
				return temporary("qui cannot close %s on %s: %v", f.rel, c.j.target.Name, err)
			}
			o.writer = nil
		}
	}
	clear(c.dirty)
	c.confirmed = c.confirmed && confirmed
	pending := c.pending
	c.pending, c.written = nil, 0
	if !confirmed || len(pending) == 0 {
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

// close closes the files of a copy that stopped. A writer is closed without
// a flush, so its pieces stay unproven.
func (c *copier) close() {
	for _, r := range c.reads {
		_ = r.reader.Close()
	}
	for _, o := range c.open {
		if o.writer != nil {
			_ = o.writer.Close()
		}
	}
}

// checkSourceSizes fails when a source file has another size than at the
// start of the copy.
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

// allProven reports whether every piece of the member is a Proven piece.
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
