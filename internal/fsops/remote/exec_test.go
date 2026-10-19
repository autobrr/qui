// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package remote

import (
	"context"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/sshpool"
	"github.com/autobrr/qui/internal/testutil/sshtest"
	"github.com/autobrr/qui/pkg/hardlink"
)

// SetSSHTier leaves the row alone: each test picks the tier its backend
// reads, and the pool tests cover the write.
func (fakeCreds) SetSSHTier(context.Context, int, models.SSHTier) error { return nil }

// newExecBackend is newBackend on the exec tier. The fake server runs each
// command through the local sh, against the files the sftp subsystem serves.
func newExecBackend(t *testing.T) (*Backend, *sshtest.Server) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the fake exec handler runs the local GNU find and stat")
	}
	backend, server := newBackend(t)
	backend.inst.SSHTier = models.SSHTierExec
	return backend, server
}

func unixID(t *testing.T, p string, follow bool) (hardlink.FileID, uint64) {
	t.Helper()
	stat := os.Lstat
	if follow {
		stat = os.Stat
	}
	fi, err := stat(p)
	require.NoError(t, err)
	id, nlink, err := hardlink.GetFileID(fi, p)
	require.NoError(t, err)
	// The unix ID holds dev and ino in the layout remoteFileID writes.
	return hardlink.RemoteFileID(id.Bytes()[1:17]), nlink
}

func TestExecIdentity(t *testing.T) {
	t.Parallel()

	backend, server := newExecBackend(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "it's -rf $(x).mkv")
	link := filepath.Join(dir, "hardlink.mkv")
	symlink := filepath.Join(dir, "symlink.mkv")
	require.NoError(t, os.WriteFile(file, []byte("data"), 0o600))
	require.NoError(t, os.Link(file, link))
	require.NoError(t, os.Symlink(file, symlink))

	info, err := backend.Lstat(t.Context(), file)
	require.NoError(t, err)
	require.NoError(t, info.FileIDErr)
	wantID, wantLinks := unixID(t, file, false)
	assert.Equal(t, wantID, info.FileID)
	assert.Equal(t, hardlink.KindRemote, info.FileID.Kind())
	assert.Equal(t, uint64(2), info.Nlinks)
	assert.Equal(t, wantLinks, info.Nlinks)

	linked, err := backend.Lstat(t.Context(), link)
	require.NoError(t, err)
	assert.Equal(t, info.FileID, linked.FileID, "a hardlink has the identity of its source")

	// Lstat never follows: a planted symlink reports no identity at all.
	planted, err := backend.Lstat(t.Context(), symlink)
	require.NoError(t, err)
	assert.True(t, planted.FileID.IsZero())
	require.NoError(t, planted.FileIDErr)

	followed, err := backend.Stat(t.Context(), symlink)
	require.NoError(t, err)
	require.NoError(t, followed.FileIDErr)
	assert.Equal(t, info.FileID, followed.FileID, "Stat gives the identity of the link target")

	dirInfo, err := backend.Lstat(t.Context(), dir)
	require.NoError(t, err)
	require.NoError(t, dirInfo.FileIDErr)
	wantDirID, _ := unixID(t, dir, false)
	assert.Equal(t, wantDirID, dirInfo.FileID)

	commands := server.Commands()
	assert.Contains(t, commands, []string{"LC_ALL=C", "stat", "--printf", "%d %i %h", "--", file})
	assert.Contains(t, commands, []string{"LC_ALL=C", "stat", "-L", "--printf", "%d %i %h", "--", symlink})
}

