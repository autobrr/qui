// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbtstub

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // BitTorrent v1 pieces are SHA-1
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const incompleteExt = ".!qB"

type ContentFile struct {
	Path string
	Data []byte
}

func NewContentTorrent(t *testing.T, name string, pieceLength int64, files ...ContentFile) (data []byte, hash string) {
	t.Helper()
	info := metainfo.Info{Name: name, PieceLength: pieceLength}
	var stream []byte
	for _, f := range files {
		info.Files = append(info.Files, metainfo.FileInfo{Path: strings.Split(f.Path, "/"), Length: int64(len(f.Data))})
		stream = append(stream, f.Data...)
	}
	for chunk := range slices.Chunk(stream, int(pieceLength)) {
		sum := sha1.Sum(chunk) //nolint:gosec // BitTorrent v1 pieces are SHA-1
		info.Pieces = append(info.Pieces, sum[:]...)
	}
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	mi := metainfo.MetaInfo{InfoBytes: infoBytes, Announce: "http://tracker.example.invalid/announce"}
	var buf bytes.Buffer
	require.NoError(t, mi.Write(&buf))
	return buf.Bytes(), mi.HashInfoBytes().HexString()
}

func (s *Server) Seed(data []byte, savePath string) string {
	return s.SeedWithLayout(data, savePath, "")
}

func (s *Server) SeedWithLayout(data []byte, savePath, layout string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := parseTorrent(data)
	require.True(s.t, ok, "not a torrent")
	params := map[string]string{"savepath": savePath, "seedMode": "true", "contentLayout": layout}
	s.place(t, func(key string) string { return params[key] })
	s.torrents[t.Hash] = t
	return t.Hash
}

func (s *Server) check(t *Torrent) float64 {
	if !s.HashCheck {
		return s.onDisk(t)
	}
	a := t.add
	files := s.diskFiles(t)
	var stream []byte
	starts := make([]int64, len(files))
	var missing [][2]int64
	for i, f := range files {
		starts[i] = int64(len(stream))
		data, err := os.ReadFile(f)
		if errors.Is(err, fs.ErrNotExist) {
			data, err = os.ReadFile(f + incompleteExt)
		}
		if err != nil || int64(len(data)) > a.sizes[i] {
			data = nil
		}
		if have := int64(len(data)); have < a.sizes[i] {
			missing = append(missing, [2]int64{starts[i] + have, starts[i] + a.sizes[i]})
			data = append(data, make([]byte, a.sizes[i]-have)...)
		}
		stream = append(stream, data...)
	}
	a.pieceStates = make([]int, len(a.pieces)/sha1.Size)
	passed := 0
	for i := range a.pieceStates {
		start := int64(i) * a.pieceLength
		end := min(start+a.pieceLength, int64(len(stream)))
		if slices.ContainsFunc(missing, func(m [2]int64) bool { return m[0] < end && start < m[1] }) {
			continue
		}
		sum := sha1.Sum(stream[start:end]) //nolint:gosec // BitTorrent v1 pieces are SHA-1
		if bytes.Equal(sum[:], a.pieces[i*sha1.Size:(i+1)*sha1.Size]) {
			a.pieceStates[i] = 2
			passed++
		}
	}
	if s.IncompleteFilesExt {
		for i, f := range files {
			first, last := starts[i]/a.pieceLength, (starts[i]+a.sizes[i]-1)/a.pieceLength
			complete := !slices.Contains(a.pieceStates[first:last+1], 0)
			from, to := f, f+incompleteExt
			if complete {
				from, to = to, from
			}
			if _, err := os.Stat(from); err == nil {
				assert.NoError(s.t, os.Rename(from, to))
			}
		}
	}
	return float64(passed) / float64(len(a.pieceStates))
}

func (s *Server) pieceStatesOf(w http.ResponseWriter, param func(string) string) {
	t, ok := s.torrents[strings.ToLower(param("hash"))]
	if !ok || t.add == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	states := t.add.pieceStates
	if states == nil {
		state := 0
		if t.add.checked && t.add.progress >= 1 {
			state = 2
		}
		states = make([]int, len(t.add.pieces)/sha1.Size)
		for i := range states {
			states[i] = state
		}
	}
	writeJSON(w, states)
}

func (s *Server) export(w http.ResponseWriter, param func(string) string) {
	t, ok := s.torrents[strings.ToLower(param("hash"))]
	if !ok || t.add == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/x-bittorrent")
	_, _ = w.Write(t.add.torrent)
}

func (s *Server) renameFile(w http.ResponseWriter, param func(string) string) {
	t, ok := s.torrents[strings.ToLower(param("hash"))]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	names := s.fileNames(t)
	index := slices.Index(names, param("oldPath"))
	newPath := param("newPath")
	if index < 0 || newPath == "" || slices.Contains(names, newPath) || path.Clean(newPath) != newPath {
		w.WriteHeader(http.StatusConflict)
		return
	}
	from := s.diskFiles(t)[index]
	save := s.anchor(t)
	if t.staleLists == 0 {
		t.staleNames = slices.Clone(names)
	}
	t.staleLists = s.StaleFileLists
	if t.add != nil && t.add.seedMode && t.add.missing {
		t.add.reload = true
	}
	names[index] = newPath
	t.Layout, t.Files = NoRootFolder, names
	to := filepath.Join(onHost(save), filepath.FromSlash(newPath))
	if _, err := os.Stat(from); err == nil {
		assert.NoError(s.t, os.MkdirAll(filepath.Dir(to), 0o755))
		assert.NoError(s.t, os.Rename(from, to))
	}
}

func (s *Server) stop(t *Torrent) {
	if t.add != nil {
		t.add.stopped = true
	}
}

func (s *Server) Stopped(hash string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.torrents[hash]
	return assert.True(s.t, ok && t.add != nil, "no added torrent %s", hash) && t.add.stopped
}

func (s *Server) Edit(hash string, edit func(t *Torrent)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.torrents[hash]; assert.True(s.t, ok, "no torrent %s", hash) {
		edit(t)
	}
}

func (s *Server) Has(hash string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.torrents[hash]
	return ok
}
