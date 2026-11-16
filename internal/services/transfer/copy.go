// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // BitTorrent v1 pieces are SHA-1
	"errors"
	"io"

	"github.com/autobrr/qui/internal/fsops"
)

// incompleteExt is the suffix that qBittorrent's setting "Append .!qB
// extension to incomplete files" gives each incomplete file that it checks.
const incompleteExt = ".!qB"

// open is a file of the copy whose bytes qui reads and writes.
type open struct {
	reader io.ReadCloser
	writer fsops.FileWriter
	// done is set once every byte of the file is written.
	done bool
}

// copier copies the pieces of a torrent in order and checks each one.
type copier struct {
	j       *job
	open    map[int]*open
	dirty   map[int]struct{}
	pending []int
	written int64
	// confirmed is false once a flush was not confirmed.
	confirmed bool
}

// copy copies every piece from the source to temporary files on the target.
// qui checks each piece against the .torrent before it writes it, and
// records it as a Proven piece once the target confirmed the flush of its
// bytes.
func (j *job) copy(ctx context.Context) error {
	c := &copier{j: j, open: map[int]*open{}, dirty: map[int]struct{}{}, confirmed: true}
	defer c.close()
	info := j.src.info
	j.proven = make([]byte, (info.NumPieces()+7)/8)

	for i, f := range j.plan {
		if f.size == 0 && !f.pad {
			if _, err := c.writer(ctx, i); err != nil {
				return err
			}
			c.open[i].done = true
		}
	}
	total := info.TotalLength()
	buf := make([]byte, info.PieceLength)
	for piece := range info.NumPieces() {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := int64(piece) * info.PieceLength
		data := buf[:min(start+info.PieceLength, total)-start]
		segments := j.segments(start, int64(len(data)))
		for _, s := range segments {
			if err := c.read(ctx, s, data[s.at:s.at+s.n]); err != nil {
				return err
			}
		}
		sum := sha1.Sum(data) //nolint:gosec // BitTorrent v1 pieces are SHA-1
		if !bytes.Equal(sum[:], info.Pieces[piece*sha1.Size:(piece+1)*sha1.Size]) {
			return permanent("Piece %d on %s does not match the .torrent. qBittorrent can find the damage with a recheck.", piece, j.source.Name)
		}
		for _, s := range segments {
			if err := c.write(ctx, s, data[s.at:s.at+s.n]); err != nil {
				return err
			}
		}
		c.pending = append(c.pending, piece)
		c.written += int64(len(data))
		if c.written >= j.svc.deps.Config.FlushBytes || piece == info.NumPieces()-1 {
			if err := c.flush(ctx); err != nil {
				return err
			}
			if err := j.checkSource(ctx); err != nil {
				return err
			}
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

// segment is the part of one file in a piece.
type segment struct {
	file int
	// offset is where the part starts in the file, at where it starts in
	// the piece, and n is its length.
	offset, at, n int64
}

func (j *job) segments(start, length int64) []segment {
	var out []segment
	end := start + length
	for i, f := range j.plan {
		from, to := max(start, f.offset), min(end, f.offset+f.size)
		if from < to {
			out = append(out, segment{file: i, offset: from - f.offset, at: from - start, n: to - from})
		}
	}
	return out
}

func (c *copier) read(ctx context.Context, s segment, into []byte) error {
	f := c.j.plan[s.file]
	if f.pad {
		clear(into)
		return nil
	}
	o := c.file(s.file)
	if o.reader == nil {
		paths := c.j.srcFiles.Paths()
		reader, err := c.j.srcFiles.OpenRead(ctx, paths.Join(c.j.src.torrent.SavePath, paths.FromSlash(f.name)), s.offset)
		if err != nil {
			return temporary("qui cannot read %s on %s: %v", f.name, c.j.source.Name, err)
		}
		o.reader = reader
	}
	if _, err := io.ReadFull(o.reader, into); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return sourceChanged("the file " + f.name + " got shorter")
		}
		return temporary("qui cannot read %s on %s: %v", f.name, c.j.source.Name, err)
	}
	if s.offset+s.n == f.size {
		_ = o.reader.Close()
		o.reader = nil
	}
	return nil
}

func (c *copier) write(ctx context.Context, s segment, data []byte) error {
	if c.j.plan[s.file].pad {
		return nil
	}
	w, err := c.writer(ctx, s.file)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return temporary("qui cannot write on %s: %v", c.j.target.Name, err)
	}
	c.dirty[s.file] = struct{}{}
	if s.offset+s.n == c.j.plan[s.file].size {
		c.open[s.file].done = true
	}
	return nil
}