func TestExecIdentityOfAMissingPathIsNotExist(t *testing.T) {
	t.Parallel()

	backend, _ := newExecBackend(t)
	_, err := backend.Lstat(t.Context(), filepath.Join(t.TempDir(), "missing"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestExecSameFilesystem(t *testing.T) {
	t.Parallel()

	backend, server := newExecBackend(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o750))

	same, err := backend.SameFilesystem(t.Context(), dir, sub)
	require.NoError(t, err)
	assert.True(t, same)

	same, err = backend.SameFilesystem(t.Context(), dir, "/proc")
	require.NoError(t, err)
	assert.False(t, same)

	assert.Contains(t, server.Commands(), []string{"LC_ALL=C", "stat", "-L", "--printf", `%d\0`, "--", dir, sub})

	_, err = backend.SameFilesystem(t.Context(), dir, filepath.Join(dir, "missing"))
	require.Error(t, err)
}

// walkTree builds a fixture with the names find output has to survive: a
// space, a newline, a quote, a leading dash, and a hardlink across
// directories.
func walkTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"a dir", "a dir/inner", "line\nbreak", "-rf", "@eaDir", "@eaDir/thumbs", "b"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, d), 0o750))
	}
	for _, f := range []string{"a dir/one.mkv", "a dir/inner/it's.mkv", "line\nbreak/two.mkv", "-rf/-rf", "@eaDir/thumbs/x.jpg", "b/z", "b/a", ".hidden"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, f), []byte(f), 0o600))
	}
	require.NoError(t, os.Link(filepath.Join(root, "a dir/one.mkv"), filepath.Join(root, "b/one-link.mkv")))
	require.NoError(t, os.Symlink(filepath.Join(root, "a dir"), filepath.Join(root, "b/dirlink")))
	return root
}

type walked struct {
	RelPath   string
	Path      string
	IsDir     bool
	IsSymlink bool
	Size      int64
	Mode      fs.FileMode
	ModTime   int64
}

func walkAll(t *testing.T, backend *Backend, root string, opts fsops.WalkOptions) ([]walked, []fsops.WalkEntry) {
	t.Helper()
	ch, err := backend.WalkDir(t.Context(), root, opts)
	require.NoError(t, err)
	var out []walked
	var entries []fsops.WalkEntry
	for e := range ch {
		require.NoError(t, e.Err)
		out = append(out, walked{e.RelPath, e.Path, e.IsDir, e.IsSymlink, e.Size, e.Mode, e.ModTime.Unix()})
		entries = append(entries, e)
	}
	return out, entries
}

// The exec walk and the sftp walk give the same entries on one tree. The
// sftp walk lists sibling directories concurrently, so only the exec order
// is fixed: lexical within a directory, a directory before its contents.
func TestExecWalkMatchesTheSFTPWalk(t *testing.T) {
	t.Parallel()

	backend, server := newExecBackend(t)
	sftpBackend, _ := newBackend(t)
	root := walkTree(t)
	opts := fsops.WalkOptions{WantFileID: true, IgnoreDirNames: []string{"@EADIR"}}

	viaExec, execEntries := walkAll(t, backend, root, opts)
	viaSFTP, _ := walkAll(t, sftpBackend, root, opts)

	assert.ElementsMatch(t, viaSFTP, viaExec)
	assert.Equal(t, []string{
		".", "-rf", "-rf/-rf", ".hidden", "a dir", "a dir/inner", "a dir/inner/it's.mkv", "a dir/one.mkv",
		"b", "b/a", "b/dirlink", "b/one-link.mkv", "b/z", "line\nbreak", "line\nbreak/two.mkv",
	}, relPaths(viaExec))
	assert.True(t, slices.ContainsFunc(server.Commands(), func(words []string) bool {
		return slices.Equal(words, []string{"LC_ALL=C", "find", "-P", "--", root, "-mindepth", "1", "-printf", findFormat})
	}), "find argv: %q", server.Commands())

	ids := map[string]fsops.WalkEntry{}
	for _, e := range execEntries {
		ids[e.RelPath] = e
		if e.Mode.IsRegular() {
			assert.NoError(t, e.FileIDErr, e.RelPath)
			want, _ := unixID(t, e.Path, false)
			assert.Equal(t, want, e.FileID, e.RelPath)
		} else {
			assert.True(t, e.FileID.IsZero(), e.RelPath)
		}
	}
	assert.Equal(t, ids["a dir/one.mkv"].FileID, ids["b/one-link.mkv"].FileID)
	assert.Equal(t, uint64(2), ids["b/one-link.mkv"].Nlinks)
}

