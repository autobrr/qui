// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package qbtstub is a small qBittorrent WebAPI over a temp dir. Deletes and
// moves happen on disk the way qBittorrent and libtorrent do them, so tests can
// assert what is left behind.
package qbtstub

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Masterminds/semver/v3"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Layout is how a torrent's files sit under its save path.
type Layout uint8

const (
	// RootFolder: the files sit in save/Name.
	RootFolder Layout = iota
	// SingleFile: the one file is save/Name.
	SingleFile
	// NoRootFolder: the files sit directly in the save path.
	NoRootFolder
)

// Torrent is one torrent in the stub. Files are slash names below the root
// folder, or below the save path without one; a single file has none.
type Torrent struct {
	Hash         string
	Name         string
	SavePath     string
	DownloadPath string
	Category     string
	AutoTMM      bool
	// Incomplete is a torrent still downloading, which setDownloadPath moves.
	Incomplete bool
	Layout     Layout
	Files      []string
	// InfohashV1 and InfohashV2 are reported as the WebAPI does; Hash alone
	// names the torrent in a request.
	InfohashV1 string
	InfohashV2 string

	// movingFrom is where the content still sits during a slow move.
	movingFrom *Torrent
}

// Server answers the WebAPI calls qui makes around a delete or a move.
type Server struct {
	*httptest.Server
	t          *testing.T
	mu         sync.Mutex
	rid        int
	torrents   map[string]*Torrent
	categories map[string]string
	// downloadPaths holds each category's download path option, with no
	// entry when the category inherits it.
	downloadPaths map[string]*qbt.CategoryDownloadPath
	savePath      string
	// ConflictAfter, when set, makes setCategory change that many torrents
	// and then answer 409, as qBittorrent does when it throws part way
	// through the list.
	ConflictAfter int
	// Reject, when set, answers every write with this status and changes
	// nothing.
	Reject int
	// SlowMoves, when set, makes the moves of torrents with ATM on wait for
	// FinishMoves: as in qBittorrent, the save path changes at once and the
	// content path once the files have moved.
	SlowMoves bool
	// ScanDirs is reported as the scan_dirs preference: each monitored
	// folder maps to 0 (save there), 1 (the default save path) or a path.
	ScanDirs map[string]any
	// DownloadPath, when set, replaces the reported temp_path
	// <save path>/incomplete. DownloadPathOff turns it off.
	DownloadPath    string
	DownloadPathOff bool
	// SubcategoriesOff reports use_subcategories turned off.
	SubcategoriesOff bool
	// Platform, when set, makes app/buildInfo answer with this build
	// platform. Without it the endpoint answers 404.
	Platform string
	// CategoriesStatus, when set, makes torrents/categories answer with it.
	CategoriesStatus int
	// WebAPIVersion, when set, replaces the reported 2.11.4 (qBittorrent
	// 5.1). Category edits follow it: up to 5.1 an edit that changes nothing
	// or names a missing category answers 409 (404 later), up to 5.2 an edit
	// replaces every option, and from 2.16.1 (5.3) it keeps the options it
	// leaves out.
	WebAPIVersion string
	// WebAPIVersionStatus, when set, makes app/webapiVersion answer with it.
	WebAPIVersionStatus int

	writes    map[string]int
	conflicts map[string]int
	bodies    map[string]string
	requests  int
	syncs     int
}