func (c *copier) file(i int) *open {
	o, ok := c.open[i]
	if !ok {
		o = &open{}
		c.open[i] = o
	}
	return o
}

// writer creates the temporary file of a file at its first byte.
func (c *copier) writer(ctx context.Context, i int) (fsops.FileWriter, error) {
	o := c.file(i)
	if o.writer == nil {
		name := c.j.tempName(c.j.plan[i].name)
		w, err := c.j.root.Create(ctx, name)
		if err != nil {
			return nil, permanent("qui cannot create %s on %s: %v", name, c.j.target.Name, err)
		}
		o.writer = w
		c.dirty[i] = struct{}{}
	}
	return o.writer, nil
}

// flush flushes each file written since the last flush and closes the files
// that are complete. When the target confirms every flush, the pieces
// written since the last flush become Proven pieces.
func (c *copier) flush(ctx context.Context) error {
	confirmed := true
	for i := range c.dirty {
		ok, err := c.open[i].writer.Flush()
		if err != nil {
			return temporary("qui cannot flush %s on %s: %v", c.j.plan[i].name, c.j.target.Name, err)
		}
		confirmed = confirmed && ok
		if c.open[i].done {
			if err := c.open[i].writer.Close(); err != nil {
				return temporary("qui cannot close %s on %s: %v", c.j.plan[i].name, c.j.target.Name, err)
			}
			c.open[i].writer = nil
		}
	}
	clear(c.dirty)
	c.confirmed = c.confirmed && confirmed
	pending := c.pending
	c.pending, c.written = nil, 0
	if !confirmed || len(pending) == 0 {
		return nil
	}
	for _, piece := range pending {
		c.j.proven[piece/8] |= 0x80 >> (piece % 8)
	}
	c.j.member().ProvenPieces = c.j.proven
	return c.j.svc.deps.Store.SaveProven(ctx, c.j.rec.ID, 0, c.j.proven)
}

// close closes the files of a copy that stopped. A writer is closed without
// a flush, so its pieces stay unproven.
func (c *copier) close() {
	for _, o := range c.open {
		if o.reader != nil {
			_ = o.reader.Close()
		}
		if o.writer != nil {
			_ = o.writer.Close()
		}
	}
}

// checkSourceSizes fails when a source file has another size than at the
// start of the copy.
func (j *job) checkSourceSizes(ctx context.Context) error {
	paths := j.srcFiles.Paths()
	for _, f := range j.plan {
		if f.pad {
			continue
		}
		info, err := j.srcFiles.Lstat(ctx, paths.Join(j.src.torrent.SavePath, paths.FromSlash(f.name)))
		if err != nil {
			return temporary("qui cannot read %s on %s: %v", f.name, j.source.Name, err)
		}
		if info.Size != f.size {
			return sourceChanged("the file " + f.name + " changed its size")
		}
	}
	return nil
}

// allProven reports whether every piece is a Proven piece.
func (j *job) allProven() bool {
	pieces := j.src.info.NumPieces()
	for piece := range pieces {
		if j.proven[piece/8]&(0x80>>(piece%8)) == 0 {
			return false
		}
	}
	return true
}
