// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

type options struct {
	cache     string
	rounds    int
	benchtime time.Duration
	limit     time.Duration
	progress  io.Writer
}

func (o options) runLimit() time.Duration { return o.limit / time.Duration(2*o.rounds) }

type builder struct {
	options
	repo     repo
	scratch  string
	roots    map[string]string
	binaries map[string]string
}

func withBuilder(ctx context.Context, r repo, o options, use func(b *builder) error) error {
	if err := os.MkdirAll(o.cache, 0o750); err != nil {
		return err
	}
	scratch, err := os.MkdirTemp(o.cache, "run-*")
	if err != nil {
		return err
	}
	b := &builder{options: o, repo: r, scratch: scratch, roots: map[string]string{}, binaries: map[string]string{}}
	defer b.clean(context.WithoutCancel(ctx))
	return use(b)
}

func (b *builder) clean(ctx context.Context) {
	for _, root := range b.roots {
		if _, err := b.repo.git(ctx, "", "worktree", "remove", "--force", "--force", root); err != nil {
			fmt.Fprintln(b.progress, "cleanup:", err)
		}
	}
	if err := os.RemoveAll(b.scratch); err != nil {
		fmt.Fprintln(b.progress, "cleanup:", err)
	}
}

func (b *builder) root(ctx context.Context, commit string) (string, error) {
	if root, ok := b.roots[commit]; ok {
		return root, nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root := filepath.Join(b.scratch, "tree-"+short(commit))
	if _, err := b.repo.git(context.WithoutCancel(ctx), "", "worktree", "add", "--quiet", "--detach", root, commit); err != nil {
		return "", err
	}
	b.roots[commit] = root
	return root, nil
}

func (b *builder) binary(ctx context.Context, commit, dir string, flags ...string) (string, error) {
	key := strings.Join(append([]string{commit, dir}, flags...), "\x00")
	if file, ok := b.binaries[key]; ok {
		return file, nil
	}
	root, err := b.root(ctx, commit)
	if err != nil {
		return "", err
	}
	file := filepath.Join(b.scratch, "bench"+strconv.Itoa(len(b.binaries))+".test")
	if runtime.GOOS == "windows" {
		file += ".exe"
	}
	args := slices.Concat([]string{"test", "-c", "-o", file}, flags, []string{"./" + dir})
	if out, err := goTool(ctx, root, args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("it does not build: %s", failure(out, err))
	}
	if _, err := os.Stat(file); err != nil {
		return "", fmt.Errorf("the package has no tests: %w", err)
	}
	b.binaries[key] = file
	return file, nil
}

func (b *builder) exec(ctx context.Context, binary, commit, dir string, args ...string) ([]byte, error) {
	root, err := b.root(ctx, commit)
	if err != nil {
		return nil, err
	}
	limited, cancel := context.WithTimeout(ctx, b.runLimit())
	defer cancel()
	args = append([]string{"-test.run=^$", "-test.timeout=0"}, args...)
	cmd := exec.CommandContext(limited, binary, args...)
	cmd.Dir = filepath.Join(root, filepath.FromSlash(dir))
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if ctx.Err() == nil && limited.Err() != nil {
		return nil, fmt.Errorf("one run takes more than %s, so %d runs of each side do not fit in %s", b.runLimit(), b.rounds, b.limit)
	}
	if err != nil {
		return stdout.Bytes(), runError(failure(append(stdout.Bytes(), stderr.Bytes()...), err))
	}
	return stdout.Bytes(), nil
}

func (b *builder) measure(ctx context.Context, commit string, one bench, pattern string) (samples, error) {
	binary, err := b.binary(ctx, commit, one.dir)
	if err != nil {
		return samples{}, err
	}
	out, failed := b.exec(ctx, binary, commit, one.dir, "-test.bench="+pattern, "-test.benchtime="+b.benchtime.String(), "-test.benchmem")
	got, err := readSamples(out, runtime.GOMAXPROCS(0))
	return got, errors.Join(failed, err)
}

func goTool(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	return cmd
}

type runError string

func (r runError) Error() string { return string(r) }

func failure(out []byte, err error) string {
	text := ""
	failed := false
	for line := range strings.Lines(string(out)) {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "panic: "), failed && line != strings.TrimLeft(line, " \t"):
			return trimmed
		case trimmed != "" && trimmed != "FAIL" && !strings.HasPrefix(trimmed, "exit status"):
			text = trimmed
		}
		failed = strings.HasPrefix(trimmed, "--- FAIL")
	}
	return cmp.Or(text, err.Error())
}
