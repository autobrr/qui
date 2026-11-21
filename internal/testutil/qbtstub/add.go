// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbtstub

import (
	"bytes"
	"cmp"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TorrentFile is one file of a .torrent that NewTorrentFile builds.
type TorrentFile struct {
	Path string
	Size int64
}

// NewTorrentFile builds a .torrent whose files sit in the folder name, and
// returns it with its infohash.
func NewTorrentFile(t *testing.T, name string, files ...TorrentFile) (data []byte, hash string) {
	t.Helper()
	info := metainfo.Info{Name: name, PieceLength: 16384, Pieces: make([]byte, metainfo.HashSize)}
	for _, f := range files {
		info.Files = append(info.Files, metainfo.FileInfo{Path: strings.Split(f.Path, "/"), Length: f.Size})
	}
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	mi := metainfo.MetaInfo{InfoBytes: infoBytes}
	var buf bytes.Buffer
	require.NoError(t, mi.Write(&buf))
	return buf.Bytes(), mi.HashInfoBytes().HexString()
}

// added is the state of a torrent that came in through torrents/add.
type added struct {
	// torrent is the .torrent file that torrents/export gives back.
	torrent     []byte
	pieceLength int64
	pieces      []byte
	sizes       []int64
	priorities  []int
	stopped     bool
	forced      bool
	seedMode    bool
	// stopWhenChecked is the stop condition "FilesChecked".
	stopWhenChecked bool
	// pieceStates is what the last hash check found, nil before one.
	pieceStates []int
	// checked is set once a check ran, or at the add in seed mode.
	checked  bool
	checking bool
	reload   bool
	// checkLeft counts the syncs that still report the running check.
	checkLeft int
	// progress is what the last check found.
	progress float64
	missing  bool
	// listAfter counts the syncs that still leave the torrent out.
	listAfter int
	// staleRows counts the syncs after a recheck that still report
	// staleState and staleProgress, the row from before it.
	staleRows     int
	staleState    string
	staleProgress float64
	// downloaded is the bytes that the torrent got from its peers.
	downloaded int64
	// allTime is the count of all sessions, which qBittorrent reports as
	// downloaded. qBittorrent adds the bytes of a session to it about once a
	// second, so it is late when a download ends: allTimeLate counts the
	// syncs that still report the count from before the last bytes.
	allTime     int64
	allTimeLate int
}

// add answers torrents/add for uploaded .torrent files, as qBittorrent 5
// does: it refuses file priorities in an upload from WebAPI 2.11.9, a torrent
// it already has with 409, and bytes that are no torrent with 415. Seed mode
// takes the files on trust, but finds missing wanted files at the add, also
// when the torrent is stopped. A stopped torrent without seed mode is not
// checked until it starts or a recheck.
func (s *Server) add(w http.ResponseWriter, form *multipart.Form, param func(string) string) {
	if param("filePriorities") != "" && s.webAPIAtLeast("2.11.9") {
		http.Error(w, "Cannot specify filePriorities when uploading torrent files", http.StatusBadRequest)
		return
	}
	if form == nil || len(form.File["torrents"]) == 0 {
		http.Error(w, "no torrent file", http.StatusBadRequest)
		return
	}
	var ids []string
	for _, header := range form.File["torrents"] {
		t, ok := parseUpload(header)
		if !ok {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		if _, exists := s.torrents[t.Hash]; exists {
			continue
		}
		if s.OnAdd != nil {
			s.OnAdd(t.Hash)
		}
		s.place(t, param)
		s.torrents[t.Hash] = t
		s.addedTags[t.Hash] = slices.Clone(t.Tags)
		ids = append(ids, t.Hash)
	}
	if len(ids) == 0 {
		w.WriteHeader(http.StatusConflict)
		return
	}
	if s.DropAddAnswers > 0 {
		s.DropAddAnswers--
		panic(http.ErrAbortHandler)
	}
	if !s.webAPIAtLeast("2.14.0") {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "Ok.")
		return
	}
	writeJSON(w, map[string]any{"success_count": len(ids), "failure_count": 0, "pending_count": 0, "added_torrent_ids": ids})
}

func parseUpload(header *multipart.FileHeader) (*Torrent, bool) {
	file, err := header.Open()
	if err != nil {
		return nil, false
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, false
	}
	return parseTorrent(data)
}

func parseTorrent(data []byte) (*Torrent, bool) {
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, false
	}
	t := &Torrent{Hash: mi.HashInfoBytes().HexString(), Name: info.BestName(), Layout: SingleFile,
		add: &added{torrent: data, pieceLength: info.PieceLength, pieces: info.Pieces}}
	if info.IsDir() {
		t.Layout = RootFolder
	}
	for _, f := range info.UpvertedFiles() {
		if info.IsDir() {
			t.Files = append(t.Files, strings.Join(f.BestPath(), "/"))
		}
		t.add.sizes = append(t.add.sizes, f.Length)
		t.add.priorities = append(t.add.priorities, 1)
	}
	return t, true
}

