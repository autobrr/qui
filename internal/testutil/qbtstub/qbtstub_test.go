package qbtstub

import (
	"bytes"
	"encoding/json/v2"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestEditCategoryFollowsTheVersion(t *testing.T) {
	edit := func(t *testing.T, s *Server, form url.Values) int {
		t.Helper()
		resp, err := http.Post(s.URL+"/api/v2/torrents/editCategory", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		return resp.StatusCode
	}
	inc := &qbt.CategoryDownloadPath{Enabled: true, Path: "/inc"}
	saveOnly := url.Values{"category": {"c"}, "savePath": {"/new"}}

	t.Run("5.1 replaces every option", func(t *testing.T) {
		s := New(t, "/data")
		s.AddCategory("c", "/old")
		s.SetCategoryDownloadPath("c", inc)
		require.Equal(t, http.StatusOK, edit(t, s, saveOnly))
		require.Nil(t, s.CategoryDownloadPath("c"))
	})
	t.Run("5.1 refuses an edit that changes nothing", func(t *testing.T) {
		s := New(t, "/data")
		s.AddCategory("c", "/old")
		require.Equal(t, http.StatusConflict, edit(t, s, url.Values{"category": {"c"}, "savePath": {"/old/"}}))
		require.Equal(t, 1, s.Conflicts("torrents/editCategory"))
	})
	t.Run("5.2 replaces every option", func(t *testing.T) {
		s := New(t, "/data")
		s.WebAPIVersion = "2.15.1"
		s.AddCategory("c", "/old")
		s.SetCategoryDownloadPath("c", &qbt.CategoryDownloadPath{})
		require.Equal(t, http.StatusOK, edit(t, s, saveOnly))
		require.Nil(t, s.CategoryDownloadPath("c"))
	})
	t.Run("5.3 keeps what the edit leaves out", func(t *testing.T) {
		s := New(t, "/data")
		s.WebAPIVersion = "2.16.1"
		s.AddCategory("c", "/old")
		s.SetCategoryDownloadPath("c", inc)
		require.Equal(t, http.StatusOK, edit(t, s, saveOnly))
		require.Equal(t, inc, s.CategoryDownloadPath("c"))
		require.Equal(t, http.StatusOK, edit(t, s, url.Values{"category": {"c"}, "downloadPathEnabled": {""}}))
		require.Nil(t, s.CategoryDownloadPath("c"))
	})
}

func TestHashCheckFindsDamagedPieces(t *testing.T) {
	dir := t.TempDir()
	s := New(t, dir)
	s.HashCheck, s.IncompleteFilesExt = true, true
	a, b := bytes.Repeat([]byte("a"), 32), bytes.Repeat([]byte("b"), 32)
	data, hash := NewContentTorrent(t, "Show.S01.1080p-GRP", 16, ContentFile{"a.bin", a}, ContentFile{"b.bin", b})
	folder := filepath.Join(dir, "Show.S01.1080p-GRP")
	require.NoError(t, os.MkdirAll(folder, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(folder, "a.bin"), a, 0o600))
	damaged := bytes.Clone(b)
	damaged[20] ^= 0xff
	require.NoError(t, os.WriteFile(filepath.Join(folder, "b.bin"), damaged, 0o600))

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("torrents", "x.torrent")
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, form.WriteField("savepath", dir))
	require.NoError(t, form.WriteField("stopCondition", "FilesChecked"))
	require.NoError(t, form.Close())
	resp, err := http.Post(s.URL+"/api/v2/torrents/add", form.FormDataContentType(), &body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp, err = http.Get(s.URL + "/api/v2/sync/maindata?rid=0")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	resp, err = http.Get(s.URL + "/api/v2/torrents/pieceStates?hash=" + hash)
	require.NoError(t, err)
	var states []int
	require.NoError(t, json.UnmarshalRead(resp.Body, &states))
	require.NoError(t, resp.Body.Close())

	require.Equal(t, []int{2, 2, 2, 0}, states)
	require.True(t, s.Stopped(hash))
	require.FileExists(t, filepath.Join(folder, "a.bin"))
	require.FileExists(t, filepath.Join(folder, "b.bin.!qB"))
	require.NoFileExists(t, filepath.Join(folder, "b.bin"))
}
