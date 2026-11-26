// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const usage = `usage: webhookreplay -old <url> -new <url> -announces <file> [flags]

Sends each announce to the webhook check of two qui servers and compares the answers.
Exit status: 0 when every answer agrees, 1 when one differs or fails, 2 on a usage error.`

var (
	errDiffers = errors.New("the answers differ")
	errUsage   = errors.New("invalid arguments")
)

type config struct {
	old, new          server
	announces, out    string
	instanceIDs       []int
	ready             string
	readyTimeout      time.Duration
	workers           int
	requestTimeout    time.Duration
	stdout, stderr    io.Writer
	client            *http.Client
	readyPollInterval time.Duration
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	switch {
	case err == nil:
	case errors.Is(err, errDiffers):
		os.Exit(1)
	case errors.Is(err, errUsage):
		fmt.Fprintf(os.Stderr, "%v\n\n%s\n", err, usage)
		os.Exit(2)
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, "webhookreplay:", err)
		os.Exit(1)
	}
}

func parseConfig(args []string, stdout, stderr io.Writer) (config, error) {
	c := config{stdout: stdout, stderr: stderr, readyPollInterval: 2 * time.Second}
	flags := flag.NewFlagSet("webhookreplay", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&c.old.url, "old", "", "base URL of the qui server that runs the old build")
	flags.StringVar(&c.new.url, "new", "", "base URL of the qui server that runs the new build")
	flags.StringVar(&c.old.key, "old-key", os.Getenv("REPLAY_OLD_KEY"), "API key of the old server (default $REPLAY_OLD_KEY)")
	flags.StringVar(&c.new.key, "new-key", os.Getenv("REPLAY_NEW_KEY"), "API key of the new server (default $REPLAY_NEW_KEY)")
	flags.StringVar(&c.announces, "announces", "", "file with one announce per line: a name, a name and a size in bytes separated by a tab, or a JSON webhook check body")
	flags.StringVar(&c.out, "out", "", "write one JSON line per announce to this file")
	instances := flags.String("instances", "", "comma-separated instance IDs to send with each announce that names none")
	flags.StringVar(&c.ready, "ready", "", "name of a torrent that both servers hold; wait until both answer it with a match before the replay")
	flags.DurationVar(&c.readyTimeout, "ready-timeout", 10*time.Minute, "how long to wait for -ready")
	flags.IntVar(&c.workers, "workers", 2, "announces in flight at once")
	flags.DurationVar(&c.requestTimeout, "request-timeout", 3*time.Minute, "timeout of one webhook check")
	if err := flags.Parse(args); err != nil {
		return c, err
	}
	if c.old.url == "" || c.new.url == "" || c.announces == "" || flags.NArg() > 0 || c.workers < 1 {
		return c, errUsage
	}
	c.old.url, c.new.url = strings.TrimRight(c.old.url, "/"), strings.TrimRight(c.new.url, "/")
	c.old.name, c.new.name = "old", "new"
	for field := range strings.SplitSeq(*instances, ",") {
		if field = strings.TrimSpace(field); field == "" {
			continue
		}
		id, err := strconv.Atoi(field)
		if err != nil || id < 1 {
			return c, fmt.Errorf("%w\n\n-instances: %q is not an instance ID", errUsage, field)
		}
		c.instanceIDs = append(c.instanceIDs, id)
	}
	c.client = &http.Client{Timeout: c.requestTimeout}
	return c, nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	c, err := parseConfig(args, stdout, stderr)
	if err != nil {
		return err
	}
	announces, err := readAnnounces(c.announces, c.instanceIDs)
	if err != nil {
		return err
	}
	if c.ready != "" {
		if err := waitReady(ctx, c); err != nil {
			return err
		}
	}
	out := io.Discard
	if c.out != "" {
		file, err := os.Create(c.out)
		if err != nil {
			return err
		}
		defer file.Close()
		out = file
	}
	results, err := replay(ctx, c, announces, out)
	if err != nil {
		return err
	}
	summary := summarize(results)
	summary.write(c.stdout)
	if !summary.agrees() {
		return errDiffers
	}
	return nil
}