// place applies the add parameters to a new torrent.
func (s *Server) place(t *Torrent, param func(string) string) {
	a := t.add
	t.Category = param("category")
	t.AutoTMM = param("autoTMM") == "true"
	t.SavePath = param("savepath")
	if t.SavePath == "" || t.AutoTMM {
		t.SavePath = s.categoryPath(t.Category)
	}
	if tags := param("tags"); tags != "" {
		t.Tags = strings.Split(tags, ",")
	}
	switch layout := param("contentLayout"); {
	case t.Layout == RootFolder && layout == "NoSubfolder":
		t.Layout = NoRootFolder
	case t.Layout == SingleFile && layout == "Subfolder":
		// qBittorrent names the folder after the file, without its extension.
		folder, _, _ := strings.CutLast(t.Name, ".")
		t.Layout, t.Files = NoRootFolder, []string{folder + "/" + t.Name}
	}
	a.stopped = param("stopped") == "true" || param("paused") == "true"
	a.forced = param("forced") == "true"
	a.seedMode = param("seedMode") == "true" || param("skip_checking") == "true"
	a.stopWhenChecked = param("stopCondition") == "FilesChecked"
	a.listAfter = s.ListAfterSyncs
	if a.seedMode {
		a.checked, a.progress = true, 1
		a.missing = s.onDisk(t) < 1
	}
	if !a.stopped {
		s.run(t)
	}
}

// run starts a torrent: seed mode looks for missing files again, as
// qBittorrent loads a torrent with missing files again at its start, and
// checks the files that a rename gave it. A torrent never checked is checked.
func (s *Server) run(t *Torrent) {
	a := t.add
	switch {
	case a.seedMode:
		a.missing = s.onDisk(t) < 1
		if a.reload && !a.missing {
			a.seedMode = false
			s.startCheck(t)
		}
	case !a.checked && !a.checking:
		s.startCheck(t)
	}
}

// onDisk is the share of the wanted bytes whose files are on disk at their
// size.
func (s *Server) onDisk(t *Torrent) float64 {
	var have, want int64
	for i, f := range s.diskFiles(t) {
		if t.add.priorities[i] == 0 {
			continue
		}
		want += t.add.sizes[i]
		if info, err := os.Stat(f); err == nil && info.Size() == t.add.sizes[i] {
			have += t.add.sizes[i]
		}
	}
	if want == 0 {
		return 1
	}
	return float64(have) / float64(want)
}

func (s *Server) startCheck(t *Torrent) {
	t.add.checking, t.add.checkLeft = true, s.CheckSyncs
}

// addedRow is the state and progress that a sync reports for an added
// torrent. listed is false while the torrent is not listed yet.
func (s *Server) addedRow(t *Torrent) (state string, progress float64, listed bool) {
	a := t.add
	if a.listAfter > 0 {
		a.listAfter--
		return "", 0, false
	}
	if a.staleRows > 0 {
		a.staleRows--
		return a.staleState, a.staleProgress, true
	}
	if a.checking {
		if a.checkLeft > 0 {
			a.checkLeft--
			if a.reload {
				return "checkingUP", 1, true
			}
			return "checkingDL", 0, true
		}
		a.checking, a.checked, a.reload, a.progress = false, true, false, s.check(t)
		if a.stopWhenChecked {
			a.stopped = true
		}
	}
	state, progress = a.row()
	return state, progress, true
}

// row is the state and progress of a torrent that runs no check.
func (a *added) row() (state string, progress float64) {
	switch {
	case a.missing:
		return "missingFiles", 0
	case !a.checked:
		return "stoppedDL", 0
	}
	suffix := "DL"
	if a.progress >= 1 {
		suffix = "UP"
	}
	switch {
	case a.stopped:
		return "stopped" + suffix, a.progress
	case a.forced:
		return "forced" + suffix, a.progress
	}
	return "stalled" + suffix, a.progress
}

// amountLeft is the bytes of the pieces that the last check did not pass,
// as qBittorrent reports amount_left. It ignores file priorities.
func (a *added) amountLeft() int64 {
	var total int64
	for _, size := range a.sizes {
		total += size
	}
	switch {
	case a.progress >= 1:
		return 0
	case a.pieceStates == nil:
		return total
	}
	var left int64
	for i, state := range a.pieceStates {
		if state != 2 {
			left += min(a.pieceLength, total-int64(i)*a.pieceLength)
		}
	}
	return left
}

// start answers torrents/start and torrents/setForceStart.
func (s *Server) start(t *Torrent, forced bool) {
	if t.add == nil {
		return
	}
	t.add.stopped, t.add.forced = false, forced
	s.run(t)
}

// recheck answers torrents/recheck, which ends seed mode. A stopped torrent
// stays stopped after the check. The next sync, or the next StaleSyncs syncs,
// still report the state from before the recheck, as qBittorrent does until
// its next refresh.
func (s *Server) recheck(t *Torrent) {
	a := t.add
	if a == nil {
		return
	}
	if !a.checking {
		a.staleState, a.staleProgress = a.row()
		a.staleRows = cmp.Or(s.StaleSyncs, 1)
	}
	a.seedMode, a.missing = false, false
	s.startCheck(t)
}

// filePrio answers torrents/filePrio as qBittorrent 5 does.
func (s *Server) filePrio(w http.ResponseWriter, param func(string) string) {
	t, ok := s.torrents[strings.ToLower(param("hash"))]
	if !ok || t.add == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	priority, err := strconv.Atoi(param("priority"))
	if err != nil || !slices.Contains([]int{0, 1, 6, 7}, priority) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	for id := range strings.SplitSeq(param("id"), "|") {
		index, err := strconv.Atoi(id)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if index < 0 || index >= len(t.add.priorities) {
			w.WriteHeader(http.StatusConflict)
			return
		}
		t.add.priorities[index] = priority
	}
}

// FilePriorities returns the file priorities of a torrent that came in
// through torrents/add.
func (s *Server) FilePriorities(hash string) []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.torrents[hash]
	if !assert.True(s.t, ok && t.add != nil, "no added torrent %s", hash) {
		return nil
	}
	return slices.Clone(t.add.priorities)
}
