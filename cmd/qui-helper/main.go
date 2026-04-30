// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Command qui-helper is a filesystem broker that runs on a remote seedbox
// and executes filesystem operations dispatched by qui over an SSH stdio
// channel. It is not meant to be run directly — qui starts it via SSH.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/autobrr/qui/cmd/qui-helper/internal/server"
	"github.com/autobrr/qui/pkg/agent/proto"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: qui-helper <command> [flags]")
		fmt.Fprintln(os.Stderr, "commands: serve, version")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "version":
		cmdVersion(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func cmdServe(args []string) {
	var roots []string
	isStdio := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--stdio":
			isStdio = true
		case "--root":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--root requires an argument")
				os.Exit(1)
			}
			i++
			roots = append(roots, args[i])
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", args[i])
			os.Exit(1)
		}
	}

	if !isStdio {
		fmt.Fprintln(os.Stderr, "serve requires --stdio flag")
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)

	srv := server.NewServer(os.Stdin, os.Stdout)
	err := srv.Serve(ctx, roots)
	cancel()

	if err != nil {
		_ = json.NewEncoder(os.Stderr).Encode(map[string]string{
			"level":   "error",
			"error":   err.Error(),
			"message": "serve exited with error",
		})
		os.Exit(1)
	}
}

func cmdVersion(args []string) {
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", arg)
			os.Exit(1)
		}
	}

	if jsonOutput {
		banner := proto.HelloBanner{
			HelperVersion: version,
			ProtoVersion:  "1",
			Capabilities: []string{
				proto.OpDiagEcho,
			},
			Platform:  runtime.GOOS,
			Hostname:  hostname(),
			PID:       os.Getpid(),
			StartedAt: time.Now().UTC().Format(time.RFC3339),
		}
		data, _ := json.Marshal(banner)
		fmt.Println(string(data))
	} else {
		caps := []string{proto.OpDiagEcho}
		fmt.Printf("qui-helper %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		fmt.Printf("proto: v1\n")
		fmt.Printf("capabilities: %s\n", strings.Join(caps, ", "))
	}
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}
