// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package remote

import (
	"context"
	"io/fs"
	"net"
	"os"
	"path"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/sshpool"
	"github.com/autobrr/qui/pkg/fsutil"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

// liveCreds is fakeCreds for a real host: the key comes from a file and the
// pin is learned on first contact, since nothing in qui reaches these methods
// until #2942 and a hand-run test is the only route to a seedbox.
type liveCreds struct {
	key  string
	pin  []byte
	inst *models.Instance
}

func (c liveCreds) Get(context.Context, int) (*models.Instance, error)  { return c.inst, nil }
func (c liveCreds) GetDecryptedSSHKey(*models.Instance) (string, error) { return c.key, nil }
func (c liveCreds) GetHostKeyPin(*models.Instance) ([]byte, error) {
	if c.pin == nil {
		return nil, models.ErrSSHHostKeyNotPinned
	}
	return c.pin, nil
}

// TestLive_WriteOperations is the field test for #2725. It runs only with
// QUI_SFTP_LIVE_HOST (host or host:port), QUI_SFTP_LIVE_USER and
// QUI_SFTP_LIVE_KEY (private key file) set, works under
// QUI_SFTP_LIVE_DIR (default qui-fsops-live-test, relative to the sftp home)
// and removes everything it made.
func TestLive_WriteOperations(t *testing.T) {
	hostPort, user, keyFile := os.Getenv("QUI_SFTP_LIVE_HOST"), os.Getenv("QUI_SFTP_LIVE_USER"), os.Getenv("QUI_SFTP_LIVE_KEY")
	if hostPort == "" || user == "" || keyFile == "" {
		t.Skip("QUI_SFTP_LIVE_HOST, QUI_SFTP_LIVE_USER and QUI_SFTP_LIVE_KEY select the live seedbox")
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

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	report, err := sshpool.NewDialer(liveCreds{key: string(key), inst: inst}).Test(ctx, inst)
	require.NoError(t, err)
	require.Equal(t, sshpool.StatusUnpinned, report.Status)
	t.Logf("host key %s %s; capabilities %+v", report.HostKey.Type(), ssh.FingerprintSHA256(report.HostKey), *report.Capabilities)

	pool := sshpool.NewPool(sshpool.NewDialer(liveCreds{key: string(key), pin: report.HostKey.Marshal(), inst: inst}))
	t.Cleanup(pool.Close)
	b := New(pool, inst)
	client, err := pool.SFTP(ctx, inst)
	require.NoError(t, err)

	root := os.Getenv("QUI_SFTP_LIVE_DIR")
	if root == "" {
		root = "qui-fsops-live-test"
	}
	// A previous run that died mid-way leaves its tree; clear it first.
	_ = b.Remove(ctx, root, fsops.RemoveOptions{Recursive: true})
	t.Cleanup(func() {
		require.NoError(t, b.Remove(context.WithoutCancel(ctx), root, fsops.RemoveOptions{Recursive: true}))
	})

	writeRemote := func(name string) {
		t.Helper()
		require.NoError(t, client.MkdirAll(path.Dir(name)))
		f, err := client.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte("live"))
		require.NoError(t, err)
		require.NoError(t, f.Close())
	}
	step := func(name string, fn func()) {
		t.Helper()
		start := time.Now()
		fn()
		t.Logf("%-32s %v", name, time.Since(start).Round(time.Millisecond))
	}

	src := path.Join(root, "src")
	step("mkdirall nested", func() {
		require.NoError(t, b.MkdirAll(ctx, path.Join(src, "deep"), fsutil.ContentDirMode))
	})
	writeRemote(path.Join(src, "one.mkv"))
	writeRemote(path.Join(src, "deep", "two.mkv"))

	links := path.Join(root, "links", "Show.S01")
	plan := &hardlinktree.TreePlan{RootDir: links, Files: []hardlinktree.FilePlan{
		{SourcePath: path.Join(src, "one.mkv"), TargetPath: path.Join(links, "one.mkv")},
		{SourcePath: path.Join(src, "deep", "two.mkv"), TargetPath: path.Join(links, "Sub -rf", "two.mkv")},
	}}

	var created *fsops.TreeCreateResult
	step("hardlinktree create", func() {
		created, err = b.HardlinkTree(ctx, plan)
		if !report.Capabilities.Hardlink {
			require.ErrorIs(t, err, fsops.ErrUnsupported)
			return
		}
		require.NoError(t, err)
		assert.Equal(t, 2, created.Created)
		for _, fp := range plan.Files {
			_, err := b.Lstat(ctx, fp.TargetPath)
			require.NoError(t, err, fp.TargetPath)
		}
	})
	if created != nil {
		step("removetree", func() {
			require.NoError(t, b.RemoveTree(ctx, created))
			_, err := b.Lstat(ctx, links)
			require.ErrorIs(t, err, fs.ErrNotExist)
		})
		step("hardlinktree forced failure", func() {
			// The second target is pre-created, so the first link must be
			// rolled back and the stranger left alone.
			require.NoError(t, b.MkdirAll(ctx, path.Dir(plan.Files[1].TargetPath), fsutil.ContentDirMode))
			writeRemote(plan.Files[1].TargetPath)
			_, err := b.HardlinkTree(ctx, plan)
			require.Error(t, err)
			_, err = b.Lstat(ctx, plan.Files[0].TargetPath)
			require.ErrorIs(t, err, fs.ErrNotExist)
			_, err = b.Lstat(ctx, plan.Files[1].TargetPath)
			require.NoError(t, err)
		})
	}

	keep := path.Join(root, "keep")
	orphan := path.Join(root, "orphan")
	step("remove recursive, symlink inside", func() {
		writeRemote(path.Join(keep, "precious"))
		writeRemote(path.Join(orphan, "a", "f1"))
		writeRemote(path.Join(orphan, "f2"))
		require.NoError(t, client.Symlink(keep, path.Join(orphan, "a", "linkdir")))
		require.NoError(t, b.Remove(ctx, orphan, fsops.RemoveOptions{Recursive: true}))
		_, err := b.Lstat(ctx, orphan)
		require.ErrorIs(t, err, fs.ErrNotExist)
		_, err = b.Lstat(ctx, path.Join(keep, "precious"))
		require.NoError(t, err, "the link target must survive")
	})
}
