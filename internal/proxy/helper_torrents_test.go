// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// helperRegistry names the helper torrents of the rig's instance.
type helperRegistry map[string]string

func (h helperRegistry) HelperKind(_ int, hash string) string { return h[hash] }

// newHelperProxyRig has a helper torrent (proxyHashA) and a torrent of the
// user (proxyHashB).
func newHelperProxyRig(t *testing.T) *proxyRig {
	t.Helper()
	r := newProxyRig(t)
	own := r.perRelease(proxyHashB, "tv")
	own.SavePath = r.p("torrents/tv/Show.S01E02-GRP")
	r.add(r.perRelease(proxyHashA, ""), own)
	r.handler.syncManager.SetHelperTorrents(helperRegistry{proxyHashA: "transfer"})
	return r
}

// A proxy client such as Sonarr cannot delete the files of a helper torrent:
// they are the files of a real torrent. It can delete the helper torrent and
// keep its files.
func TestProxyRefusesDeleteWithFilesOfAHelperTorrent(t *testing.T) {
	t.Parallel()
	r := newHelperProxyRig(t)
	multipart := "--rig\r\nContent-Disposition: form-data; name=\"hashes\"\r\n\r\n" + proxyHashA + "\r\n" +
		"--rig\r\nContent-Disposition: form-data; name=\"deleteFiles\"\r\n\r\ntrue\r\n--rig--\r\n"
	requests := map[string]struct{ contentType, body string }{
		"helper and own":   {"application/x-www-form-urlencoded", "hashes=" + proxyHashB + "|" + proxyHashA + "&deleteFiles=true"},
		"all":              {"application/x-www-form-urlencoded", "hashes=all&deleteFiles=TRUE"},
		"multipart helper": {"multipart/form-data; boundary=rig", multipart},
	}
	for name, req := range requests {
		rec := r.postAs(r.handler.handleDeleteTorrents, "torrents/delete", req.contentType, req.body)

		require.Equal(t, http.StatusConflict, rec.Code, name)
		require.Contains(t, rec.Body.String(), "qui added this torrent", name)
	}
	require.Zero(t, r.stub.Calls("torrents/delete"), "qBittorrent gets no delete")

	rec := r.post(r.handler.handleDeleteTorrents, "torrents/delete", "hashes="+proxyHashA+"&deleteFiles=false")
	require.Equal(t, http.StatusOK, rec.Code, "a delete that keeps the files")
	require.Equal(t, 1, r.stub.Calls("torrents/delete"))
}

// Only the registry makes a helper torrent: without a row, the user's torrent
// is deleted with its files.
func TestProxyDeletesTheFilesOfATorrentThatIsNoHelper(t *testing.T) {
	t.Parallel()
	r := newHelperProxyRig(t)

	rec := r.post(r.handler.handleDeleteTorrents, "torrents/delete", "hashes="+proxyHashB+"&deleteFiles=true")

	require.Equal(t, http.StatusOK, rec.Code)
	r.requireGone("torrents/tv/Show.S01E02-GRP")
}
