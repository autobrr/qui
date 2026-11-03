// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"bytes"
	"io/fs"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
)

// An exec walk that fell back to sftp reads no identity, which a row claiming Identity should explain once.
func TestScanLogsFilesWithoutIdentity(t *testing.T) {
	t.Parallel()

	identified := slashEntry("/data/scan/Film/a.mkv", "Film/a.mkv", 0)
	identified.FileID = remoteID(1)
	for _, tt := range []struct {
		name string
		tier models.SSHTier
		walk []fsops.WalkEntry
		want string
	}{
		{name: "exec row, every identity read", tier: models.SSHTierExec, walk: []fsops.WalkEntry{identified}},
		{name: "exec row, identity missing", tier: models.SSHTierExec, walk: []fsops.WalkEntry{identified, slashEntry("/data/scan/Film/b.mkv", "Film/b.mkv", 0), slashEntry("/data/scan/Film/c.mkv", "Film/c.mkv", 0)}, want: `"files":2`},
		{name: "sftp row never has identity", tier: models.SSHTierSFTP, walk: []fsops.WalkEntry{slashEntry("/data/scan/Film/b.mkv", "Film/b.mkv", 0)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			walk := append([]fsops.WalkEntry{slashEntry("/data/scan", ".", fs.ModeDir), slashEntry("/data/scan/Film", "Film", fs.ModeDir)}, tt.walk...)
			row := remoteInstance(tt.tier)
			svc := &Service{torrentFiles: stubTorrentFiles{empty: true}}
			var buf bytes.Buffer
			l := zerolog.New(&buf)

			_, _, ok := svc.runScanPhase(t.Context(), &models.DirScanDirectory{TargetInstanceID: row.ID}, &scanTarget{backend: newSlashBackend(walk...), instance: row}, "/data/scan", 1, &l)

			require.True(t, ok)
			if tt.want == "" {
				require.NotContains(t, buf.String(), "no identity")
				return
			}
			require.Contains(t, buf.String(), "no identity")
			require.Contains(t, buf.String(), tt.want)
		})
	}
}
