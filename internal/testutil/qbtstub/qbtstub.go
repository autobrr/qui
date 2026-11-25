// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbtstub

import (
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Masterminds/semver/v3"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type Layout uint8

const (
	RootFolder Layout = iota
	SingleFile
	NoRootFolder
)

type Torrent struct {
	Hash         string
	Name         string
	SavePath     string
	DownloadPath string
	Category     string
	AutoTMM      bool
	Incomplete   bool
	Layout       Layout
	Tags         []string
	Files        []string
	InfohashV1   string
	InfohashV2   string
	Popularity   float64
	Private      bool
	NoMetadata   bool
	AddedOn      int64
	Uploaded     int64
	Peers        []qbt.TorrentPeer
	Moving       bool

	movingFrom *Torrent
	staleNames []string
	staleLists int
	add        *added
}

type Server struct {
	*httptest.Server
	t                   *testing.T
	mu                  sync.Mutex
	rid                 int
	torrents            map[string]*Torrent
	categories          map[string]string
	addedTags           map[string][]string
	downloadPaths       map[string]*qbt.CategoryDownloadPath
	savePath            string
	ConflictAfter       int
	Reject              int
	SlowMoves           bool
	ScanDirs            map[string]any
	DownloadPath        string
	DownloadPathOff     bool
	SubcategoriesOff    bool
	AutoManagement      bool
	RefreshInterval     int
	StaleSyncs          int
	Platform            string
	Libtorrent          string
	CategoriesStatus    int
	WebAPIVersion       string
	WebAPIVersionStatus int
	HangWebAPIVersion   bool
	PartialUpdates      bool
	LeaveOutNew         bool
	FailSyncs           int
	CheckSyncs          int
	ListAfterSyncs      int
	StaleFileLists      int
	HashCheck           bool
	IncompleteFilesExt  bool
	OnAdd               func(hash string)
	DropAddAnswers      int
	OnWrite             func(endpoint string)
	OnSync              func()
	ListenPort          int
	BittorrentProtocol  int
	FreeSpace           int64
	Unreachable         bool

	swarm                  *Swarm
	externalV4, externalV6 string

	calls         map[string]int
	writes        map[string]int
	conflicts     map[string]int
	bodies        map[string]string
	params        map[string]url.Values
	writeLog      []string
	requests      int
	syncs         int
	synced        map[int]chan struct{}
	versionReads  int
	freeSpacePath string
	listed        map[string]bool
}

func New(t *testing.T, savePath string) *Server {
	s := &Server{t: t, torrents: map[string]*Torrent{}, categories: map[string]string{}, addedTags: map[string][]string{}, downloadPaths: map[string]*qbt.CategoryDownloadPath{},
		savePath: savePath, calls: map[string]int{}, writes: map[string]int{}, conflicts: map[string]int{}, bodies: map[string]string{},
		params: map[string]url.Values{}, synced: map[int]chan struct{}{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *Server) AddCategory(name, savePath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.categories[name] = savePath
}

func (s *Server) SetCategoryDownloadPath(name string, option *qbt.CategoryDownloadPath) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if option == nil {
		delete(s.downloadPaths, name)
		return
	}
	s.downloadPaths[name] = option
}

func (s *Server) CategoryDownloadPath(name string) *qbt.CategoryDownloadPath {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.downloadPaths[name]
}

func (s *Server) categoryJSON(name string) map[string]any {
	c := map[string]any{"name": name, "savePath": s.categories[name]}
	if option, ok := s.downloadPaths[name]; ok {
		c["download_path"] = option
	}
	return c
}

func (s *Server) SetExternalAddress(v4, v6 string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.externalV4, s.externalV6 = v4, v6
}

func (s *Server) SetDefaultSavePath(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.savePath = p
}

func (s *Server) SetOnAdd(fn func(hash string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.OnAdd = fn
}

func (s *Server) SetOnWrite(fn func(endpoint string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.OnWrite = fn
}

func (s *Server) SetRefreshInterval(ms int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RefreshInterval = ms
}

func (s *Server) DefaultSavePath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.savePath
}

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

func (s *Server) DeleteElsewhere(hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.torrents[hash]; ok {
		s.remove(t)
		delete(s.torrents, hash)
	}
}

func (s *Server) DeleteElsewhereKeepFiles(hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.torrents, hash)
}

func (s *Server) Writes(endpoint string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes[endpoint]
}

func (s *Server) Conflicts(endpoint string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conflicts[endpoint]
}

func (s *Server) Calls(endpoint string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[endpoint]
}

func (s *Server) Requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

func (s *Server) RequestsWithout(endpoint string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests - s.calls[endpoint]
}

func (s *Server) Syncs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncs
}

func (s *Server) Synced(n int) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.synced[n]
	if !ok {
		ch = make(chan struct{})
		s.synced[n] = ch
		if s.syncs >= n {
			close(ch)
		}
	}
	return ch
}

func (s *Server) FreeSpacePath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.freeSpacePath
}

func (s *Server) VersionReads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.versionReads
}