func relPaths(entries []walked) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.RelPath)
	}
	return out
}

// find exits non-zero for a directory it cannot read, and cannot say which
// one in a form worth parsing. The walk then runs over sftp, which names it.
func TestExecWalkFallsBackToSFTPOnAnUnreadableDirectory(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory")
	}

	backend, _ := newExecBackend(t)
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	require.NoError(t, os.Mkdir(locked, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "file"), nil, 0o600))
	require.NoError(t, os.Chmod(locked, 0))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o750) })

	ch, err := backend.WalkDir(t.Context(), root, fsops.WalkOptions{WantFileID: true})
	require.NoError(t, err)
	var errs []string
	var files []fsops.WalkEntry
	for e := range ch {
		if e.Err != nil {
			errs = append(errs, e.Path)
			require.ErrorIs(t, e.Err, fs.ErrPermission)
			continue
		}
		if e.Mode.IsRegular() {
			files = append(files, e)
		}
	}
	assert.Equal(t, []string{locked}, errs)
	require.Len(t, files, 1)
	assert.ErrorIs(t, files[0].FileIDErr, errNoIdentity, "the sftp walk carries no identity")
}

// The key loses its shell: the reconnect writes the tier down, and a backend
// built on the stored exec tier gets ErrUnsupported for identity instead of
// a guess, while its sftp reads go on.
func TestExecTierDropAnswersUnsupported(t *testing.T) {
	t.Parallel()

	backend, server := newExecBackend(t)
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	info, err := backend.Lstat(t.Context(), file)
	require.NoError(t, err)
	require.NoError(t, info.FileIDErr)

	server.SetExec(sshtest.ExecSFTPOnly)
	backend.pool.Invalidate(backend.inst.ID)

	info, err = backend.Lstat(t.Context(), file)
	require.NoError(t, err)
	require.ErrorIs(t, info.FileIDErr, fsops.ErrUnsupported)
	assert.True(t, info.FileID.IsZero())

	_, err = backend.SameFilesystem(t.Context(), file, file)
	assert.ErrorIs(t, err, fsops.ErrUnsupported)
}

func TestParseFindRecord(t *testing.T) {
	t.Parallel()

	entry, err := parseFindRecord([]byte("f 2049 131 3 42 1700000000.5000000000 4755 dir/a b\nc"))
	require.NoError(t, err)
	assert.Equal(t, "dir/a b\nc", entry.rel)
	assert.Equal(t, remoteFileID(2049, 131), entry.id)
	assert.Equal(t, uint64(3), entry.nlink)
	assert.Equal(t, int64(42), entry.size)
	assert.Equal(t, int64(1700000000), entry.modTime.Unix())
	assert.Equal(t, 500_000_000, entry.modTime.Nanosecond())
	assert.Equal(t, fs.FileMode(0o755)|fs.ModeSetuid, entry.mode)

	for _, bad := range []string{"", "f 1 2 3 4 5", "f x 2 3 4 5.0 644 a", "ff 1 2 3 4 5.0 644 a", "f 1 2 3 4 5.0 644 "} {
		_, err := parseFindRecord([]byte(bad))
		assert.Error(t, err, "%q", bad)
	}
}

// A host that prints more records than the caller allows, as a hostile host
// can for a batch, fails the read at the limit and not at the end of its
// output.
func TestFindStopsAtItsLimit(t *testing.T) {
	t.Parallel()

	backend, _ := newExecBackend(t)
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o600))
	}
	conn, err := backend.exec(t.Context())
	require.NoError(t, err)
	cmd := sshpool.Command{Name: "find", Paths: []string{dir}, Expression: []string{"-mindepth", "1", "-printf", findFormat}}

	entries, err := find(t.Context(), conn, cmd, 2, findByteLimit)
	require.ErrorIs(t, err, errFindLimit)
	assert.Empty(t, entries)

	entries, err = find(t.Context(), conn, cmd, 3, findByteLimit)
	require.NoError(t, err)
	assert.Len(t, entries, 3)

	// The byte cap bounds memory when a host prints long records.
	entries, err = find(t.Context(), conn, cmd, 3, 8)
	require.ErrorIs(t, err, errFindLimit)
	assert.Empty(t, entries)
}