// New starts a stub whose default save path is savePath.
func New(t *testing.T, savePath string) *Server {
	s := &Server{t: t, torrents: map[string]*Torrent{}, categories: map[string]string{}, downloadPaths: map[string]*qbt.CategoryDownloadPath{},
		savePath: savePath, writes: map[string]int{}, conflicts: map[string]int{}, bodies: map[string]string{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// AddCategory adds a category; an empty path inherits the default save path.
func (s *Server) AddCategory(name, savePath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.categories[name] = savePath
}

// SetCategoryDownloadPath sets a category's download path option, nil to
// make it inherit.
func (s *Server) SetCategoryDownloadPath(name string, option *qbt.CategoryDownloadPath) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if option == nil {
		delete(s.downloadPaths, name)
		return
	}
	s.downloadPaths[name] = option
}

// CategoryDownloadPath returns a category's download path option, nil when it
// inherits.
func (s *Server) CategoryDownloadPath(name string) *qbt.CategoryDownloadPath {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.downloadPaths[name]
}

// categoryJSON is a category as qBittorrent 5.1 sends it, which leaves
// download_path out when the category inherits it.
func (s *Server) categoryJSON(name string) map[string]any {
	c := map[string]any{"name": name, "savePath": s.categories[name]}
	if option, ok := s.downloadPaths[name]; ok {
		c["download_path"] = option
	}
	return c
}

// SetDefaultSavePath changes the default save path, as an edit in
// qBittorrent's own WebUI does, without qui seeing the request.
func (s *Server) SetDefaultSavePath(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.savePath = p
}

// DefaultSavePath returns the reported default save path.
func (s *Server) DefaultSavePath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.savePath
}

// Add adds a torrent and writes its files.
func (s *Server) Add(t Torrent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tt := t
	s.torrents[t.Hash] = &tt
	for _, f := range s.diskFiles(&tt) {
		require.NoError(s.t, os.MkdirAll(filepath.Dir(f), 0o755))
		require.NoError(s.t, os.WriteFile(f, []byte("x"), 0o600))
	}
}

// DeleteElsewhere deletes a torrent with its files, as a request from
// another client does, without qui seeing the request.
func (s *Server) DeleteElsewhere(hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.torrents[hash]; ok {
		s.remove(t)
		delete(s.torrents, hash)
	}
}

// Writes counts the write requests qBittorrent received on an endpoint.
func (s *Server) Writes(endpoint string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes[endpoint]
}

// Conflicts counts the writes to endpoint that answered 409.
func (s *Server) Conflicts(endpoint string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conflicts[endpoint]
}

// Requests counts every request qBittorrent received.
func (s *Server) Requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

// Syncs counts the sync/maindata requests qBittorrent received.
func (s *Server) Syncs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncs
}

// LastBody is the raw body of the last write to an endpoint.
func (s *Server) LastBody(endpoint string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[endpoint]
}

func (s *Server) anchor(t *Torrent) string {
	if t.DownloadPath != "" {
		return t.DownloadPath
	}
	return t.SavePath
}

func (s *Server) contentPath(t *Torrent) string {
	switch t.Layout {
	case NoRootFolder:
		return s.anchor(t)
	case RootFolder, SingleFile:
	}
	return filepath.Join(s.anchor(t), t.Name)
}

func (s *Server) diskFiles(t *Torrent) []string {
	if t.Layout == SingleFile {
		return []string{s.contentPath(t)}
	}
	files := make([]string, 0, len(t.Files))
	for _, f := range t.Files {
		files = append(files, filepath.Join(s.contentPath(t), filepath.FromSlash(f)))
	}
	return files
}

func (s *Server) apiFiles(t *Torrent) []map[string]any {
	var names []string
	switch t.Layout {
	case SingleFile:
		names = []string{t.Name}
	case RootFolder:
		for _, f := range t.Files {
			names = append(names, path.Join(t.Name, f))
		}
	case NoRootFolder:
		names = t.Files
	}
	out := make([]map[string]any, 0, len(names))
	for i, name := range names {
		out = append(out, map[string]any{"index": i, "name": name, "size": 1, "progress": 1, "priority": 1})
	}
	return out
}

func (s *Server) categoryPath(name string) string {
	if p := s.categories[name]; p != "" {
		return p
	}
	if name == "" {
		return s.savePath
	}
	return filepath.Join(s.savePath, filepath.FromSlash(name))
}

// move relocates the content the way libtorrent's move_storage does: file by
// file, leaving the old folders it emptied. Handlers run off the test
// goroutine, so failures are reported with assert.
func (s *Server) move(t *Torrent, dest string) {
	from := *t
	t.SavePath, t.DownloadPath = dest, ""
	s.relocate(&from, t)
}

// moveATM moves a torrent with ATM on, which SlowMoves holds back.
func (s *Server) moveATM(t *Torrent, dest string) {
	if !s.SlowMoves {
		s.move(t, dest)
		return
	}
	if t.movingFrom == nil {
		from := *t
		t.movingFrom = &from
	}
	t.SavePath, t.DownloadPath = dest, ""
}

// FinishMoves completes the moves SlowMoves held back.
func (s *Server) FinishMoves() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.torrents {
		if t.movingFrom != nil {
			s.relocate(t.movingFrom, t)
			t.movingFrom = nil
		}
	}
}