func (s *Server) Tags(hash string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Sorted(slices.Values(s.torrents[hash].Tags))
}

func (s *Server) AddedTags(hash string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Sorted(slices.Values(s.addedTags[hash]))
}

func (s *Server) LastBody(endpoint string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[endpoint]
}

func (s *Server) LastParams(endpoint string) url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.params[endpoint].Clone()
}

func (s *Server) WriteLog() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.writeLog)
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
	return join(s.anchor(t), t.Name)
}

func (s *Server) diskFiles(t *Torrent) []string {
	content := onHost(s.contentPath(t))
	if t.Layout == SingleFile {
		return []string{content}
	}
	files := make([]string, 0, len(t.Files))
	for _, f := range t.Files {
		files = append(files, filepath.Join(content, filepath.FromSlash(f)))
	}
	return files
}

func onHost(p string) string {
	if native, ok := strings.CutPrefix(p, "/"); ok && filepath.VolumeName(native) != "" {
		return filepath.FromSlash(native)
	}
	return p
}

func join(dir, name string) string {
	if onHost(dir) != dir {
		return path.Join(dir, name)
	}
	return filepath.Join(dir, filepath.FromSlash(name))
}

func (s *Server) fileNames(t *Torrent) []string {
	switch t.Layout {
	case SingleFile:
		return []string{t.Name}
	case RootFolder:
		names := make([]string, 0, len(t.Files))
		for _, f := range t.Files {
			names = append(names, path.Join(t.Name, f))
		}
		return names
	case NoRootFolder:
	}
	return slices.Clone(t.Files)
}

func (s *Server) apiFiles(t *Torrent) []map[string]any {
	names := s.fileNames(t)
	if t.staleLists > 0 {
		t.staleLists--
		names = t.staleNames
	}
	out := make([]map[string]any, 0, len(names))
	for i, name := range names {
		size, priority := int64(1), 1
		if t.add != nil {
			size, priority = t.add.sizes[i], t.add.priorities[i]
		}
		out = append(out, map[string]any{"index": i, "name": name, "size": size, "progress": 1, "priority": priority})
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
	return join(s.savePath, name)
}

func (s *Server) move(t *Torrent, dest string) {
	from := *t
	t.SavePath, t.DownloadPath = dest, ""
	s.relocate(&from, t)
}

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
	old, files := s.diskFiles(from), s.diskFiles(to)
	if slices.Equal(old, files) {
		return
	}
	for i, f := range files {
		assert.NoError(s.t, os.MkdirAll(filepath.Dir(f), 0o755))
		assert.NoError(s.t, os.Rename(old[i], f))
	}
	if from.Layout == RootFolder {
		assert.NoError(s.t, os.RemoveAll(onHost(s.contentPath(from))))
	}
}