func TestComparePaths(t *testing.T) {
	t.Parallel()

	paths := []string{"b", "a/b", "a", "a b", "a/a/z", "a-b", "a/a"}
	slices.SortFunc(paths, comparePaths)
	assert.Equal(t, []string{"a", "a/a", "a/a/z", "a/b", "a b", "a-b", "b"}, paths)
}

// lstatBatchTree is a directory with one of each kind of path LstatBatch gets,
// and enough plain files for two batches.
func lstatBatchTree(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "it's -rf $(x).mkv")
	require.NoError(t, os.WriteFile(file, []byte("data"), 0o600))
	require.NoError(t, os.Link(file, filepath.Join(dir, "hardlink.mkv")))
	require.NoError(t, os.Symlink(file, filepath.Join(dir, "symlink.mkv")))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o700))
	paths := make([]string, 0, 5+batchPaths)
	paths = append(paths, file, filepath.Join(dir, "hardlink.mkv"), filepath.Join(dir, "symlink.mkv"), filepath.Join(dir, "sub"), filepath.Join(dir, "missing"))
	for i := range batchPaths {
		p := filepath.Join(dir, "sub", "f"+strconv.Itoa(i))
		require.NoError(t, os.WriteFile(p, nil, 0o600))
		paths = append(paths, p)
	}
	return paths
}

func finds(server *sshtest.Server) int {
	n := 0
	for _, words := range server.Commands() {
		if len(words) > 1 && words[1] == "find" {
			n++
		}
	}
	return n
}

type batchFunc func(context.Context, []string) ([]*fsops.LstatInfo, []error)

// assertBatchMatches compares each answer of batch with single. sftp attrs
// carry whole seconds, so the mtimes compare at that precision.
func assertBatchMatches(t *testing.T, batch batchFunc, single func(context.Context, string) (*fsops.LstatInfo, error), paths []string) {
	t.Helper()
	infos, errs := batch(t.Context(), paths)
	require.Len(t, infos, len(paths))
	require.Len(t, errs, len(paths))
	for i, p := range paths {
		want, wantErr := single(t.Context(), p)
		if wantErr != nil {
			require.ErrorIs(t, errs[i], fs.ErrNotExist, p)
			require.ErrorIs(t, wantErr, fs.ErrNotExist, p)
			assert.Nil(t, infos[i], p)
			continue
		}
		require.NoError(t, errs[i], p)
		got := *infos[i]
		assert.Equal(t, want.ModTime.Truncate(time.Second), got.ModTime.Truncate(time.Second), p)
		got.ModTime, want.ModTime = time.Time{}, time.Time{}
		assert.Equal(t, *want, got, p)
	}
}

func TestExecLstatBatchMatchesLstat(t *testing.T) {
	t.Parallel()

	backend, server := newExecBackend(t)
	paths := lstatBatchTree(t)
	// Connect first: the connect probe runs its own commands through the shell.
	_, err := backend.exec(t.Context())
	require.NoError(t, err)

	before := len(server.Commands())
	infos, errs := backend.LstatBatch(t.Context(), paths)
	ran := server.Commands()[before:]
	require.Len(t, ran, 2, "one find per batch, and no stat per path")
	for _, words := range ran {
		require.Greater(t, len(words), 8)
		assert.Equal(t, []string{"LC_ALL=C", "find", "-P", "--"}, words[:4])
		assert.Equal(t, []string{"-maxdepth", "0", "-printf", batchFormat}, words[len(words)-4:])
	}
	assert.Equal(t, paths, slices.Concat(ran[0][4:len(ran[0])-4], ran[1][4:len(ran[1])-4]), "each path goes to find once, in order")
	for i := range paths {
		if i == 4 {
			require.ErrorIs(t, errs[i], fs.ErrNotExist)
			continue
		}
		require.NoError(t, errs[i], paths[i])
		assert.Equal(t, paths[i], infos[i].Path)
	}
	assert.False(t, infos[0].FileID.IsZero())
	assert.Equal(t, infos[0].FileID, infos[1].FileID, "a hardlink has the identity of its source")
	assert.True(t, infos[2].FileID.IsZero(), "LstatBatch never follows a symlink")

	// Lstat runs a stat per path, so the field by field check reads the
	// special paths and one file from each batch.
	assertBatchMatches(t, backend.LstatBatch, backend.Lstat, append(paths[:6:6], paths[len(paths)-1]))
}

