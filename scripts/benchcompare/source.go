// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type repo struct{ root string }

func (r repo) git(ctx context.Context, stdin string, args ...string) (string, error) {
	return r.gitWith(ctx, nil, stdin, args...)
}

func (r repo) gitWith(ctx context.Context, env []string, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok && len(exit.Stderr) > 0 {
			err = fmt.Errorf("%w: %s", err, bytes.TrimSpace(exit.Stderr))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func (r repo) gitLine(ctx context.Context, args ...string) (string, error) {
	out, err := r.git(ctx, "", args...)
	return strings.TrimSpace(out), err
}

func (r repo) freeze(ctx context.Context, scratch string) (commit string, edited bool, err error) {
	head, err := r.gitLine(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", false, err
	}
	env := []string{
		"GIT_INDEX_FILE=" + filepath.Join(scratch, "index"),
		"GIT_AUTHOR_NAME=benchcompare", "GIT_AUTHOR_EMAIL=benchcompare@localhost",
		"GIT_COMMITTER_NAME=benchcompare", "GIT_COMMITTER_EMAIL=benchcompare@localhost",
	}
	for _, args := range [][]string{{"read-tree", head}, {"add", "--all"}} {
		if _, err := r.gitWith(ctx, env, "", args...); err != nil {
			return "", false, err
		}
	}
	files, err := r.gitWith(ctx, env, "", "write-tree")
	if err != nil {
		return "", false, err
	}
	files = strings.TrimSpace(files)
	committed, err := r.gitLine(ctx, "rev-parse", head+"^{tree}")
	if err != nil || files == committed {
		return head, false, err
	}
	out, err := r.gitWith(ctx, env, "", "commit-tree", files, "-p", head, "-m", "the working tree")
	return strings.TrimSpace(out), true, err
}

func split(out, sep string) []string {
	return slices.DeleteFunc(strings.Split(out, sep), func(s string) bool { return s == "" })
}

const benchPattern = `^func Benchmark[^a-z]`

type commitTree struct {
	repo   repo
	commit string
}

func (t commitTree) file(ctx context.Context, name string) ([]byte, error) {
	found, err := t.repo.git(ctx, "", "ls-tree", "-z", "--name-only", t.commit, "--", name)
	if err != nil {
		return nil, err
	}
	if found == "" {
		return nil, fmt.Errorf("%s at %s: %w", name, t.commit, fs.ErrNotExist)
	}
	out, err := t.repo.git(ctx, "", "cat-file", "blob", t.commit+":"+name)
	return []byte(out), err
}

func (t commitTree) goFiles(ctx context.Context, dir string) (map[string][]byte, error) {
	list, err := t.repo.git(ctx, "", "ls-tree", "-z", t.commit, "--", path.Clean(dir)+"/")
	if err != nil {
		return nil, err
	}
	var names []string
	var ids strings.Builder
	for entry := range strings.SplitSeq(list, "\x00") {
		meta, name, _ := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if len(fields) == 3 && fields[1] == "blob" && strings.HasSuffix(name, ".go") {
			names = append(names, name)
			ids.WriteString(fields[2] + "\n")
		}
	}
	out, err := t.repo.git(ctx, ids.String(), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte, len(names))
	rest := []byte(out)
	for _, name := range names {
		header, body, _ := bytes.Cut(rest, []byte("\n"))
		fields := bytes.Fields(header)
		if len(fields) != 3 {
			return nil, fmt.Errorf("git cat-file: bad header %q for %s", header, name)
		}
		size, err := strconv.Atoi(string(fields[2]))
		if err != nil || len(body) <= size {
			return nil, fmt.Errorf("git cat-file: bad header %q for %s", header, name)
		}
		files[name], rest = body[:size], body[size+1:]
	}
	return files, nil
}

func (t commitTree) benchDirs(ctx context.Context) ([]string, error) {
	return benchDirs(t.repo.git(ctx, "", "grep", "-lzE", benchPattern, t.commit, "--", "*_test.go"))
}

func benchDirs(out string, err error) ([]string, error) {
	if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == 1 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, name := range split(out, "\x00") {
		_, file, _ := strings.Cut(name, ":")
		dirs = append(dirs, path.Dir(file))
	}
	slices.Sort(dirs)
	return slices.Compact(dirs), nil
}

func sortedKeys[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }
