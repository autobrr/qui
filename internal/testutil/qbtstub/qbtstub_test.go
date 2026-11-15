package qbtstub

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

// The category edit tests in internal/qbittorrent depend on the stub editing
// a category the way each qBittorrent version does.
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
