// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

type deleteNoteSeen struct {
	instanceID int
	hashes     []string
	actor      DeleteActor
	// deletesSent is the number of delete requests that qBittorrent got
	// before the note.
	deletesSent int
}

type fakeDeleteNoter struct {
	stub  *qbtstub.Server
	notes []deleteNoteSeen
}

func (f *fakeDeleteNoter) NoteDelete(instanceID int, hashes []string, actor DeleteActor) {
	slices.Sort(hashes)
	f.notes = append(f.notes, deleteNoteSeen{instanceID, hashes, actor, f.stub.Calls("torrents/delete")})
}

func TestDeleteTorrentsGivesTheDeleteNoteBeforeTheRequest(t *testing.T) {
	t.Parallel()
	for _, deleteFiles := range []bool{false, true} {
		t.Run(fmt.Sprintf("deleteFiles=%t", deleteFiles), func(t *testing.T) {
			t.Parallel()
			r := newTriggerRig(t)
			r.add(r.perRelease(hashA, "tv"))
			noter := &fakeDeleteNoter{stub: r.stub}
			r.sm.SetDeleteNoter(noter)
			actor := DeleteActor{Kind: DeleteActorAutomation, RuleName: "Remove old"}

			require.NoError(t, r.sm.DeleteTorrents(t.Context(), r.instanceID, []string{strings.ToUpper(hashA)}, deleteFiles, actor))

			require.Equal(t, []deleteNoteSeen{{r.instanceID, []string{hashA}, actor, 0}}, noter.notes)
			require.Equal(t, 1, r.stub.Calls("torrents/delete"))
		})
	}
}

func TestNoteProxiedDeleteNamesTheKeyAndEachTorrentOfAll(t *testing.T) {
	t.Parallel()
	r := newTriggerRig(t)
	r.add(r.perRelease(hashA, "tv"), qbtstub.Torrent{Hash: hashB, Name: "Film-GRP", SavePath: r.p("torrents/movies/Film-GRP"),
		Category: "movies", Layout: qbtstub.NoRootFolder, Files: []string{"Film.mkv"}})
	noter := &fakeDeleteNoter{stub: r.stub}
	r.sm.SetDeleteNoter(noter)
	actor := DeleteActor{Kind: DeleteActorClientProxy, KeyName: "sonarr"}

	r.sm.NoteProxiedDelete(t.Context(), r.instanceID, []string{hashB}, "sonarr")
	r.sm.NoteProxiedDelete(t.Context(), r.instanceID, []string{"all"}, "sonarr")

	require.Equal(t, []deleteNoteSeen{
		{r.instanceID, []string{hashB}, actor, 0},
		{r.instanceID, []string{hashA, hashB}, actor, 0},
	}, noter.notes)
}
