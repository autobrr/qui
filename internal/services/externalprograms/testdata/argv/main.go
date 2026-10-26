// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"time"
)

// The GUI-subsystem fixture reports argv and waits until its test releases it.
func main() {
	conn, err := net.DialTimeout("tcp", os.Getenv("QUI_TEST_WINDOWS_ARGV_ADDRESS"), 5*time.Second)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		panic(err)
	}
	if err := json.NewEncoder(conn).Encode(struct {
		Args   []string
		PID    int
		Parent int
	}{os.Args[1:], os.Getpid(), os.Getppid()}); err != nil {
		panic(err)
	}
	var ack [1]byte
	if _, err := io.ReadFull(conn, ack[:]); err != nil {
		panic(err)
	}
}
