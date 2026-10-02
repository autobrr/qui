// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package remote

import (
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/sshpool"
)

// liveWalkCreds is fakeCreds for a real host: the key comes from a file and
// the pin is learned on first contact.
type liveWalkCreds struct {
	key  string
	pin  []byte
	inst *models.Instance
}

func (c liveWalkCreds) Get(context.Context, int) (*models.Instance, error)  { return c.inst, nil }
func (c liveWalkCreds) GetDecryptedSSHKey(*models.Instance) (string, error) { return c.key, nil }
func (c liveWalkCreds) GetHostKeyPin(*models.Instance) ([]byte, error) {
	if c.pin == nil {
		return nil, models.ErrSSHHostKeyNotPinned
	}
	return c.pin, nil
}

// TestLive_WalkWideTree is the measurement for #2792. It runs only with
// QUI_SFTP_LIVE_HOST (host or host:port), QUI_SFTP_LIVE_USER and
// QUI_SFTP_LIVE_KEY (private key file) set. It builds a tree of
// QUI_SFTP_LIVE_WALK_DIRS directories (default 130), one file each, under
// qui-fsops-live-walk in the sftp home, and keeps it between runs so the
// before and after walks see the same tree.
func TestLive_WalkWideTree(t *testing.T) {
	hostPort, user, keyFile := os.Getenv("QUI_SFTP_LIVE_HOST"), os.Getenv("QUI_SFTP_LIVE_USER"), os.Getenv("QUI_SFTP_LIVE_KEY")
	if hostPort == "" || user == "" || keyFile == "" {
		t.Skip("QUI_SFTP_LIVE_HOST, QUI_SFTP_LIVE_USER and QUI_SFTP_LIVE_KEY select the live seedbox")
	}
	dirs := 130
	if v := os.Getenv("QUI_SFTP_LIVE_WALK_DIRS"); v != "" {
		var err error
		dirs, err = strconv.Atoi(v)
		require.NoError(t, err)
	}
	key, err := os.ReadFile(keyFile)
	require.NoError(t, err)
	host, port := hostPort, 22
	if h, p, err := net.SplitHostPort(hostPort); err == nil {
		host = h
		port, err = strconv.Atoi(p)
		require.NoError(t, err)
	}
	inst := &models.Instance{ID: 1, SSHHost: host, SSHPort: port, SSHUsername: user, SSHKeyEncrypted: "live", SSHHostKeyEncrypted: "live"}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	report, err := sshpool.NewDialer(liveWalkCreds{key: string(key), inst: inst}).Test(ctx, inst)
	require.NoError(t, err)
	pool := sshpool.NewPool(sshpool.NewDialer(liveWalkCreds{key: string(key), pin: report.HostKey.Marshal(), inst: inst}))
	t.Cleanup(pool.Close)
	b := New(pool, inst)
	client, err := pool.SFTP(ctx, inst)
	require.NoError(t, err)

	root := "qui-fsops-live-walk"
	// An existing tree wins over the env value, so a before/after pair run
	// with different QUI_SFTP_LIVE_WALK_DIRS still walks the same tree.
	if existing, err := client.ReadDir(root); err == nil && len(existing) > 0 {
		dirs = len(existing)
		t.Logf("reusing %d directories", dirs)
	} else {
		start := time.Now()
		for i := range dirs {
			d := path.Join(root, fmt.Sprintf("d%03d", i))
			require.NoError(t, client.MkdirAll(d))
			f, err := client.Create(path.Join(d, "f"))
			require.NoError(t, err)
			require.NoError(t, f.Close())
		}
		t.Logf("built %d directories in %v", dirs, time.Since(start).Round(time.Millisecond))
	}

	start := time.Now()
	ch, err := b.WalkDir(ctx, root, fsops.WalkOptions{})
	require.NoError(t, err)
	n := 0
	for entry := range ch {
		require.NoError(t, entry.Err)
		n++
	}
	elapsed := time.Since(start)
	t.Logf("walked %d entries over %d directories in %v (%v per directory)", n, dirs, elapsed.Round(time.Millisecond), (elapsed / time.Duration(dirs)).Round(time.Millisecond))
	require.Equal(t, 1+2*dirs, n)
}