func TestSFTPLstatBatchIsLstat(t *testing.T) {
	t.Parallel()

	backend, server := newBackend(t)
	paths := lstatBatchTree(t)[:5]

	assertBatchMatches(t, backend.LstatBatch, backend.Lstat, paths)
	assert.Zero(t, finds(server))
}

// find reads a leading "-", "!" or "(" as the start of its expression, so a
// path that is not absolute never reaches it: the entry gets an error and
// the batch answers the rest.
func TestExecLstatBatchRefusesARelativePath(t *testing.T) {
	t.Parallel()

	backend, server := newExecBackend(t)
	paths := lstatBatchTree(t)[:2]
	paths = append(paths, "-fprint")

	before := len(server.Commands())
	infos, errs := backend.LstatBatch(t.Context(), paths)
	for _, cmd := range server.Commands()[before:] {
		assert.NotContains(t, cmd, "-fprint", "a relative path never reaches find")
	}
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Error(t, errs[2])
	assert.Nil(t, infos[2])
	assert.Equal(t, paths[0], infos[0].Path)
}

func TestExecStatBatchFollowsLinks(t *testing.T) {
	t.Parallel()

	backend, server := newExecBackend(t)
	tree := lstatBatchTree(t)
	file, hardlink, symlink, sub, missing := tree[0], tree[1], tree[2], tree[3], tree[4]
	broken := filepath.Join(filepath.Dir(file), "broken.mkv")
	require.NoError(t, os.Symlink(missing, broken))
	paths := []string{file, missing, symlink, broken, sub, hardlink}
	_, err := backend.exec(t.Context())
	require.NoError(t, err)

	before := len(server.Commands())
	infos, errs := backend.StatBatch(t.Context(), paths)
	ran := server.Commands()[before:]
	assert.Equal(t, [][]string{slices.Concat([]string{"LC_ALL=C", "find", "-L", "--"}, paths, []string{"-maxdepth", "0", "-printf", batchFormat})}, ran,
		"one find for the batch, and no stat per path")
	require.ErrorIs(t, errs[1], fs.ErrNotExist)
	require.ErrorIs(t, errs[3], fs.ErrNotExist, "a broken link has no target to stat")
	require.NoError(t, errs[2])
	assert.False(t, infos[2].IsSymlink)
	assert.True(t, infos[2].Mode.IsRegular())
	assert.Equal(t, infos[0].FileID, infos[2].FileID, "StatBatch reads the target of a symlink")

	assertBatchMatches(t, backend.StatBatch, backend.Stat, paths)
}