func (s *Server) relocate(from, to *Torrent) {
	old := s.diskFiles(from)
	for i, f := range s.diskFiles(to) {
		assert.NoError(s.t, os.MkdirAll(filepath.Dir(f), 0o755))
		assert.NoError(s.t, os.Rename(old[i], f))
	}
	if from.Layout == RootFolder {
		assert.NoError(s.t, os.RemoveAll(s.contentPath(from)))
	}
}

// remove deletes the files the way qBittorrent before 5.2.2 does: the root
// folder whole, but only the files of a torrent without one. A file already
// missing, such as one set to Do Not Download, is skipped.
func (s *Server) remove(t *Torrent) {
	if t.Layout == RootFolder {
		assert.NoError(s.t, os.RemoveAll(s.contentPath(t)))
		return
	}
	for _, f := range s.diskFiles(t) {
		if err := os.Remove(f); !errors.Is(err, fs.ErrNotExist) {
			assert.NoError(s.t, err)
		}
	}
}

func (s *Server) selected(list, sep string) []*Torrent {
	var out []*Torrent
	for h := range strings.SplitSeq(list, sep) {
		if h == "all" {
			for _, t := range s.torrents {
				out = append(out, t)
			}
			return out
		}
		if t, ok := s.torrents[strings.ToLower(h)]; ok {
			out = append(out, t)
		}
	}
	return out
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	endpoint := strings.TrimPrefix(r.URL.Path, "/api/v2/")
	if r.Method == http.MethodPost && endpoint != "auth/login" {
		body, err := io.ReadAll(r.Body)
		assert.NoError(s.t, err)
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		s.bodies[endpoint] = string(body)
		s.writes[endpoint]++
		if s.Reject != 0 {
			w.WriteHeader(s.Reject)
			return
		}
	}
	// qBittorrent 5 reads a GET's query, and a POST's urlencoded or multipart
	// body only. A repeated key keeps its last value.
	params := r.URL.Query()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		params = r.PostForm
		if mr, err := r.MultipartReader(); err == nil {
			if form, err := mr.ReadForm(1 << 20); err == nil {
				params = form.Value
			}
		}
	}
	param := func(key string) string {
		if v := params[key]; len(v) > 0 {
			return v[len(v)-1]
		}
		return ""
	}
	switch endpoint {
	case "auth/login":
		_, _ = io.WriteString(w, "Ok.")
	case "app/webapiVersion":
		if s.WebAPIVersionStatus != 0 {
			w.WriteHeader(s.WebAPIVersionStatus)
			return
		}
		_, _ = io.WriteString(w, s.webAPIVersion())
	case "app/version":
		_, _ = io.WriteString(w, "v5.1.0")
	case "app/buildInfo":
		if s.Platform == "" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"platform": s.Platform})
	case "app/preferences":
		downloadPath := s.DownloadPath
		if downloadPath == "" {
			downloadPath = filepath.Join(s.savePath, "incomplete")
		}
		writeJSON(w, map[string]any{"save_path": s.savePath, "temp_path": downloadPath,
			"temp_path_enabled": !s.DownloadPathOff, "use_subcategories": !s.SubcategoriesOff, "torrent_changed_tmm_enabled": true,
			"category_changed_tmm_enabled": true, "scan_dirs": s.ScanDirs})
	case "sync/maindata":
		s.syncs++
		s.rid++
		torrents := map[string]any{}
		for h, t := range s.torrents {
			content := t
			if t.movingFrom != nil {
				content = t.movingFrom
			}
			progress := 1.0
			if t.Incomplete {
				progress = 0.5
			}
			torrents[h] = map[string]any{"name": t.Name, "save_path": t.SavePath, "download_path": t.DownloadPath,
				"content_path": s.contentPath(content), "category": t.Category, "auto_tmm": t.AutoTMM,
				"progress": progress, "infohash_v1": t.InfohashV1, "infohash_v2": t.InfohashV2}
		}
		categories := map[string]any{}
		for name := range s.categories {
			categories[name] = s.categoryJSON(name)
		}
		writeJSON(w, map[string]any{"rid": s.rid, "full_update": true, "torrents": torrents, "categories": categories,
			"server_state": map[string]any{"connection_status": "connected"}})
	case "torrents/files":
		if t, ok := s.torrents[strings.ToLower(param("hash"))]; ok {
			writeJSON(w, s.apiFiles(t))
			return
		}
		http.NotFound(w, r)
	case "torrents/delete":
		for _, t := range s.selected(param("hashes"), "|") {
			if strings.EqualFold(param("deleteFiles"), "true") {
				s.remove(t)
			}
			delete(s.torrents, t.Hash)
		}
	case "torrents/setLocation":
		for _, t := range s.selected(param("hashes"), "|") {
			t.AutoTMM = false
			s.move(t, strings.TrimSpace(param("location")))
		}
	case "torrents/setSavePath":
		for _, t := range s.selected(param("id"), "|") {
			if !t.AutoTMM && t.DownloadPath == "" {
				s.move(t, param("path"))
			}
		}
	case "torrents/setDownloadPath":
		// TorrentImpl::setDownloadPath: an incomplete torrent moves into the
		// new path, or back to its save path when that is empty.
		for _, t := range s.selected(param("id"), "|") {
			p := param("path")
			if t.AutoTMM || p == t.DownloadPath {
				continue
			}
			save, dest := t.SavePath, p
			if dest == "" {
				dest = save
			}
			if t.Incomplete {
				s.move(t, dest)
			}
			t.SavePath, t.DownloadPath = save, p
		}
	case "torrents/setCategory":
		category := param("category")
		if _, ok := s.categories[category]; !ok && category != "" {
			w.WriteHeader(http.StatusConflict)
			return
		}
		for i, t := range s.selected(param("hashes"), "|") {
			if s.ConflictAfter > 0 && i == s.ConflictAfter {
				w.WriteHeader(http.StatusConflict)
				return
			}
			t.Category = category
			if t.AutoTMM {
				s.moveATM(t, s.categoryPath(category))
			}
		}
	case "torrents/setAutoManagement":
		for _, t := range s.selected(param("hashes"), "|") {
			if strings.EqualFold(param("enable"), "true") && !t.AutoTMM {
				t.AutoTMM = true
				s.moveATM(t, s.categoryPath(t.Category))
			} else {
				t.AutoTMM = strings.EqualFold(param("enable"), "true")
			}
		}
	case "torrents/categories":
		if s.CategoriesStatus != 0 {
			w.WriteHeader(s.CategoriesStatus)
			return
		}
		categories := map[string]any{}
		for name := range s.categories {
			categories[name] = s.categoryJSON(name)
		}
		writeJSON(w, categories)
	case "torrents/editCategory":
		name := param("category")
		if _, ok := s.categories[name]; !ok {
			if s.webAPIAtLeast("2.15.0") {
				w.WriteHeader(http.StatusNotFound)
			} else {
				s.conflicts[endpoint]++
				w.WriteHeader(http.StatusConflict)
			}
			return
		}
		savePath, option := s.categories[name], s.downloadPaths[name]
		_, sentSavePath := params["savePath"]
		_, sentOption := params["downloadPathEnabled"]
		partial := s.webAPIAtLeast("2.16.1")
		if sentSavePath || !partial {
			// qBittorrent stores a cleaned path.
			savePath = param("savePath")
			if savePath != "" {
				savePath = path.Clean(savePath)
			}
		}
		// Up to qBittorrent 5.2 an edit replaces every option of the category,
		// so a download path the request leaves out goes back to inherit.
		if sentOption || !partial {
			option = nil
			if enabled := param("downloadPathEnabled"); enabled == "true" || enabled == "false" {
				option = &qbt.CategoryDownloadPath{Enabled: enabled == "true", Path: param("downloadPath")}
			}
		}
		if !s.webAPIAtLeast("2.15.0") && savePath == s.categories[name] && reflect.DeepEqual(option, s.downloadPaths[name]) {
			s.conflicts[endpoint]++
			w.WriteHeader(http.StatusConflict)
			return
		}
		s.categories[name] = savePath
		if option == nil {
			delete(s.downloadPaths, name)
		} else {
			s.downloadPaths[name] = option
		}
		for _, t := range s.torrents {
			if t.AutoTMM && t.Category == name {
				s.moveATM(t, s.categoryPath(name))
			}
		}
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) webAPIVersion() string {
	if s.WebAPIVersion != "" {
		return s.WebAPIVersion
	}
	return "2.11.4"
}

func (s *Server) webAPIAtLeast(version string) bool {
	return !semver.MustParse(s.webAPIVersion()).LessThan(semver.MustParse(version))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v) //nolint:errchkjson // test stub: a failed write means the client went away, and the client side of the test reports a bad body
}