func (s *Server) remove(t *Torrent) {
	if t.Layout == RootFolder {
		assert.NoError(s.t, os.RemoveAll(onHost(s.contentPath(t))))
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
	s.calls[endpoint]++
	if r.Method == http.MethodPost && endpoint != "auth/login" {
		body, err := io.ReadAll(r.Body)
		assert.NoError(s.t, err)
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		s.bodies[endpoint] = string(body)
		s.writes[endpoint]++
		if s.OnWrite != nil {
			s.OnWrite(endpoint)
		}
		if s.Reject != 0 {
			w.WriteHeader(s.Reject)
			return
		}
	}
	params := r.URL.Query()
	var form *multipart.Form
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		params = r.PostForm
		if mr, err := r.MultipartReader(); err == nil {
			if form, err = mr.ReadForm(1 << 20); err == nil {
				params = form.Value
			}
		}
		if endpoint != "auth/login" {
			s.params[endpoint] = params
			s.writeLog = append(s.writeLog, endpoint)
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
		s.versionReads++
		if s.HangWebAPIVersion {
			<-r.Context().Done()
			return
		}
		if s.WebAPIVersionStatus != 0 {
			w.WriteHeader(s.WebAPIVersionStatus)
			return
		}
		_, _ = io.WriteString(w, s.webAPIVersion())
	case "app/version":
		_, _ = io.WriteString(w, "v5.1.0")
	case "app/buildInfo":
		if s.Platform == "" && s.Libtorrent == "" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"platform": s.Platform, "libtorrent": s.Libtorrent})
	case "app/preferences":
		downloadPath := s.DownloadPath
		if downloadPath == "" {
			downloadPath = join(s.savePath, "incomplete")
		}
		writeJSON(w, map[string]any{"save_path": s.savePath, "temp_path": downloadPath,
			"temp_path_enabled": !s.DownloadPathOff, "use_subcategories": !s.SubcategoriesOff, "auto_tmm_enabled": s.AutoManagement, "torrent_changed_tmm_enabled": true,
			"category_changed_tmm_enabled": true, "scan_dirs": s.ScanDirs, "listen_port": s.ListenPort,
			"bittorrent_protocol": s.BittorrentProtocol, "refresh_interval": cmp.Or(s.RefreshInterval, 1)})
	case "sync/maindata":
		s.syncs++
		if ch, ok := s.synced[s.syncs]; ok {
			close(ch)
		}
		if s.OnSync != nil {
			s.OnSync()
		}
		if s.syncs <= s.FailSyncs {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
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
			var state string
			if t.add != nil {
				var listed bool
				if state, progress, listed = s.addedRow(t); !listed {
					continue
				}
			}
			if t.Moving {
				state = string(qbt.TorrentStateMoving)
			}
			row := map[string]any{"name": t.Name, "save_path": t.SavePath, "download_path": t.DownloadPath,
				"content_path": s.contentPath(content), "category": t.Category, "auto_tmm": t.AutoTMM,
				"progress": progress, "infohash_v1": t.InfohashV1, "infohash_v2": t.InfohashV2,
				"tags": strings.Join(t.Tags, ", "), "added_on": t.AddedOn, "uploaded": t.Uploaded}
			if state != "" {
				row["state"] = state
			}
			if t.add != nil {
				var size int64
				for _, n := range t.add.sizes {
					size += n
				}
				row["size"], row["amount_left"] = size, t.add.amountLeft()
				if t.add.allTimeLate > 0 {
					t.add.allTimeLate--
				} else {
					t.add.allTime = t.add.downloaded
				}
				row["downloaded"], row["downloaded_session"] = t.add.allTime, t.add.downloaded
			}
			if s.webAPIAtLeast("2.11.2") {
				row["popularity"], row["has_metadata"] = t.Popularity, !t.NoMetadata
				if t.NoMetadata {
					row["private"] = nil
				} else {
					row["private"] = t.Private
				}
			}
			torrents[h] = row
		}
		categories := map[string]any{}
		for name := range s.categories {
			categories[name] = s.categoryJSON(name)
		}
		full := !s.PartialUpdates || param("rid") == "0"
		if full {
			s.listed = map[string]bool{}
			for h := range torrents {
				s.listed[h] = true
			}
		} else if s.LeaveOutNew {
			for h, row := range torrents {
				if !s.listed[h] {
					delete(row.(map[string]any), "added_on")
				}
			}
		}
		writeJSON(w, map[string]any{"rid": s.rid, "full_update": full, "torrents": torrents, "categories": categories,
			"server_state": map[string]any{"connection_status": "connected",
				"last_external_address_v4": s.externalV4, "last_external_address_v6": s.externalV6, "free_space_on_disk": s.FreeSpace}})
	case "sync/torrentPeers":
		t, ok := s.torrents[strings.ToLower(param("hash"))]
		if !ok {
			http.NotFound(w, r)
			return
		}
		peers := map[string]qbt.TorrentPeer{}
		for _, p := range t.Peers {
			peers[net.JoinHostPort(p.IP, strconv.Itoa(p.Port))] = p
		}
		writeJSON(w, map[string]any{"rid": 1, "full_update": true, "peers": peers})
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
	case "torrents/addTags":
		for _, t := range s.selected(param("hashes"), "|") {
			for tag := range strings.SplitSeq(param("tags"), ",") {
				if !slices.Contains(t.Tags, tag) {
					t.Tags = append(t.Tags, tag)
				}
			}
		}
	case "torrents/removeTags":
		for _, t := range s.selected(param("hashes"), "|") {
			if param("tags") == "" {
				t.Tags = nil
				continue
			}
			for tag := range strings.SplitSeq(param("tags"), ",") {
				t.Tags = slices.DeleteFunc(t.Tags, func(have string) bool { return have == tag })
			}
		}
	case "torrents/setTags":
		if !s.webAPIAtLeast("2.11.4") {
			http.NotFound(w, r)
			return
		}
		for _, t := range s.selected(param("hashes"), "|") {
			t.Tags = nil
			if param("tags") != "" {
				t.Tags = strings.Split(param("tags"), ",")
			}
		}
	case "torrents/add":
		s.add(w, form, param)
	case "torrents/filePrio":
		s.filePrio(w, param)
	case "torrents/start", "torrents/resume":
		for _, t := range s.selected(param("hashes"), "|") {
			s.start(t, false)
		}
	case "torrents/setForceStart":
		for _, t := range s.selected(param("hashes"), "|") {
			if param("value") == "true" {
				s.start(t, true)
			}
		}
	case "torrents/stop", "torrents/pause":
		for _, t := range s.selected(param("hashes"), "|") {
			s.stop(t)
		}
	case "torrents/export":
		s.export(w, param)
	case "torrents/pieceStates":
		s.pieceStatesOf(w, param)
	case "torrents/addPeers":
		s.addPeers(w, param)
	case "app/getFreeSpaceAtPath":
		s.freeSpaceAt(w, r)
	case "torrents/renameFile":
		s.renameFile(w, param)
	case "torrents/recheck":
		for _, t := range s.selected(param("hashes"), "|") {
			s.recheck(t)
		}
	case "torrents/setShareLimits":
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
			savePath = param("savePath")
			if savePath != "" {
				savePath = path.Clean(savePath)
			}
		}
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
