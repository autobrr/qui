// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

const usage = `usage: benchcompare <command> [flags]

  compare  compare the benchmarks that a change touches with its merge base
  drift    compare each benchmark with its starting commit
  check    fail when a change moves a starting commit or loosens a limit without approval`

var errFailed = errors.New("")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := runCommand(ctx, os.Args[1:])
	stop()
	if err != nil {
		if !errors.Is(err, errFailed) {
			fmt.Fprintln(os.Stderr, "benchcompare:", err)
		}
		os.Exit(1)
	}
}

func runCommand(ctx context.Context, args []string) error {
	commands := map[string]func(context.Context, repo, []string) error{"compare": compareCommand, "drift": driftCommand, "check": checkCommand}
	if len(args) == 0 || commands[args[0]] == nil {
		return errors.New(usage)
	}
	root, err := repo{"."}.gitLine(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if err := commands[args[0]](ctx, repo{filepath.FromSlash(root)}, args[1:]); err != nil {
		return err
	}
	return ctx.Err()
}

func measureFlags(name string, limit time.Duration) (*flag.FlagSet, *options) {
	o := &options{progress: os.Stderr}
	flags := flag.NewFlagSet(name, flag.ExitOnError)
	flags.DurationVar(&o.limit, "timeout", limit, "stop after this time")
	flags.IntVar(&o.rounds, "count", 10, "runs of each benchmark on each side")
	flags.DurationVar(&o.benchtime, "benchtime", 20*time.Millisecond, "time of one run")
	return flags, o
}

func (o *options) start(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if o.rounds < 10 {
		return nil, nil, errors.New("-count must be 10 or more")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, nil, err
	}
	o.cache = filepath.Join(cache, "benchcompare")
	limited, cancel := context.WithTimeout(ctx, o.limit)
	return limited, cancel, nil
}

func compareCommand(ctx context.Context, r repo, args []string) error {
	flags, o := measureFlags("compare", 15*time.Minute)
	baseRef := flags.String("base", "origin/HEAD", "the branch that the change goes to")
	if err := flags.Parse(args); err != nil {
		return err
	}
	limited, cancel, err := o.start(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	return withBuilder(ctx, r, *o, func(b *builder) error {
		report, err := compare(limited, b, *baseRef)
		if err == nil {
			fmt.Print(report)
		}
		return err
	})
}

func driftCommand(ctx context.Context, r repo, args []string) error {
	flags, o := measureFlags("drift", 2*time.Hour)
	skippedFile := flags.String("skipped", "", "the skipped.txt of the run before this one")
	out := flags.String("out", "", "the folder for summary.md, issue.md, and skipped.txt")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is empty")
	}
	if err := os.MkdirAll(*out, 0o750); err != nil {
		return err
	}
	before, err := readSkipped(*skippedFile)
	if err != nil {
		return err
	}
	limited, cancel, err := o.start(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	return withBuilder(ctx, r, *o, func(b *builder) error {
		report, err := drift(limited, b, before, b.measure)
		if err != nil {
			return err
		}
		fmt.Print(report.summary)
		return report.write(*out)
	})
}

func readSkipped(file string) ([]bench, error) {
	if file == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var benches []bench
	for line := range strings.Lines(string(raw)) {
		if b, ok := parseBench(strings.TrimSpace(line)); ok && !slices.Contains(benches, b) {
			benches = append(benches, b)
		}
	}
	return benches, err
}

func (d driftReport) write(dir string) error {
	var late strings.Builder
	for _, one := range d.skipped {
		late.WriteString(one.String() + "\n")
	}
	files := map[string]string{"summary.md": d.summary, "skipped.txt": late.String(), "issue.md": d.issue}
	for _, name := range sortedKeys(files) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(files[name]), 0o600); err != nil {
			return err
		}
	}
	if d.broken {
		return errors.New("a benchmark fails. The summary gives the reason")
	}
	return nil
}

func checkCommand(ctx context.Context, r repo, args []string) error {
	flags := flag.NewFlagSet("check", flag.ExitOnError)
	baseRef := flags.String("base", "origin/HEAD", "the branch that the change goes to")
	headRef := flags.String("head", "HEAD", "the change")
	names := flags.String("labels", "[]", "the labels of the pull request, as a JSON list")
	if err := flags.Parse(args); err != nil {
		return err
	}
	var set []string
	if err := json.Unmarshal([]byte(*names), &set); err != nil {
		return fmt.Errorf("-labels: %w", err)
	}
	lines, passed, err := check(ctx, r, *baseRef, *headRef, slices.Contains(set, approveLabel))
	if err != nil {
		return err
	}
	fmt.Println(strings.Join(lines, "\n"))
	if !passed {
		return errFailed
	}
	return nil
}