// Each single stat is a round trip, so answering n paths one after the other
// cannot beat n round trips. On the sftp tier every path is a single stat,
// and on the exec tier every path that find does not print is one, such as
// the files of a torrent that were moved away.
func TestBatchSingleStatsOverlapRoundTrips(t *testing.T) {
	t.Parallel()

	const paths, latency = 64, 25 * time.Millisecond
	tests := []struct {
		name    string
		backend func(t *testing.T) (*Backend, *sshtest.Server)
		missing bool
	}{
		{name: "sftp tier", backend: newBackend},
		{name: "exec tier, missing paths", backend: newExecBackend, missing: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			backend, server := tt.backend(t)
			dir := t.TempDir()
			batch := make([]string, paths)
			for i := range batch {
				batch[i] = filepath.Join(dir, "f"+strconv.Itoa(i))
				if !tt.missing {
					require.NoError(t, os.WriteFile(batch[i], nil, 0o600))
				}
			}
			server.SetLatency(latency)
			// Connect first: the dial and the connect probe are round trips too.
			_, err := backend.client(t.Context())
			require.NoError(t, err)

			start := time.Now()
			_, errs := backend.LstatBatch(t.Context(), batch)
			elapsed := time.Since(start)
			serialFloor := time.Duration(paths) * latency
			t.Logf("%d paths at %v latency: %v (serial floor %v)", paths, latency, elapsed.Round(time.Millisecond), serialFloor)

			for i, err := range errs {
				if tt.missing {
					require.ErrorIs(t, err, fs.ErrNotExist, batch[i])
				} else {
					require.NoError(t, err, batch[i])
				}
			}
			assert.Less(t, elapsed, serialFloor/2, "the single stats must overlap")
			// At most batchWidth single stats are in flight, so the batch
			// cannot beat one round trip for each batchWidth paths.
			assert.GreaterOrEqual(t, elapsed, time.Duration(paths/batchWidth)*latency, "at most batchWidth single stats in flight")
		})
	}
}

// A connection lost before a batch is not an answer about the paths. The
// pool remembers the failed dial, so each path gets its own
// fsops.ErrConnectionLost at once, and no path reads as missing.
func TestExecBatchAfterALostConnectionReportsEachPathAsLost(t *testing.T) {
	t.Parallel()

	backend, server := newExecBackend(t)
	tree := lstatBatchTree(t)
	_, err := backend.exec(t.Context())
	require.NoError(t, err)

	_, portText, err := net.SplitHostPort(sshtest.DeadAddr(t))
	require.NoError(t, err)
	backend.inst.SSHPort, err = strconv.Atoi(portText)
	require.NoError(t, err)
	server.DropConnections()

	for name, batch := range map[string]batchFunc{"lstat": backend.LstatBatch, "stat": backend.StatBatch} {
		infos, errs := batch(t.Context(), tree)
		for i, p := range tree {
			assert.Nil(t, infos[i], "%s %s", name, p)
			require.ErrorIs(t, errs[i], fsops.ErrConnectionLost, "%s %s", name, p)
			require.NotErrorIs(t, errs[i], fs.ErrNotExist, "%s %s", name, p)
		}
	}
}

// The exec tier reads find, the sftp tier reads sftp attrs, and both answer
// a batch the same way. Only identity differs, since sftp has none.
func TestBatchTiersAgree(t *testing.T) {
	t.Parallel()

	execBackend, _ := newExecBackend(t)
	sftpBackend, _ := newBackend(t)
	tree := lstatBatchTree(t)
	broken := filepath.Join(filepath.Dir(tree[0]), "broken.mkv")
	require.NoError(t, os.Symlink(tree[4], broken))
	paths := slices.Concat(tree[:5], []string{broken, tree[len(tree)-1]})

	withoutIdentity := func(infos []*fsops.LstatInfo) []*fsops.FileInfo {
		out := make([]*fsops.FileInfo, len(infos))
		for i, info := range infos {
			if info != nil {
				fi := info.FileInfo
				fi.ModTime = fi.ModTime.Truncate(time.Second)
				out[i] = &fi
			}
		}
		return out
	}
	for name, batch := range map[string]func(*Backend) batchFunc{
		"lstat": func(b *Backend) batchFunc { return b.LstatBatch },
		"stat":  func(b *Backend) batchFunc { return b.StatBatch },
	} {
		viaExec, execErrs := batch(execBackend)(t.Context(), paths)
		viaSFTP, sftpErrs := batch(sftpBackend)(t.Context(), paths)
		assert.Equal(t, withoutIdentity(viaSFTP), withoutIdentity(viaExec), name)
		for i, p := range paths {
			assert.Equal(t, sftpErrs[i] == nil, execErrs[i] == nil, "%s %s: sftp %v, exec %v", name, p, sftpErrs[i], execErrs[i])
		}
	}
}
