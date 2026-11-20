// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type helperRegistry map[string]string

func (h helperRegistry) HelperKinds(int) map[string]string {
	if len(h) == 0 {
		return nil
	}
	return h
}

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

// A delete of all torrents with files sees a helper torrent that qBittorrent
// added after the last sync.
// regression: #4231
func TestProxyRefusesDeleteOfAllWithFilesAfterANewHelper(t *testing.T) {
	t.Parallel()
	r := newProxyRigWorker(t, false)
	own := r.perRelease(proxyHashB, "tv")
	own.SavePath = r.p("torrents/tv/Show.S01E02-GRP")
	r.add(own)
	r.stub.Add(r.perRelease(proxyHashA, ""))
	r.handler.syncManager.SetHelperTorrents(helperRegistry{proxyHashA: "transfer"})

	rec := r.post(r.handler.handleDeleteTorrents, "torrents/delete", "hashes=all&deleteFiles=true")

	require.Equal(t, http.StatusConflict, rec.Code)
	require.Zero(t, r.stub.Calls("torrents/delete"), "qBittorrent gets no delete")
}

// While the instance has no helper torrent, a delete of all torrents with
// files reads no maindata before it reaches qBittorrent.
// regression: #4231
func TestProxyDeleteOfAllWithFilesReadsNoMaindataWithoutAHelper(t *testing.T) {
	t.Parallel()
	r := newProxyRigWorker(t, false)
	own := r.perRelease(proxyHashB, "tv")
	own.SavePath = r.p("torrents/tv/Show.S01E02-GRP")
	r.add(own)
	r.handler.syncManager.SetHelperTorrents(helperRegistry{})
	reads := r.stub.Calls("sync/maindata")

	rec := r.post(r.handler.handleDeleteTorrents, "torrents/delete", "hashes=all&deleteFiles=true")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, r.stub.Calls("torrents/delete"))
	require.Equal(t, reads, r.stub.Calls("sync/maindata"))
}

// qui cannot tell whether a delete that it cannot read names a helper
// torrent, so it refuses the delete. qBittorrent reads a multipart body
// without its closing boundary, and a Content-Type that only starts with the
// urlencoded type.
// regression: #4231
func TestProxyRefusesADeleteThatQuiCannotRead(t *testing.T) {
	t.Parallel()
	r := newHelperProxyRig(t)
	requests := map[string]struct{ contentType, body string }{
		"multipart without its closing boundary": {"multipart/form-data; boundary=rig",
			"--rig\r\nContent-Disposition: form-data; name=\"hashes\"\r\n\r\n" + proxyHashA + "\r\n" +
				"--rig\r\nContent-Disposition: form-data; name=\"deleteFiles\"\r\n\r\ntrue\r\n"},
		"urlencoded type with a suffix": {"application/x-www-form-urlencodedx",
			"hashes=" + proxyHashA + "&deleteFiles=true&x=1;2"},
	}
	for name, req := range requests {
		rec := r.postAs(r.handler.handleDeleteTorrents, "torrents/delete", req.contentType, req.body)

		require.Equal(t, http.StatusBadRequest, rec.Code, name)
	}
	require.Zero(t, r.stub.Calls("torrents/delete"), "qBittorrent gets no delete")
}
